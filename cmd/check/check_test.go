package check

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// write creates path (and its parent directories) with body.
func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testCmd builds a bare *cobra.Command carrying the one flag run() reads
// itself (--root); --show-html is a package-level var, toggled directly by
// tests that need it, the same way other commands' dry-run-style flags are.
func testCmd(t *testing.T, root string) *cobra.Command {
	t.Helper()
	c := &cobra.Command{}
	c.Flags().String("root", root, "")
	return c
}

// captureOutput runs fn with both os.Stdout and os.Stderr redirected into one
// buffer, returning what it printed. check's human output splits Warn/Error
// (stderr) from Info/show-html (stdout), so an end-to-end test needs both.
func captureOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = w, w
	runErr := fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func TestRunClean(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "clean.md"), "# Clean\n\nNothing wrong here.\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "clean.md")}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("output = %q, want a clean line", out)
	}
}

func TestRunWarnings(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "draft.md"), "# Draft\n\nNo page_id yet.\n")
	write(t, filepath.Join(dir, "main.md"), "# Main\n\n[the draft](draft.md)\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if err != nil {
		t.Fatalf("run: %v (warnings alone must not fail)", err)
	}
	if !strings.Contains(out, "link not resolved") {
		t.Errorf("output = %q, want the unresolved-link warning", out)
	}
}

func TestRunSamePageAnchorUnpublishedWarnsDistinctly(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "draft.md"), "# Draft\n\n[back to top](#draft)\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "draft.md")}) })
	if err != nil {
		t.Fatalf("run: %v (warnings alone must not fail)", err)
	}
	if !strings.Contains(out, "same-page anchor not resolved: #draft") {
		t.Errorf("output = %q, want the same-page-anchor warning", out)
	}
	if strings.Contains(out, "link not resolved") {
		t.Errorf("output = %q, must not read as an unresolved cross-file link to itself", out)
	}
}

func TestRunSelfReferenceWithBadFragmentDoesNotClaimAnchorResolved(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "draft.md"), "# Draft\n\n[bad self ref](draft.md#does-not-exist)\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "draft.md")}) })
	if err != nil {
		t.Fatalf("run: %v (warnings alone must not fail)", err)
	}
	if !strings.Contains(out, "anchor not found: draft.md#does-not-exist") {
		t.Errorf("output = %q, want the anchor-not-found warning", out)
	}
	if strings.Contains(out, "same-page anchor not resolved") {
		t.Errorf("output = %q, must not claim the anchor resolved when it didn't", out)
	}
}

func TestRunBroken(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.md"), "# Main\n\n![missing](nope.png)\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error for a broken file", err)
	}
	if !strings.Contains(out, "IMAGE BROKEN") {
		t.Errorf("output = %q, want the broken-image message", out)
	}
}

