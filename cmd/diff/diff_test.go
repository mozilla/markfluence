package diff

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// pageStub is the page a test serves: everything the comparison reads.
type pageStub struct {
	id       string
	title    string
	parentID string
	space    string
	body     string
	width    string
	labels   []string
	// status is the page's own page status, empty for a page with none.
	// statuses is what its space offers; nil means the four this suite uses.
	status   string
	statuses []string
	// widthFails, labelsFail and statusFails make those reads error, which is
	// the uncomparable case rather than a difference.
	widthFails, labelsFail, statusFails bool
}

func (p pageStub) withDefaults() pageStub {
	if p.id == "" {
		p.id = "1234567890"
	}
	if p.title == "" {
		p.title = "Deploy Runbook"
	}
	if p.space == "" {
		p.space = "ENG"
	}
	if p.width == "" {
		p.width = "full-width" // the stored spelling of page_width: max
	}
	if p.statuses == nil {
		p.statuses = []string{"Rough draft", "In progress", "Ready for review", "Verified"}
	}
	return p
}

// handler serves the four routes a diff makes: the page, its width property,
// its labels, and nothing else.
func (p pageStub) handler(t *testing.T) http.HandlerFunc {
	p = p.withDefaults()
	return func(w http.ResponseWriter, r *http.Request) {
		write := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(v); err != nil {
				t.Errorf("encoding a response: %v", err)
			}
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/state/available"):
			if p.statusFails {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			states := []map[string]any{}
			for i, name := range p.statuses {
				states = append(states, map[string]any{"id": i + 10, "name": name, "color": "#000000"})
			}
			write(map[string]any{"spaceContentStates": states, "customContentStates": []any{}})
		case strings.HasSuffix(r.URL.Path, "/state"):
			if p.statusFails {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			if p.status == "" {
				write(map[string]any{})
				return
			}
			for i, name := range p.statuses {
				if name == p.status {
					write(map[string]any{"contentState": map[string]any{
						"id": i + 10, "name": name, "color": "#000000",
					}})
					return
				}
			}
			t.Errorf("stub status %q is not in the space's list", p.status)
		case strings.HasSuffix(r.URL.Path, "/properties"):
			if p.widthFails {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			write(map[string]any{"results": []map[string]any{
				{"id": "p1", "key": r.URL.Query().Get("key"), "value": p.width, "version": map[string]any{"number": 1}},
			}})
		case strings.HasSuffix(r.URL.Path, "/labels"):
			if p.labelsFail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			results := []map[string]any{}
			for i, name := range p.labels {
				results = append(results, map[string]any{
					"id": fmt.Sprint(i), "name": name, "prefix": "global",
				})
			}
			write(map[string]any{"results": results, "_links": map[string]any{}})
		case strings.Contains(r.URL.Path, "/api/v2/pages/"):
			write(map[string]any{
				"id": p.id, "title": p.title, "status": "current", "parentId": p.parentID,
				"body":   map[string]any{"storage": map[string]any{"value": p.body, "representation": "storage"}},
				"_links": map[string]any{"webui": "/spaces/" + p.space + "/pages/" + p.id + "/Title"},
			})
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

// project writes a temp documentation root, returning its directory. files maps
// a root-relative path to its content; markfluence.yaml is written from cfg
// (empty for a bare marker).
func projectDir(t *testing.T, cfg string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "markfluence.yaml"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// testCmd builds a bare *cobra.Command carrying the flags run() reads, and
// sets credentials for url in the environment, where a real invocation finds
// them. It doesn't go through the real root command tree.
func testCmd(t *testing.T, url string) *cobra.Command {
	t.Helper()
	t.Setenv("CONFLUENCE_URL", url)
	t.Setenv("CONFLUENCE_USERNAME", "u")
	t.Setenv("CONFLUENCE_TOKEN", "t")
	c := &cobra.Command{}
	c.Flags().String("env-file", "", "")
	c.Flags().String("root", "", "")
	c.Flags().Bool("reverse", false, "")
	return c
}

// outcome is what one run produced on each stream, plus its exit code.
type outcome struct {
	stdout, stderr string
	exit           int
}

// run executes the command against a stub, capturing both streams separately.
//
// Separately, and that is the point of the helper: the contract this command
// has to keep is that stdout alone is a patch, which a test cannot check if it
// merges the streams the way a terminal does.
func runDiff(t *testing.T, stub pageStub, dir, file string, args ...string) outcome {
	t.Helper()
	c := clienttest.New(t, stub.handler(t))
	cmd := testCmd(t, c.SiteURL())
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}

	reverseFlag = false
	for _, a := range args {
		if a == "--reverse" {
			reverseFlag = true
		}
	}
	t.Cleanup(func() { reverseFlag = false })

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	runErr := run(cmd, []string{file})

	if err := os.Chdir(wd); err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()

	out, err := readAll(outR)
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := readAll(errR)
	if err != nil {
		t.Fatal(err)
	}

	o := outcome{stdout: out, stderr: errOut}
	if runErr != nil {
		if !ui.IsSilent(runErr) {
			t.Fatalf("run returned a non-silent error: %v", runErr)
		}
		o.exit = ui.ExitCode(runErr)
	}
	return o
}

func readAll(f *os.File) (string, error) {
	defer func() { _ = f.Close() }()
	b, err := os.ReadFile(f.Name())
	if err == nil {
		return string(b), nil
	}
	// A pipe has no name to re-read; drain it instead.
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, rerr := f.Read(buf)
		sb.Write(buf[:n])
		if rerr != nil {
			return sb.String(), nil
		}
	}
}

const runbookBody = "<p>Restart the worker pool:</p>" +
	"<ac:structured-macro ac:name=\"code\"><ac:plain-text-body>" +
	"<![CDATA[kubectl rollout restart deploy/worker]]></ac:plain-text-body></ac:structured-macro>"

// The whole contract: stdout is a patch and nothing else. A frontmatter
// difference, a warning or a hint leaking onto stdout would make
// `markfluence diff FILE > my.diff` produce a file patch cannot read.
func TestStdoutIsNothingButTheDiff(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\ntitle: Renamed Locally\npage_id: 1234567890\n---\n\n" +
			"Restart the worker pool:\n\n```\nkubectl rollout restart deploy/worker -n prod\n```\n",
	})
	o := runDiff(t, pageStub{body: runbookBody}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1 (differs); stderr:\n%s", o.exit, o.stderr)
	}
	lines := strings.Split(strings.TrimSuffix(o.stdout, "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("stdout is not a diff:\n%s", o.stdout)
	}
	if lines[0] != "--- confluence/runbook.md" || lines[1] != "+++ local/runbook.md" {
		t.Errorf("stdout does not start with the two labels:\n%s", o.stdout)
	}
	for i, line := range lines[2:] {
		switch line[0] {
		case '@', '+', '-', ' ', '\\':
		default:
			t.Errorf("stdout line %d is not part of a unified diff: %q", i+3, line)
		}
	}
	// The title difference is on the other stream, where it cannot corrupt the
	// patch.
	if strings.Contains(o.stdout, "Renamed Locally") {
		t.Error("the frontmatter difference leaked onto stdout")
	}
	if !strings.Contains(o.stderr, "frontmatter differs") ||
		!strings.Contains(o.stderr, "Renamed Locally") {
		t.Errorf("stderr is missing the frontmatter report:\n%s", o.stderr)
	}
}

// The patch has to apply to the real file, which is the reason the frontmatter
// block is shared between the two sides rather than diffed.
func TestPatchApplies(t *testing.T) {
	if _, err := exec.LookPath("patch"); err != nil {
		t.Skip("patch is not installed")
	}
	const file = "docs/runbook.md"
	const local = "---\ntitle: Deploy Runbook\npage_id: 1234567890\n---\n\n" +
		"Restart the worker pool:\n\n```\nkubectl rollout restart deploy/worker -n prod\n```\n"
	dir := projectDir(t, "", map[string]string{file: local})

	o := runDiff(t, pageStub{body: runbookBody}, dir, file)
	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}

	patchFile := filepath.Join(dir, "my.diff")
	if err := os.WriteFile(patchFile, []byte(o.stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	// -R: the file on disk is the +++ side, so reversing is what pulls the
	// page's body in. --reverse exists so this needs no -R; both are tested.
	cmd := exec.Command("patch", "-R", "-p1", "-i", "my.diff")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch failed: %v\n%s\npatch was:\n%s", err, out, o.stdout)
	}

	got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(file)))
	if err != nil {
		t.Fatal(err)
	}
	// The frontmatter survived untouched, and the body is now the page's.
	if !strings.HasPrefix(string(got), "---\ntitle: Deploy Runbook\npage_id: 1234567890\n---\n") {
		t.Errorf("the patch rewrote the frontmatter block:\n%s", got)
	}
	if !strings.Contains(string(got), "kubectl rollout restart deploy/worker\n") ||
		strings.Contains(string(got), "-n prod") {
		t.Errorf("the body is not the page's:\n%s", got)
	}
}

// --reverse applies the same change without patch -R, which is the workflow
// somebody pulling the page's edits in actually types.
func TestReverseAppliesForward(t *testing.T) {
	if _, err := exec.LookPath("patch"); err != nil {
		t.Skip("patch is not installed")
	}
	const file = "runbook.md"
	dir := projectDir(t, "", map[string]string{
		file: "---\ntitle: Deploy Runbook\npage_id: 1234567890\n---\n\n" +
			"Restart the worker pool:\n\n```\nkubectl rollout restart deploy/worker -n prod\n```\n",
	})

	o := runDiff(t, pageStub{body: runbookBody}, dir, file, "--reverse")
	if !strings.HasPrefix(o.stdout, "--- local/runbook.md\n+++ confluence/runbook.md\n") {
		t.Fatalf("--reverse did not swap the labels:\n%s", o.stdout)
	}
	if err := os.WriteFile(filepath.Join(dir, "my.diff"), []byte(o.stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("patch", "-p1", "-i", "my.diff")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("patch failed: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "-n prod") {
		t.Errorf("the page's body was not applied:\n%s", got)
	}
}

// Hunk numbers are the ones a reader would count to in an editor, not ones
// counted from the body's first line. Including the frontmatter as identical
// context is what buys this.
func TestHunkLinesAreFileRelative(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		// A five-line frontmatter block, so a body-relative number would be
		// visibly wrong.
		"runbook.md": "---\ntitle: Deploy Runbook\nspace: ENG\nparent: null\npage_id: 1234567890\n---\n\n" +
			"one\n\ntwo\n\nthree\n\nEDITED\n\nfive\n\nsix\n\nseven\n",
	})
	o := runDiff(t, pageStub{
		body: "<p>one</p><p>two</p><p>three</p><p>four</p><p>five</p><p>six</p><p>seven</p>",
	}, dir, "runbook.md")

	// The frontmatter block is 6 lines, so "one" is line 8 and the edited
	// paragraph is line 14; three lines of context puts the hunk at line 11.
	// A body-relative number would say 4.
	if !strings.Contains(o.stdout, "@@ -11,7 +11,7 @@") {
		t.Errorf("hunk header is not file-relative:\n%s", o.stdout)
	}
}

// Identical is silent on both streams and exits 0, the way diff(1) is.
func TestIdenticalIsSilent(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\ntitle: Deploy Runbook\npage_id: 1234567890\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0; stdout:\n%s\nstderr:\n%s", o.exit, o.stdout, o.stderr)
	}
	if o.stdout != "" || o.stderr != "" {
		t.Errorf("not silent:\nstdout: %q\nstderr: %q", o.stdout, o.stderr)
	}
}

