package find

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// testCmd builds a bare *cobra.Command carrying the flags run() reads,
// pointed at url. It doesn't go through the real root command (which would
// need a full command tree and CONFLUENCE_TOKEN can't be a flag), so the
// token is supplied via the environment instead, exactly as a real
// invocation would.
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

// findRunServer answers the two requests FindByTitle makes with no --space:
// the v2 pages query and the CQL search.
func findRunServer(t *testing.T, pages, search string) string {
	t.Helper()
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages"):
			_, _ = w.Write([]byte(pages))
		case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/search"):
			_, _ = w.Write([]byte(search))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	return c.SiteURL()
}

func TestRunFindsMatches(t *testing.T) {
	url := findRunServer(t,
		`{"results":[{"id":"1","title":"Runbook","status":"current",`+
			`"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}],"_links":{}}`,
		`{"results":[],"_links":{}}`)

	out, err := captureStdout(t, func() error { return run(testCmd(t, url), []string{"Runbook"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "Runbook") || !strings.Contains(out, "1") {
		t.Errorf("output = %q, want it to list the match", out)
	}
}

func TestRunReportsNoMatches(t *testing.T) {
	url := findRunServer(t, `{"results":[],"_links":{}}`, `{"results":[],"_links":{}}`)

	out, err := captureStdout(t, func() error { return run(testCmd(t, url), []string{"Ghost"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "No matches found.") {
		t.Errorf("output = %q, want the no-matches message", out)
	}
}

func TestRunEmptyTitleIsAUsageError(t *testing.T) {
	cmd := testCmd(t, "https://wiki.example.net")
	_, err := captureStdout(t, func() error { return run(cmd, []string{"  "}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error", err)
	}
}

func TestRunUnknownSpaceIsAUsageError(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/wiki/api/v2/spaces") {
			_, _ = w.Write([]byte(`{"results":[]}`))
			return
		}
		t.Errorf("unexpected request with an unresolved space: %s", r.URL.Path)
	})
	// run() reads the package-level spaceOpt (bound to Cmd's own --space flag
	// in init()), not a flag on whatever *cobra.Command is passed in.
	spaceOpt = "NOPE"
	t.Cleanup(func() { spaceOpt = "" })

	_, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"Runbook"}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error for an unknown space", err)
	}
}

func TestRunJSONOutput(t *testing.T) {
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	url := findRunServer(t,
		`{"results":[{"id":"1","title":"Runbook","status":"current",`+
			`"_links":{"webui":"/spaces/ENG/pages/1/Runbook"}}],"_links":{}}`,
		`{"results":[],"_links":{}}`)

	out, err := captureStdout(t, func() error { return run(testCmd(t, url), []string{"Runbook"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var env struct {
		Command string `json:"command"`
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if env.Command != "find" || len(env.Results) != 1 || env.Results[0].ID != "1" {
		t.Errorf("envelope = %+v, want command=find with one result id=1", env)
	}
}

func TestTableAligns(t *testing.T) {
	got := table([]client.TitleMatch{
		{ID: "500", Type: "page", Title: "Runbook", Status: "current", Space: "ENG",
			URL: "https://wiki.example.net/wiki/spaces/ENG/pages/500/Runbook"},
		{ID: "300", Type: "folder", Title: "Runbook", Status: "current", Space: "CLOUDSERVICES",
			URL: "https://wiki.example.net/wiki/spaces/CLOUDSERVICES/folder/300"},
	})
	want := strings.Join([]string{
		"TYPE    ID   SPACE          STATUS   TITLE    URL",
		"page    500  ENG            current  Runbook  https://wiki.example.net/wiki/spaces/ENG/pages/500/Runbook",
		"folder  300  CLOUDSERVICES  current  Runbook  https://wiki.example.net/wiki/spaces/CLOUDSERVICES/folder/300",
	}, "\n")
	if got != want {
		t.Errorf("table mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

// TestTableHasNoTrailingWhitespace: the last column is deliberately unpadded,
// so a row is safe to copy out of a terminal.
func TestTableHasNoTrailingWhitespace(t *testing.T) {
	out := table([]client.TitleMatch{
		{ID: "1", Type: "page", Title: "A", Status: "current", Space: "ENG", URL: "https://x/1"},
		{ID: "22", Type: "page", Title: "Longer title", Status: "archived", Space: "OPSOPS", URL: "https://x/22"},
	})
	for i, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

// TestTableDashesAMissingLink: a match whose space key could not be derived is
// still a usable row -- the id is the answer -- so it renders as "-" rather
// than collapsing the columns.
func TestTableDashesAMissingLink(t *testing.T) {
	out := table([]client.TitleMatch{{ID: "1", Type: "page", Title: "A", Status: "current"}})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[1], " -  ") || !strings.HasSuffix(lines[1], "-") {
		t.Errorf("row = %q, want dashes for the missing space and url", lines[1])
	}
}

// TestArchivedStatusIsVisible: an archived page reserves its title but is
// absent from the page tree, so a reader who cannot see the status would treat
// an unusable id as a live page.
func TestArchivedStatusIsVisible(t *testing.T) {
	out := table([]client.TitleMatch{
		{ID: "400", Type: "page", Title: "Runbook", Status: "archived", Space: "ENG", URL: "https://x/400"},
	})
	if !strings.Contains(out, "archived") {
		t.Errorf("table = %q, want the archived status shown", out)
	}
}

func TestCmdWiring(t *testing.T) {
	if Cmd.Name() != "find" {
		t.Errorf("Cmd.Name() = %q, want find", Cmd.Name())
	}
	if Cmd.Flags().Lookup("space") == nil {
		t.Error("--space not registered")
	}
	// A title is free text and a space key lives on the server, so nothing here
	// may complete to local files.
	if Cmd.ValidArgsFunction == nil {
		t.Error("no ValidArgsFunction")
	}
	if err := Cmd.Args(Cmd, []string{"a", "b"}); err == nil {
		t.Error("two args accepted, want exactly one")
	}
}
