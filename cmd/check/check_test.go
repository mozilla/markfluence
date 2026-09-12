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

// TestRunEmptyTitleIsBroken pins that a present-but-empty title is reported.
// The narrowness elsewhere -- check never reports whether page_id/space/parent
// are set -- rests on check not knowing whether create or update is coming, and
// that reasoning stops applying to title once both verbs reject an empty one.
func TestRunEmptyTitleIsBroken(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.md"), "---\ntitle:\npage_id: 1\n---\n# Main\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error", err)
	}
	if !strings.Contains(out, "empty 'title:'") {
		t.Errorf("output = %q, want the empty-title message", out)
	}
}

// TestRunAbsentTitleIsNotReported is the other half: a file with no title key is
// the normal shape for update, which keeps the live page's title.
func TestRunAbsentTitleIsNotReported(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "main.md"), "---\npage_id: 1\n---\n# Main\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if err != nil {
		t.Fatalf("run = %v, want success", err)
	}
	if strings.Contains(out, "title") {
		t.Errorf("output = %q, want no complaint about the absent title", out)
	}
}

// TestRunEmptyTitleReportedEvenWhenConversionFails pins that a frontmatter
// defect is not hidden behind an unrelated one. A name collision aborts the
// conversion, and collecting the title check afterwards made it unreachable.
func TestRunEmptyTitleReportedEvenWhenConversionFails(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "arch", "diagram.png"), "PNG")
	write(t, filepath.Join(dir, "ops", "diagram.png"), "PNG")
	write(t, filepath.Join(dir, "main.md"),
		"---\ntitle:\n---\n![a](arch/diagram.png)\n\n![b](ops/diagram.png)\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error", err)
	}
	if !strings.Contains(out, "empty 'title:'") {
		t.Errorf("output = %q, want the empty-title message alongside the collision", out)
	}
}

// --- labels -------------------------------------------------------------------

// TestRunInvalidLabelIsFailed pins the worst label defect as a failure rather
// than a warning. "Runbook Two" publishes *successfully* as two labels that
// read back as neither, so no later run can remove them -- catching it offline
// is the only cheap place to catch it at all.
func TestRunInvalidLabelIsFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "bad.md"),
		"---\ntitle: T\nlabels: [Runbook Two]\n---\n# T\n\nBody.\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "bad.md")})
	})
	if err == nil {
		t.Fatal("run = nil error, want a failure for an invalid label")
	}
	if !strings.Contains(out, "separator") {
		t.Errorf("output = %q, want it to explain that a space is a separator", out)
	}
}

// TestRunLabelCaseIsAWarning: lowercasing is the one repair, so the file still
// publishes -- but silently rewriting an author's label without telling them is
// how a file stays permanently out of step with its page.
func TestRunLabelCaseIsAWarning(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "case.md"),
		"---\ntitle: T\nlabels: [Runbook]\n---\n# T\n\nBody.\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "case.md")})
	})
	if err != nil {
		t.Fatalf("run = %v, want a warning rather than a failure", err)
	}
	if !strings.Contains(out, "Runbook") || !strings.Contains(out, "runbook") {
		t.Errorf("output = %q, want both spellings named", out)
	}
}

// TestRunValidLabelsAreClean covers the shapes that must *not* be refused: a
// slash (ci/cd is a real label in the SRE space), an underscore, and non-ASCII.
// Mirroring the server's reject set rather than an allowlist is what makes
// these pass.
func TestRunValidLabelsAreClean(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "ok.md"),
		"---\ntitle: T\nlabels: [ci/cd, dataops_reports, héllo-wörld]\n---\n# T\n\nBody.\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "ok.md")})
	})
	if err != nil {
		t.Fatalf("run = %v, want clean", err)
	}
	if !strings.Contains(out, "clean") {
		t.Errorf("output = %q, want a clean line", out)
	}
}

// TestRunBothLabelStylesCheckTheSame closes the loop through the real file
// reader: check is the command an author runs before publishing, so it must
// accept the spelling they chose.
func TestRunBothLabelStylesCheckTheSame(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "flow.md"),
		"---\ntitle: T\nlabels: [Runbook]\n---\n# T\n\nBody.\n")
	write(t, filepath.Join(dir, "block.md"),
		"---\ntitle: T\nlabels:\n  - Runbook\n---\n# T\n\nBody.\n")

	for _, name := range []string{"flow.md", "block.md"} {
		out, err := captureOutput(t, func() error {
			return run(testCmd(t, ""), []string{filepath.Join(dir, name)})
		})
		if err != nil {
			t.Fatalf("%s: run = %v", name, err)
		}
		if !strings.Contains(out, "not lowercase") {
			t.Errorf("%s: output = %q, want the case warning", name, out)
		}
	}
}

// TestRunScalarLabelsIsFailed: the field removes every label not listed, so
// "labels:" with nothing after it must not be read as "strip this page".
func TestRunScalarLabelsIsFailed(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "scalar.md"),
		"---\ntitle: T\nlabels: runbook\n---\n# T\n\nBody.\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "scalar.md")})
	})
	if err == nil {
		t.Fatal("run = nil error, want a failure for a scalar labels field")
	}
	if !strings.Contains(out, "must be a list") {
		t.Errorf("output = %q, want it to name the list form", out)
	}
}