// The manifest case, and the reason pagemeta is threaded in at all: a pristine
// file whose metadata lives in markfluence.yaml has no frontmatter block on
// disk, and a report reading only the file's own frontmatter would compare
// nothing while a whole-document diff would call the block an addition.
func TestManifestMetadataAgrees(t *testing.T) {
	dir := projectDir(t,
		"pages:\n  runbook.md:\n    title: Deploy Runbook\n    page_id: 1234567890\n",
		map[string]string{"runbook.md": "Hello.\n"})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0; stderr:\n%s\nstdout:\n%s", o.exit, o.stderr, o.stdout)
	}
	if o.stderr != "" {
		t.Errorf("a pristine manifest-managed file reported a difference:\n%s", o.stderr)
	}
}

// And when it does differ, the report names markfluence.yaml -- otherwise
// "the title differs" sends the reader to edit the wrong file.
func TestManifestMetadataDiffersNamesTheManifest(t *testing.T) {
	dir := projectDir(t,
		"pages:\n  runbook.md:\n    title: Stale Title\n    page_id: 1234567890\n",
		map[string]string{"runbook.md": "Hello.\n"})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "(markfluence.yaml)") {
		t.Errorf("the report does not name markfluence.yaml:\n%s", o.stderr)
	}
	if o.stdout != "" {
		t.Errorf("the bodies agree, so there should be no patch:\n%s", o.stdout)
	}
}

