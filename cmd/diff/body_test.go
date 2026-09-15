package diff

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/ui"
)

// The frontmatter block is shared byte for byte, and the author's blank line
// belongs to it rather than to either body -- a file with two blank lines after
// the delimiter must not diff against a rendered side with one.
func TestDocumentsShareTheFrontmatterAndTheBlankLine(t *testing.T) {
	const content = "---\ntitle: A\npage_id: 1\n---\n\n\nHello.\n"
	body := content[strings.Index(content, "---\n\n")+len("---\n"):]

	confluence, local := documents(content, body, "Hello.\n")
	if local != content {
		t.Errorf("the local side is not the file's bytes:\n got %q\nwant %q", local, content)
	}
	if confluence != content {
		t.Errorf("an agreeing body produced a difference:\n got %q\nwant %q", confluence, content)
	}
}

// A file with no frontmatter block needs no special case: the prefix is empty
// and the line numbers are already right.
func TestDocumentsWithNoFrontmatter(t *testing.T) {
	const content = "Hello.\n"
	confluence, local := documents(content, content, "Hello.\n")
	if local != content || confluence != content {
		t.Errorf("got (%q, %q), want both %q", confluence, local, content)
	}
}

// The local side is never normalized, because the patch has to apply to the
// real file: padding a missing final newline would put a line in the last
// hunk's context that the file does not have.
func TestDocumentsDoNotPadTheLocalSide(t *testing.T) {
	const content = "---\ntitle: A\n---\n\nHello."
	_, local := documents(content, "\nHello.", "Hello.\n")
	if local != content {
		t.Errorf("the local side was normalized: %q", local)
	}
}

// A label is recognised by its *position*, never by its prefix. This test
// asserted the opposite and passed, which is how the collision below survived:
// a removed body line is "-" plus its content, so "--json is a flag" arrives
// as "---json is a flag" and looks exactly like a file label.
func TestClassifyReadsBodyLines(t *testing.T) {
	for line, want := range map[string]lineKind{
		"@@ -1,2 +1,2 @@":              lineHunk,
		"+added":                       lineAdded,
		"-removed":                     lineRemoved,
		" context":                     lineContext,
		"\\ No newline at end of file": lineContext,
		"":                             lineContext,
		// Content that looks like a label, which is the point: past the first
		// two lines there are no labels left to find.
		"----":            lineRemoved,
		"---json is here": lineRemoved,
		"+++plus":         lineAdded,
	} {
		if got := classify(line); got != want {
			t.Errorf("classify(%q) = %v, want %v", line, got, want)
		}
	}
}