// check is the one verb that can find a project-wide page_width Confluence
// does not accept without publishing: internal/project cannot validate its own
// value, since it would have to import internal/pagewidth, which imports
// internal/client, which holds a *project.Cache.
func TestRunInvalidProjectPageWidthIsBroken(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "markfluence.yaml"), "page_width: huge\n")
	write(t, filepath.Join(dir, "main.md"), "---\ntitle: Main\npage_id: 1\n---\n# Main\n")

	out, err := captureOutput(t, func() error { return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")}) })
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error", err)
	}
	if !strings.Contains(out, "invalid page_width") {
		t.Errorf("output = %q, want the invalid-width message", out)
	}
	// The message has to name the project file, not the markdown file, which
	// has no page_width in it at all.
	if !strings.Contains(out, "markfluence.yaml") {
		t.Errorf("output = %q, want it to name markfluence.yaml", out)
	}
}

// A valid project-wide width is a default, not a per-file requirement.
func TestRunValidProjectPageWidthIsClean(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "markfluence.yaml"), "page_width: wide\nspace: ENG\n")
	write(t, filepath.Join(dir, "main.md"), "---\ntitle: Main\npage_id: 1\n---\n# Main\n")

	if _, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")})
	}); err != nil {
		t.Fatalf("run = %v, want success", err)
	}
}

// A markfluence.yaml that cannot be understood is a local defect in a file the
// author can open and fix, which is exactly check's subject -- so it fails the
// file as VALIDATION rather than as I/O, and without the "resolving the
// documentation root" heading, since the root was found.
func TestRunMalformedProjectFileIsAValidationFailure(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "markfluence.yaml"), "spce: ENG\n")
	write(t, filepath.Join(dir, "main.md"), "---\ntitle: Main\npage_id: 1\n---\n# Main\n")

	var env struct {
		Results []struct {
			Status string  `json:"status"`
			Code   *string `json:"code"`
			Error  *string `json:"error"`
		} `json:"results"`
	}
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })
	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")})
	})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error", err)
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}
	if len(env.Results) != 1 {
		t.Fatalf("results = %#v, want one", env.Results)
	}
	got := env.Results[0]
	if got.Status != "failed" {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.Code == nil || *got.Code != "VALIDATION" {
		t.Errorf("code = %v, want VALIDATION", got.Code)
	}
	if got.Error == nil {
		t.Fatal("error = nil, want a message")
	}
	if !strings.Contains(*got.Error, `unknown setting "spce"`) {
		t.Errorf("error = %q, want the unknown-setting message", *got.Error)
	}
	if strings.Contains(*got.Error, "resolving the documentation root") {
		t.Errorf("error = %q, want no root-resolution heading", *got.Error)
	}
}

// A project file under a *different* root says nothing about a file checked
// elsewhere: diagnostics stay scoped to the file they apply to. Both files are
// named in one run so the scoping is actually exercised -- naming only the good
// one would pass against any implementation, since nothing would touch the bad
// tree at all.
func TestRunProjectDefectIsScopedToItsOwnRoot(t *testing.T) {
	base := t.TempDir()
	bad := filepath.Join(base, "bad")
	good := filepath.Join(base, "good")
	write(t, filepath.Join(bad, "markfluence.yaml"), "page_width: huge\n")
	write(t, filepath.Join(bad, "bad.md"), "---\ntitle: Bad\npage_id: 1\n---\n# Bad\n")
	write(t, filepath.Join(good, "markfluence.yaml"), "page_width: wide\n")
	write(t, filepath.Join(good, "good.md"), "---\ntitle: Good\npage_id: 2\n---\n# Good\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{
			filepath.Join(good, "good.md"), filepath.Join(bad, "bad.md")})
	})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 1 {
		t.Fatalf("run = %v, want a silent exit-1 error (bad.md is broken)", err)
	}
	if !strings.Contains(out, "1 of 2 file(s) failed") {
		t.Errorf("output = %q, want exactly one of the two files to fail", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "good.md") && strings.Contains(line, "invalid page_width") {
			t.Errorf("good.md was blamed for the other project's width: %q", line)
		}
	}
}

// A file declaring its own page_width wins over the project file, so the
// project's bad value must not fail it: check's rule is that a false positive
// is worse than a miss (CLAUDE.md), and update would publish this file fine.
func TestRunInvalidProjectPageWidthIsNotReportedForAFileThatOverridesIt(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "markfluence.yaml"), "page_width: huge\n")
	write(t, filepath.Join(dir, "main.md"),
		"---\ntitle: Main\npage_id: 1\npage_width: wide\n---\n# Main\n")

	out, err := captureOutput(t, func() error {
		return run(testCmd(t, ""), []string{filepath.Join(dir, "main.md")})
	})
	if err != nil {
		t.Fatalf("run = %v, want success: the file declares its own width", err)
	}
	if strings.Contains(out, "invalid page_width") {
		t.Errorf("output = %q, want no complaint: the file's own width wins", out)
	}
}