// A field the file does not declare is not compared, because publishing would
// not touch it: an absent labels or page_width leaves the page's own alone.
func TestUndeclaredFieldsAreNotCompared(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{
		body: "<p>Hello.</p>", title: "A Title The File Never Mentions",
		parentID: "999", width: "wide", labels: []string{"runbook", "oncall"},
	}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0; stderr:\n%s", o.exit, o.stderr)
	}
	for _, field := range []string{"title", "space", "parent", "page_status", "page_width", "labels"} {
		if strings.Contains(o.stderr, field) {
			t.Errorf("%s was compared though the file declares none:\n%s", field, o.stderr)
		}
	}
}

// labels: [] is a declaration, not silence: it means remove them all, which is
// a real difference against a page that has some.
func TestEmptyLabelListIsADifference(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\nlabels: []\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", labels: []string{"runbook"}}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "labels") || !strings.Contains(o.stderr, "[runbook]") {
		t.Errorf("labels: [] was not reported as a removal:\n%s", o.stderr)
	}
}

// A page-side read that fails is uncomparable, not different. read/export omit
// the field on a failed fetch, which here would claim publishing adds a label
// that may already be on the page.
func TestFailedFetchIsUncomparable(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\nlabels: [runbook]\npage_width: wide\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{
		body: "<p>Hello.</p>", widthFails: true, labelsFail: true,
	}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0: an uncomparable field is not a difference; stderr:\n%s",
			o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "could not be read") {
		t.Errorf("the report does not say the field could not be compared:\n%s", o.stderr)
	}
	if !strings.Contains(o.stderr, "could not be compared") {
		t.Errorf("no warning about the failed comparison:\n%s", o.stderr)
	}
}