func TestBodyLinesDropsExactlyTheLabels(t *testing.T) {
	text := "--- confluence/a.md\n+++ local/a.md\n@@ -1,1 +1,1 @@\n-x\n+y\n"
	got := bodyLines(text)
	want := []string{"@@ -1,1 +1,1 @@", "-x", "+y", ""}
	if len(got) != len(want) {
		t.Fatalf("bodyLines = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bodyLines[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// A body line that begins with two dashes -- a markdown thematic break, a long
// CLI flag, both of which this repository's own docs are full of -- was
// classified as a file label: left out of the counts and coloured as one.
func TestDashedBodyLinesAreCountedAndColoured(t *testing.T) {
	before := "intro\n\n---\n\n--json is a flag\n\noutro\n"
	after := "intro\n\noutro\n"
	d, err := compare(before, after, "a.md", false)
	if err != nil {
		t.Fatal(err)
	}
	// Five, which is every - line in the hunk. Before the fix the two dashed
	// ones were read as file labels and it reported three.
	if d.Removed != 5 {
		t.Errorf("removed = %d, want 5\n%s", d.Removed, d.Text)
	}

	marked := renderDiff(d.Text, styler{
		added:   func(s string) string { return "A(" + s + ")" },
		removed: func(s string) string { return "R(" + s + ")" },
		meta:    func(s string) string { return "M(" + s + ")" },
	})
	for _, want := range []string{"R(----)", "R(---json is a flag)"} {
		if !strings.Contains(marked, want) {
			t.Errorf("%s is not coloured as a removal:\n%s", want, marked)
		}
	}
	// And the real labels still are labels.
	if !strings.Contains(marked, "M(--- confluence/a.md)") {
		t.Errorf("the file label lost its colour:\n%s", marked)
	}
}

func TestCompareCounts(t *testing.T) {
	before := "one\ntwo\nthree\n"
	after := "one\nTWO\nthree\n"
	d, err := compare(before, after, "a.md", false)
	if err != nil {
		t.Fatal(err)
	}
	if !d.Differs() {
		t.Fatal("no difference reported")
	}
	// 1/1, not 2/2: the ---/+++ header lines start with - and + and must not
	// be counted, which is what a naive count of the text would do.
	if d.Added != 1 || d.Removed != 1 {
		t.Errorf("added/removed = %d/%d, want 1/1\n%s", d.Added, d.Removed, d.Text)
	}
	if !strings.HasPrefix(d.Text, "--- confluence/a.md\n+++ local/a.md\n") {
		t.Errorf("labels are wrong:\n%s", d.Text)
	}
}

func TestCompareIdenticalIsEmpty(t *testing.T) {
	d, err := compare("same\n", "same\n", "a.md", false)
	if err != nil {
		t.Fatal(err)
	}
	if d.Differs() || d.Text != "" {
		t.Errorf("identical bodies produced %q", d.Text)
	}
}

// Colour is applied to the library's own output line by line, so what is
// printed stays byte-identical to what it produced. A marking styler is passed
// in because lipgloss emits nothing when stdout is not a terminal, which is
// every test: wiring the real style would assert against unstyled text.
func TestRenderDiffMarksTheRightLines(t *testing.T) {
	text := "--- confluence/a.md\n+++ local/a.md\n@@ -1,2 +1,2 @@\n context\n-old\n+new\n"
	got := renderDiff(text, styler{
		added:   func(s string) string { return "A(" + s + ")" },
		removed: func(s string) string { return "R(" + s + ")" },
		meta:    func(s string) string { return "M(" + s + ")" },
	})
	want := "M(--- confluence/a.md)\nM(+++ local/a.md)\nM(@@ -1,2 +1,2 @@)\n context\nR(-old)\nA(+new)\n"
	if got != want {
		t.Errorf("renderDiff:\n got %q\nwant %q", got, want)
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	if got := renderDiff("", termStyler); got != "" {
		t.Errorf("renderDiff(\"\") = %q", got)
	}
}

// Provenance: when both locations supply a value, the report says so, because
// correcting such a field means editing two files.
func TestSourceLabels(t *testing.T) {
	for source, want := range map[pagemeta.Source]string{
		pagemeta.FromFrontmatter: "frontmatter",
		pagemeta.FromManifest:    "markfluence.yaml",
		pagemeta.FromBoth:        "frontmatter and markfluence.yaml",
		pagemeta.Unmanaged:       "",
	} {
		if got := sourceLabel(source); got != want {
			t.Errorf("sourceLabel(%q) = %q, want %q", source, got, want)
		}
	}
}

// A field both locations supply reports both, end to end.
func TestReportNamesBothLocations(t *testing.T) {
	dir := projectDir(t,
		"pages:\n  runbook.md:\n    title: Stale\n    page_id: 1234567890\n",
		map[string]string{
			"runbook.md": "---\ntitle: Stale\npage_id: 1234567890\n---\n\nHello.\n",
		})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", title: "Live Title"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "(frontmatter and markfluence.yaml)") {
		t.Errorf("the report does not name both locations:\n%s", o.stderr)
	}
}

// The project file's page_width is a real declaration (#100) -- update asserts
// it on a file that declares none -- so diff compares it, and names it
// distinctly from a pages: entry because it is a different place in the file.
func TestProjectDefaultWidthIsCompared(t *testing.T) {
	dir := projectDir(t, "page_width: narrow\n", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", width: "full-width"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr:\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stderr, "(markfluence.yaml (project default))") {
		t.Errorf("the project default was not named as such:\n%s", o.stderr)
	}
}

// Trouble exits 2, not 1: 1 is spent on "differs". Every class of it.
func TestTroubleExitsTwo(t *testing.T) {
	cases := []struct {
		name  string
		cfg   string
		files map[string]string
		file  string
		want  string
	}{
		{
			name:  "no page_id",
			files: map[string]string{"a.md": "---\ntitle: A\n---\n\nHello.\n"},
			file:  "a.md",
			want:  "names no page",
		},
		{
			name:  "non-numeric page_id",
			files: map[string]string{"a.md": "---\npage_id: https://x/pages/1\n---\n\nHello.\n"},
			file:  "a.md",
			want:  "not a numeric page id",
		},
		{
			name:  "unterminated frontmatter",
			files: map[string]string{"a.md": "---\ntitle: A\n"},
			file:  "a.md",
			want:  "frontmatter",
		},
		{
			name:  "coordinate disagreement",
			cfg:   "pages:\n  a.md:\n    page_id: 999\n",
			files: map[string]string{"a.md": "---\npage_id: 1234567890\n---\n\nHello.\n"},
			file:  "a.md",
			want:  "disagree about where this page is",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := projectDir(t, tc.cfg, tc.files)
			o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, tc.file)
			if o.exit != 2 {
				t.Errorf("exit = %d, want 2; stderr:\n%s", o.exit, o.stderr)
			}
			if !strings.Contains(o.stderr, tc.want) {
				t.Errorf("stderr does not mention %q:\n%s", tc.want, o.stderr)
			}
			if o.stdout != "" {
				t.Errorf("trouble wrote to stdout:\n%s", o.stdout)
			}
		})
	}
}

// A page that is gone names the page, so it is a results[0] failure under
// --json -- and still exits 2.
func TestMissingPageExitsTwo(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"a.md": "---\npage_id: 1234567890\n---\n\nHello.\n",
	})
	c := clienttest.New(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		// A body whose *title* names the target. A bare "title":"Not Found" is
		// how a rejected credential looks on a v2 route, and would be reported
		// as AUTH rather than as a missing page.
		_, _ = w.Write([]byte(`{"errors":[{"title":"Cannot find a page with id 1234567890"}]}`))
	})

	cmd := testCmd(t, c.SiteURL())
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(wd) }()

	err := run(cmd, []string{"a.md"})
	if err == nil || ui.ExitCode(err) != 2 {
		t.Errorf("exit = %v, want 2", err)
	}
}