// TestRunNameCollisionIsBroken pins the bucket, not just the message. A
// collision fails the conversion, and every other conversion failure is
// reported as a failed file -- but this one is a defect in the document, the
// same kind of thing as a dead link, so it belongs in broken where an author
// looking for what to fix will find it.
func TestRunNameCollisionIsBroken(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "arch", "diagram.png"), "PNG")
	write(t, filepath.Join(dir, "deploy", "diagram.png"), "PNG")
	write(t, filepath.Join(dir, "main.md"),
		"# Main\n\n![arch](arch/diagram.png)\n\n![deploy](deploy/diagram.png)\n")

	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error", err)
	}
	var env struct {
		Results []struct {
			Status string   `json:"status"`
			Broken []string `json:"broken"`
			Error  *string  `json:"error"`
			Code   *string  `json:"code"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(env.Results) != 1 {
		t.Fatalf("results = %v, want one", env.Results)
	}
	got := env.Results[0]
	if got.Status != "broken" {
		t.Errorf("status = %q, want broken -- a collision is a document defect, not a failed file", got.Status)
	}
	if got.Error != nil || got.Code != nil {
		t.Errorf("error/code = %v/%v, want both null (broken says it all)", got.Error, got.Code)
	}
	if len(got.Broken) != 1 ||
		!strings.Contains(got.Broken[0], "arch/diagram.png") ||
		!strings.Contains(got.Broken[0], "deploy/diagram.png") {
		t.Errorf("broken = %v, want one entry naming both paths", got.Broken)
	}
}

func TestRunFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.md"), "---\npage_width: huge\n---\n# Bad\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "bad.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error for a failed file", err)
	}
	if !strings.Contains(out, "page_width") {
		t.Errorf("output = %q, want the page_width error", out)
	}
}

func TestRunUnterminatedFrontmatterIsFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.md"), "---\ntitle: T\nno closing delimiter\n")

	_, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "bad.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error for unterminated frontmatter", err)
	}
}

func TestRunNonNumericPageIDIsFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.md"), "---\npage_id: not-a-number\n---\n# Bad\n")

	_, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "bad.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error for a non-numeric page_id", err)
	}
}

func TestRunExitsCleanlyWhenEverythingPasses(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.md"), "# A\n")
	write(t, filepath.Join(dir, "b.md"), "# B\n")

	_, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "b.md")})
	})
	if err != nil {
		t.Fatalf("run: %v, want nil when every file passes", err)
	}
}

func TestRunReportsOneRootPerBatch(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "markfluence.yaml"), "")
	write(t, filepath.Join(dir, "a.md"), "# A\n")
	write(t, filepath.Join(dir, "sub", "b.md"), "# B\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "a.md"), filepath.Join(dir, "sub", "b.md")})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.Count(out, "root: "+dir) != 1 {
		t.Errorf("output = %q, want exactly one root line for %s", out, dir)
	}
}

func TestRunReportsMultipleRootsInJSON(t *testing.T) {
	one := t.TempDir()
	two := t.TempDir()
	write(t, filepath.Join(one, "markfluence.yaml"), "")
	write(t, filepath.Join(one, "a.md"), "# A\n")
	write(t, filepath.Join(two, "markfluence.yaml"), "")
	write(t, filepath.Join(two, "b.md"), "# B\n")

	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(one, "a.md"), filepath.Join(two, "b.md")})
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var env struct {
		Command string   `json:"command"`
		Roots   []string `json:"roots"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if env.Command != "check" {
		t.Errorf("command = %q, want check", env.Command)
	}
	if len(env.Roots) != 2 {
		t.Errorf("roots = %v, want both %s and %s", env.Roots, one, two)
	}
}

func TestRunShowHTML(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.md"), "# Main\n\nHello.\n")

	showHTML = true
	t.Cleanup(func() { showHTML = false })

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if !strings.Contains(out, "storage HTML") || !strings.Contains(out, "<h1>Main</h1>") {
		t.Errorf("output = %q, want the storage HTML section", out)
	}
}

func TestRunJSONEnvelopeShowHTML(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.md"), "# Main\n\nHello.\n")

	showHTML = true
	t.Cleanup(func() { showHTML = false })
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var env struct {
		Results []struct {
			Debug *struct {
				HTML string `json:"html"`
			} `json:"debug"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(env.Results) != 1 || env.Results[0].Debug == nil || env.Results[0].Debug.HTML == "" {
		t.Errorf("envelope = %+v, want a non-null debug.html", env)
	}
}

// TestNeverImportsClient guards the architectural point of the whole command:
// check must stay offline and credential-free. This can't regress silently --
// importing internal/client would be caught here even before any test that
// exercises behavior would notice.
func TestNeverImportsClient(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", f, err)
		}
		for _, imp := range file.Imports {
			if strings.Trim(imp.Path.Value, `"`) == "github.com/mozilla/markfluence/internal/client" {
				t.Errorf("%s imports internal/client; check must stay offline/credential-free", f)
			}
		}
	}
}