// A parent spelled as a path to a sibling .md is compared as the page id it
// names, or every file that came out of an export tree would differ.
func TestParentPathResolvesToAnID(t *testing.T) {
	files := map[string]string{
		"index.md":        "---\ntitle: Index\npage_id: 555\n---\n\nIndex.\n",
		"docs/runbook.md": "---\npage_id: 1234567890\nparent: ../index.md\n---\n\nHello.\n",
	}
	dir := projectDir(t, "", files)

	t.Run("agreeing", func(t *testing.T) {
		o := runDiff(t, pageStub{body: "<p>Hello.</p>", parentID: "555"}, dir, "docs/runbook.md")
		if o.exit != 0 {
			t.Errorf("exit = %d, want 0; stderr:\n%s", o.exit, o.stderr)
		}
	})

	t.Run("differing", func(t *testing.T) {
		o := runDiff(t, pageStub{body: "<p>Hello.</p>", parentID: "777"}, dir, "docs/runbook.md")
		if o.exit != 1 {
			t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
		}
		if !strings.Contains(o.stderr, "page 555") || !strings.Contains(o.stderr, "777") {
			t.Errorf("the report does not show both ids:\n%s", o.stderr)
		}
	})
}

func TestParentPathThatNamesNoPage(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"index.md":   "---\ntitle: Index\n---\n\nIndex.\n",
		"runbook.md": "---\npage_id: 1234567890\nparent: index.md\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", parentID: "555"}, dir, "runbook.md")
	if !strings.Contains(o.stderr, "no page_id") {
		t.Errorf("an unpublished parent was not explained:\n%s", o.stderr)
	}
}

// parent: null is a declaration -- update moves the page to the top of the
// space for it -- so it is compared, shown as null, against a page that has a
// parent; and it agrees with a page at the top.
func TestNullParentIsCompared(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\nparent: null\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", parentID: "555"}, dir, "runbook.md")
	if o.exit != 1 || !strings.Contains(o.stderr, "parent") || !strings.Contains(o.stderr, "null") {
		t.Errorf("exit = %d, want 1 and a parent row showing null; stderr:\n%s", o.exit, o.stderr)
	}
	o = runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")
	if o.exit != 0 {
		t.Errorf("a page at the top agrees with parent: null; exit = %d, stderr:\n%s", o.exit, o.stderr)
	}
}