// A file that cannot be read and a file whose frontmatter cannot be parsed both
// exit 2, but they are not the same failure: --json has to say IO for one and
// VALIDATION for the other.
func TestUnreadableFileIsIO(t *testing.T) {
	dir := projectDir(t, "", nil)
	o := runDiff(t, pageStub{body: "<p>x</p>"}, dir, "nope.md")
	if o.exit != 2 {
		t.Errorf("exit = %d, want 2", o.exit)
	}
	if !strings.Contains(o.stderr, "nope.md") {
		t.Errorf("stderr does not name the file:\n%s", o.stderr)
	}
}

// A labels: value markfluence refuses yields no comparable row at all, and the
// warning was gated on there being one -- so `diff` reported nothing and exited
// 0 about a file that cannot be published and whose labels disagree.
func TestRefusedLabelsValueIsReported(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		// A scalar, which update rejects: labels is destructive, so `labels:`
		// with nothing after it must not read as a one-element list.
		"runbook.md": "---\npage_id: 1234567890\nlabels: runbook\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", labels: []string{"other"}}, dir, "runbook.md")

	if !strings.Contains(o.stderr, "labels could not be compared") {
		t.Errorf("a refused labels: value was reported as nothing:\nstderr: %q\nstdout: %q",
			o.stderr, o.stdout)
	}
}

// A label Declared repaired travels as a warning, because the row it produces
// compares the repaired name and so looks like it agrees with a file that says
// something else.
func TestCaseRepairedLabelWarns(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\nlabels: [Runbook]\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", labels: []string{"runbook"}}, dir, "runbook.md")

	if o.exit != 0 {
		t.Errorf("exit = %d, want 0: Confluence lowercases labels, so these agree", o.exit)
	}
	if !strings.Contains(o.stderr, "Runbook") {
		t.Errorf("the case repair was not reported:\n%s", o.stderr)
	}
}

// The heading has to agree with the exit code: a run reporting only
// uncomparable rows exits 0, and "frontmatter differs" over the top of that is
// the command contradicting itself.
func TestHeadingMatchesTheExitCode(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\nlabels: [runbook]\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: "<p>Hello.</p>", labelsFail: true}, dir, "runbook.md")

	if o.exit != 0 {
		t.Fatalf("exit = %d, want 0", o.exit)
	}
	if strings.Contains(o.stderr, "frontmatter differs") {
		t.Errorf("stderr claims a difference on a run that exits 0:\n%s", o.stderr)
	}
	if !strings.Contains(o.stderr, "could not be fully compared") {
		t.Errorf("heading does not say what happened:\n%s", o.stderr)
	}
}

// A page with an empty body is a real shape -- what create leaves for a
// body-less file, and what a parent page often is -- not a folder. Refusing it
// blamed the wrong thing and made the file undiffable.
func TestEmptyPageBodyIsComparable(t *testing.T) {
	dir := projectDir(t, "", map[string]string{
		"runbook.md": "---\npage_id: 1234567890\n---\n\nHello.\n",
	})
	o := runDiff(t, pageStub{body: ""}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1 (the file has a body, the page has none):\n%s", o.exit, o.stderr)
	}
	if !strings.Contains(o.stdout, "+Hello.") {
		t.Errorf("the file's body is not reported as an addition:\n%s", o.stdout)
	}
}

// With no markfluence.yaml anywhere, project.Discover falls back to the file's
// own directory, so a root-relative label would drop the docs/ the reader typed
// and the patch would only apply from that subdirectory.
func TestLabelKeepsTheTypedPathWithNoProjectFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "runbook.md"),
		[]byte("---\npage_id: 1234567890\n---\n\nEdited.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := runDiff(t, pageStub{body: "<p>Hello.</p>"}, dir, "docs/runbook.md")

	if !strings.Contains(o.stdout, "--- confluence/docs/runbook.md") {
		t.Errorf("the label dropped the directory:\n%s", o.stdout)
	}
}
