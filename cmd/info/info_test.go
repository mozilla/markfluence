package info

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// testCmd builds a bare *cobra.Command carrying the flags run() reads,
// pointed at url. It doesn't go through the real root command tree, and
// CONFLUENCE_TOKEN (never a flag) comes from the environment instead, as it
// would in a real invocation.
func testCmd(t *testing.T, url string) *cobra.Command {
	t.Helper()
	t.Setenv("CONFLUENCE_TOKEN", "t")
	c := &cobra.Command{}
	c.Flags().String("url", url, "")
	c.Flags().String("username", "u", "")
	c.Flags().String("cloud-id", "", "")
	c.Flags().String("env-file", "", "")
	return c
}

// captureStdout runs fn with os.Stdout redirected, returning what it printed.
func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	runErr := fn()
	os.Stdout = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

const pageJSON = `{"id":"1","title":"Runbook","status":"current","spaceId":"77",` +
	`"version":{"number":3},"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}`

func TestRenderValue(t *testing.T) {
	if got := renderValue("max"); got != "max" {
		t.Errorf("renderValue(string) = %q, want max", got)
	}
	if got := renderValue(map[string]any{"version": "v2"}); got != `{"version":"v2"}` {
		t.Errorf("renderValue(map) = %q", got)
	}
	if got := renderValue(3); got != "3" {
		t.Errorf("renderValue(3) = %q, want 3", got)
	}
}

func TestRenderValueTruncatesLongValues(t *testing.T) {
	rendered := renderValue(strings.Repeat("x", 500))
	if n := utf8.RuneCountInString(rendered); n != valueMax {
		t.Errorf("rendered length = %d, want %d", n, valueMax)
	}
	if !strings.HasSuffix(rendered, "…") {
		t.Errorf("rendered = %q, want it to end with …", rendered)
	}
}

func TestPropertiesSectionSortsByKey(t *testing.T) {
	props := []client.Property{
		{Key: "editor", Value: "v2"},
		{Key: "content-appearance-published", Value: "max"},
	}
	want := "content properties:\n  content-appearance-published: max\n  editor: v2"
	if got := propertiesSection(props, nil); got != want {
		t.Errorf("propertiesSection =\n%q\nwant\n%q", got, want)
	}
}

func TestPropertiesSectionEmpty(t *testing.T) {
	if got := propertiesSection(nil, nil); got != "content properties: (none)" {
		t.Errorf("propertiesSection(empty) = %q", got)
	}
}

func TestPropertiesSectionFetchError(t *testing.T) {
	got := propertiesSection(nil, errors.New("boom"))
	if got != "content properties: (could not fetch: boom)" {
		t.Errorf("propertiesSection(error) = %q", got)
	}
}

func TestRunPrintsPageInfo(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(pageJSON))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "Runbook") || !strings.Contains(out, "id:") {
		t.Errorf("output = %q, want the page's metadata", out)
	}
}

func TestRunPageNotFound(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"errors":[{"status":404,"code":"NOT_FOUND"}]}`))
	})
	_, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"999"}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run: %v, want a silent exit-1 error for a missing page", err)
	}
}

func TestRunWithPropertiesFlag(t *testing.T) {
	showProperties = true
	t.Cleanup(func() { showProperties = false })

	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[{"key":"content-appearance-published","value":"max"}]}`))
		default:
			_, _ = w.Write([]byte(pageJSON))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "content properties:") || !strings.Contains(out, "content-appearance-published") {
		t.Errorf("output = %q, want the properties section listed", out)
	}
}

func TestRunJSONOutput(t *testing.T) {
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(pageJSON))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var env struct {
		Command string `json:"command"`
		Results []struct {
			PageID string `json:"page_id"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if env.Command != "info" || len(env.Results) != 1 || env.Results[0].PageID != "1" {
		t.Errorf("envelope = %+v, want command=info with one result page_id=1", env)
	}
}