// The project's space: default is compared when the file declares no space,
// because update refuses a page outside it; the report says where it came from.
func TestProjectDefaultSpaceIsCompared(t *testing.T) {
	dir := projectDir(t, "space: OPS\n", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")
	if o.exit != 1 || !strings.Contains(o.stderr, "project default") || !strings.Contains(o.stderr, "refuses") {
		t.Errorf("exit = %d, want 1 and a space row naming the project default; stderr:\n%s", o.exit, o.stderr)
	}
}

// Placement.Dir: a page deeper than the root has to render a recorded
// attachment's path relative to its own file, or every sourced image differs.
func TestAttachmentPathIsRelativeToTheFile(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"docs/runbook.md": "---\npage_id: 1234567890\n---\n\n![](../assets/brand.png)\n",
	})
	body := `<p><ac:image><ri:attachment ri:filename="brand.png" /></ac:image></p>`
	stub := pageStub{body: body}

	// The attachment listing is only fetched when the body references one, so
	// this route joins the four the handler already serves.
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/child/attachment") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{
				"id": "att1", "title": "brand.png",
				"metadata": map[string]any{"comment": "markfluence: sha256=abc path=assets/brand.png"},
			}}, "size": 1, "limit": 100})
			return
		}
		stub.handler(t)(w, r)
	})

	cmd := testCmd(t, c.SiteURL())
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	err := run(cmd, []string{"docs/runbook.md"})
	os.Stdout, os.Stderr = oldOut, oldErr
	_ = outW.Close()
	_ = errW.Close()
	stdout, _ := readAll(outR)
	stderr, _ := readAll(errR)

	if err != nil && ui.ExitCode(err) != 0 {
		t.Fatalf("exit = %d, want 0 (the paths agree)\nstdout:\n%s\nstderr:\n%s",
			ui.ExitCode(err), stdout, stderr)
	}
}

// --- page_status ---------------------------------------------------------------

// A case variant is not a difference: the match is case-insensitive and only the
// id travels, so the two spellings name one status. This is parent's rule --
// compare what the value resolves to, not how it is written.
func TestPageStatusCaseVariantIsNotADifference(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\npage_status: ready for review\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", status: "Ready for review"}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0; stderr:\n%s", o.exit, o.stderr)
	}
	if strings.Contains(o.stderr, "page_status") {
		t.Errorf("a case variant was reported as a difference:\n%s", o.stderr)
	}
}

func TestPageStatusDifferenceIsReported(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\npage_status: Verified\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", status: "Rough draft"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "page_status") || !strings.Contains(o.stderr, "Rough draft") {
		t.Errorf("the status difference is not reported:\n%s", o.stderr)
	}
}

// A page with no status at all differs from a file declaring one, and the report
// says so rather than showing an empty page side.
func TestPageStatusAgainstAPageWithNone(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\npage_status: Verified\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "carries no status") {
		t.Errorf("the report does not say the page has no status:\n%s", o.stderr)
	}
}

// A failed read is uncomparable, not different: the declared status may well be
// the live one, and claiming a difference nobody could check is worse than
// saying nothing.
func TestPageStatusFailedFetchIsUncomparable(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\npage_status: Verified\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", statusFails: true}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0: an uncomparable field is not a difference; stderr:\n%s",
			o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "could not be read") {
		t.Errorf("the report does not say the status could not be compared:\n%s", o.stderr)
	}
}

// A present-but-empty page_status is a file neither verb will publish, so diff
// must not report it "in sync". The labels comparison was fixed for exactly
// this: gating the warning on a row made diff exit 0 about an unpublishable
// file.
func TestEmptyPageStatusIsWarnedAbout(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\npage_status:\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "runbook.md")

	if !strings.Contains(o.stderr, "page_status") || !strings.Contains(o.stderr, "has no value") {
		t.Errorf("an unpublishable page_status went unreported:\n%s", o.stderr)
	}
}
