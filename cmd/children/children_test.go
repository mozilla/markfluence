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
	"github.com/mozilla/markfluence/internal/schematest"
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
	// run reads --depth's *value* from the package-level flag var, but asks the
	// command whether it was set at all, so the flag has to exist here too.
	c.Flags().String("depth", "1", "")
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

// captureStderr runs fn with os.Stderr redirected, returning what it printed.
func captureStderr(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	runErr := fn()
	os.Stderr = old
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

// spaceServer answers the space-id resolve, the space root-page collection, and
// the child routes under the one root it reports. spaceID "" makes the key
// unknown.
func spaceServer(t *testing.T, spaceID string) string {
	t.Helper()
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/wiki/api/v2/spaces":
			if spaceID == "" {
				_, _ = w.Write([]byte(`{"results":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"id":"` + spaceID + `"}]}`))
		case r.URL.Path == "/wiki/rest/api/space/ENG/content/page":
			_, _ = w.Write([]byte(`{"results":[{"id":"1","type":"page","title":"Home",` +
				`"status":"current","extensions":{"position":0},` +
				`"_links":{"webui":"/spaces/ENG/overview"}}]}`))
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

// withSpace sets --space for one test, restoring the package-level flag after.
func withSpace(t *testing.T, key string) {
	t.Helper()
	spaceOpt = key
	t.Cleanup(func() { spaceOpt = "" })
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

// TestCheckTarget is the exactly-one-of rule. Both spellings name the root of the
// walk, so neither "both" nor "neither" has an answer.
func TestCheckTarget(t *testing.T) {
	if err := checkTarget([]string{"1"}, ""); err != nil {
		t.Errorf("PAGE alone: %v", err)
	}
	if err := checkTarget(nil, "ENG"); err != nil {
		t.Errorf("--space alone: %v", err)
	}
	err := checkTarget(nil, "")
	if err == nil || !strings.Contains(err.Error(), "--space") {
		t.Errorf("neither = %v, want an error naming --space", err)
	}
	err = checkTarget([]string{"1"}, "ENG")
	if err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Errorf("both = %v, want a refusal", err)
	}
}

// TestRunNoTargetIsAUsageError: the check happens before credentials are
// resolved, so it fails the same way with no server to talk to.
func TestRunNoTargetIsAUsageError(t *testing.T) {
	_, err := captureStdout(t, func() error {
		return run(testCmd(t, "https://wiki.example.net"), nil)
	})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error for no PAGE and no --space", err)
	}
}

func TestRunListsASpace(t *testing.T) {
	withSpace(t, "ENG")
	url := spaceServer(t, "77")
	out, err := captureStdout(t, func() error { return run(testCmd(t, url), nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Depth 1 is the space's root pages, so the homepage is a row and its child
	// is not.
	if !strings.Contains(out, "Home") {
		t.Errorf("output = %q, want the space's root page listed", out)
	}
	if strings.Contains(out, "Child") {
		t.Errorf("output = %q, want nothing below the root at --depth 1", out)
	}
}

// TestRunSpaceHintsAtDepth: one row is what a space's top level usually is, and
// reading it as the whole space is the trap the hint exists for.
func TestRunSpaceHintsAtDepth(t *testing.T) {
	withSpace(t, "ENG")
	url := spaceServer(t, "77")

	errOut, err := captureStderr(t, func() error { return run(testCmd(t, url), nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(errOut, "--depth all") {
		t.Errorf("stderr = %q, want a hint naming --depth all", errOut)
	}

	// Not when the caller already said how deep to go: they know the flag.
	cmd := testCmd(t, url)
	if err := cmd.Flags().Set("depth", "1"); err != nil {
		t.Fatal(err)
	}
	errOut, err = captureStderr(t, func() error { return run(cmd, nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Contains(errOut, "--depth all") {
		t.Errorf("stderr = %q, want no hint once --depth was given", errOut)
	}
}

// TestRunSpaceHintStaysOffStdout is what keeps the table pipeable: the hint
// explains the listing, so it must not become a row of it.
func TestRunSpaceHintStaysOffStdout(t *testing.T) {
	withSpace(t, "ENG")
	out, err := captureStdout(t, func() error { return run(testCmd(t, spaceServer(t, "77")), nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.Contains(line, "--depth") || strings.TrimSpace(line) == "" {
			t.Errorf("stdout line %d = %q, want only table rows", i, line)
		}
	}
}

// TestRunSpaceJSONHasNoHint: the hint is prose for a human, and a stray line in
// stdout would make the envelope unparseable.
func TestRunSpaceJSONHasNoHint(t *testing.T) {
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })
	withSpace(t, "ENG")

	out, err := captureStdout(t, func() error { return run(testCmd(t, spaceServer(t, "77")), nil) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var env struct {
		Results []struct {
			ID       string  `json:"id"`
			ParentID *string `json:"parent_id"`
			Depth    int     `json:"depth"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(env.Results) != 1 || env.Results[0].ID != "1" {
		t.Fatalf("results = %+v, want the one root page", env.Results)
	}
	// A root page hangs off no node, and the space is not one.
	if env.Results[0].ParentID != nil {
		t.Errorf("parent_id = %q, want null", *env.Results[0].ParentID)
	}
}

// TestRunUnknownSpaceIsAUsageError: an unknown key is a typo, not a failed walk,
// and it must not be confused with the 404 a rejected credential produces.
func TestRunUnknownSpaceIsAUsageError(t *testing.T) {
	withSpace(t, "ENG")
	_, err := captureStdout(t, func() error { return run(testCmd(t, spaceServer(t, "")), nil) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run: %v, want a silent exit-2 usage error for an unknown space key", err)
	}
}

// TestRunSpaceFailureIsAnErrorObject: a walk that fails partway names no page,
// so it must not be reported as a results[0] failure whose page_id would be the
// space key -- an id that resolves to nothing.
func TestRunSpaceFailureIsAnErrorObject(t *testing.T) {
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })
	withSpace(t, "ENG")

	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wiki/api/v2/spaces" {
			_, _ = w.Write([]byte(`{"results":[{"id":"77"}]}`))
			return
		}
		// A 500 with no Retry-After is not retried, so this fails once.
		w.WriteHeader(http.StatusInternalServerError)
	})

	var out, errOut string
	var runErr error
	out, _ = captureStdout(t, func() error {
		errOut, runErr = captureStderr(t, func() error { return run(testCmd(t, c.SiteURL()), nil) })
		return nil
	})
	if !ui.IsSilent(runErr) || ui.ExitCode(runErr) != 1 {
		t.Fatalf("run: %v, want a silent exit-1 operational failure", runErr)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("stdout = %q, want nothing -- there is no envelope to emit", out)
	}
	if !strings.Contains(errOut, `"command": "children"`) {
		t.Fatalf("stderr = %q, want a children error object", errOut)
	}
	if strings.Contains(errOut, "page_id") {
		t.Errorf("stderr = %q, must not report a page_id for a space walk", errOut)
	}
	schematest.ValidateError(t, []byte(errOut))
}
