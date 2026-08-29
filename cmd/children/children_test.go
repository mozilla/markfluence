package children

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/pagetree"
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

// childServer answers the v1 child/page and child/folder routes pagetree.Walk
// makes for one page: pages under "1", none under any other id.
func childServer(t *testing.T) string {
	t.Helper()
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/content/1/child/page"):
			_, _ = w.Write([]byte(`{"results":[{"id":"2","type":"page","title":"Child",` +
				`"status":"current","extensions":{"position":0},"_links":{"webui":"/spaces/ENG/pages/2/Child"}}]}`))
		case strings.HasPrefix(r.URL.Path, "/wiki/rest/api/content/"):
			_, _ = w.Write([]byte(`{"results":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})
	return c.SiteURL()
}

// TestParseDepth covers the flag's whole vocabulary. 0 is the interesting case:
// it is a common spelling of "unlimited" elsewhere, so accepting it would launch
// an unbounded walk for someone who may have meant the opposite.
func TestParseDepth(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "1", want: 1},
		{in: "3", want: 3},
		{in: "all", want: pagetree.AllDepths},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "", wantErr: true},
		{in: "deep", wantErr: true},
		{in: "1.5", wantErr: true},
		{in: "ALL", wantErr: true}, // the vocabulary is lowercase, like page_width's
	} {
		got, err := parseDepth(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDepth(%q) = %d, want an error", tc.in, got)
				continue
			}
			// The message has to name the value that does mean unlimited, or a
			// caller who tried 0 has nothing to go on.
			if !strings.Contains(err.Error(), `"all"`) {
				t.Errorf("parseDepth(%q) error = %q, must mention \"all\"", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDepth(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseDepth(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestTreeIndentsByDepth pins the shape of the human output: titles indent so the
// hierarchy is visible, while TYPE and ID stay aligned so it is still greppable.
func TestTreeIndentsByDepth(t *testing.T) {
	got := tree([]pagetree.Node{
		{ID: "11", Type: "page", Title: "Alpha", Depth: 1},
		{ID: "2222", Type: "folder", Title: "Articles", Depth: 1},
		{ID: "33", Type: "page", Title: "Inside", Depth: 2},
		{ID: "44", Type: "page", Title: "Deeper", Depth: 3},
	})
	want := strings.Join([]string{
		"TYPE    ID    TITLE",
		"page    11    Alpha",
		"folder  2222  Articles",
		"page    33      Inside",
		"page    44        Deeper",
	}, "\n")
	if got != want {
		t.Errorf("tree mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestTreeHasNoTrailingSpaces(t *testing.T) {
	got := tree([]pagetree.Node{{ID: "11", Type: "page", Title: "Alpha", Depth: 1}})
	for i, line := range strings.Split(got, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestRunListsChildren(t *testing.T) {
	url := childServer(t)
	out, err := captureStdout(t, func() error { return run(testCmd(t, url), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "Child") || !strings.Contains(out, "2") {
		t.Errorf("output = %q, want the child page listed", out)
	}
}

func TestRunNoChildren(t *testing.T) {
	c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"results":[]}`))
	})
	out, err := captureStdout(t, func() error { return run(testCmd(t, c.SiteURL()), []string{"1"}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "No children.") {
		t.Errorf("output = %q, want the no-children message", out)
	}
}

func TestRunInvalidDepthIsAUsageError(t *testing.T) {
	depthOpt = "0"
	t.Cleanup(func() { depthOpt = "1" })

	_, err := captureStdout(t, func() error {
		return run(testCmd(t, "https://wiki.example.net"), []string{"1"})
	})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error for --depth 0", err)
	}
}

func TestRunJSONOutput(t *testing.T) {
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	url := childServer(t)
	out, err := captureStdout(t, func() error { return run(testCmd(t, url), []string{"1"}) })
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
	if env.Command != "children" || len(env.Results) != 1 || env.Results[0].ID != "2" {
		t.Errorf("envelope = %+v, want command=children with one result id=2", env)
	}
}
