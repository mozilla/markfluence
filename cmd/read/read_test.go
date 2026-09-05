package read

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

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

func pageWithBody(body string) string {
	return `{"id":"1","title":"Runbook","spaceId":"77",` +
		`"body":{"storage":{"value":` + `"` + body + `"` + `,"representation":"storage"}},` +
		`"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}`
}

func TestRunPrintsMarkdown(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			_, _ = w.Write([]byte(pageWithBody("<p>Hello</p>")))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "title: Runbook") {
		t.Errorf("output = %q, want frontmatter with the title", out)
	}
	if !strings.Contains(out, "Hello") {
		t.Errorf("output = %q, want the converted body", out)
	}
}

// TestRunPositionsAnUnsourcedAttachment covers the placement rule read shares
// with export and attachment-download: an attachment with no recorded path
// belongs in the directory named after its page, so the markdown says so.
//
// Without this, read prints diagram.png while attachment-download writes
// runbook/diagram.png and the image does not resolve. The three agree because
// they go through one pagedoc.Options, and this is the assertion that says so
// for read.
func TestRunPositionsAnUnsourcedAttachment(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			// No markfluence comment: this one originated in Confluence, so
			// there is no recorded path to put it back at.
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"diagram.png","metadata":{}}]}`))
		default:
			_, _ = w.Write([]byte(pageWithBody(
				`<p><ac:image><ri:attachment ri:filename=\"diagram.png\" /></ac:image></p>`)))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "](runbook/diagram.png)") {
		t.Errorf("output = %q, want the attachment under the page's own directory", out)
	}
}

// TestRunKeepsARecordedPathAsWritten is the other provenance: a path recorded by
// a publish is relative to the root, and read prints from that root, so it is
// carried through untouched rather than positioned against anything.
func TestRunKeepsARecordedPathAsWritten(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/properties"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		case strings.Contains(r.URL.Path, "/child/attachment"):
			_, _ = w.Write([]byte(`{"results":[{"id":"a1","title":"brand.png","metadata":` +
				`{"comment":"markfluence: sha256=abc path=assets/brand.png"}}]}`))
		default:
			_, _ = w.Write([]byte(pageWithBody(
				`<p><ac:image><ri:attachment ri:filename=\"brand.png\" /></ac:image></p>`)))
		}
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "](assets/brand.png)") {
		t.Errorf("output = %q, want the recorded path unchanged", out)
	}
}

func TestRunPrintsStorage(t *testing.T) {
	formatFlag = formatStorage
	t.Cleanup(func() { formatFlag = formatMarkdown })

	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(pageWithBody("<p>Hello</p>")))
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(out) != "<p>Hello</p>" {
		t.Errorf("output = %q, want the raw storage body verbatim", out)
	}
	if strings.Contains(out, "title:") {
		t.Errorf("output = %q, storage format should carry no frontmatter", out)
	}
}

func TestRunInvalidFormatIsAUsageError(t *testing.T) {
	formatFlag = "pdf"
	t.Cleanup(func() { formatFlag = formatMarkdown })

	_, err := captureStdout(t, func() error {
		return run(testCmd(t, "https://wiki.example.net"), []string{"1"})
	})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error for an unsupported --format", err)
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

func TestRunEmptyBodyIsAFailure(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":"1","title":"Folder"}`))
	})
	_, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run: %v, want a silent exit-1 error for a page with no readable body", err)
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
			_, _ = w.Write([]byte(pageWithBody("<p>Hello</p>")))
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
			Format string `json:"format"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	res := env.Results
	if env.Command != "read" || len(res) != 1 || res[0].PageID != "1" || res[0].Format != "markdown" {
		t.Errorf("envelope = %+v, want command=read with one result page_id=1 format=markdown", env)
	}
}
