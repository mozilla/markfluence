package diff

// Human output. Two streams, deliberately: see the package comment.

import (
	"fmt"
	"os"
	"strings"

	"github.com/mozilla/markfluence/internal/ui"
)

// styler colours the three kinds of line this command prints.
//
// A parameter rather than a direct call to ui, for the reason cmd/search's
// renderSpans documents: tests run with stdout not a terminal, where lipgloss
// emits nothing at all, so a test wired to the real style would pass against
// unstyled text and prove nothing. Injecting a marking styler is what lets a
// test assert that the right lines are marked.
type styler struct {
	added   func(string) string
	removed func(string) string
	meta    func(string) string
}

// termStyler is what a real invocation uses. lipgloss renders unstyled when
// colour is off (NO_COLOR, --no-color, or stdout not a terminal), so a piped
// patch stays a patch -- and --json reports bodyDiff.Text, which never goes
// through a styler at all.
var termStyler = styler{added: ui.DiffAdded, removed: ui.DiffRemoved, meta: ui.DiffHunk}

// report writes the whole human report: the frontmatter half and any warnings
// to stderr, the body diff to stdout.
//
// Nothing at all when the two sides agree. `diff` prints nothing for identical
// files and says so with its exit code; a "no differences" line on stdout would
// also be a line in somebody's .diff file.
func report(r result) {
	if block := frontmatterReport(r, termStyler); block != "" {
		_, _ = fmt.Fprint(os.Stderr, block)
	}
	for _, w := range r.warnings {
		ui.Warn(w)
	}
	if r.body.Differs() {
		_, _ = fmt.Fprint(os.Stdout, renderDiff(r.body.Text, termStyler))
		// Only when there is a diff to misread. A note printed on an
		// in-sync run is a line people learn to scroll past, and then it is
		// not there when it matters.
		ui.Hint("some differences are round-trip artefacts rather than edits: " +
			"markfluence diff --help")
	}
}

// frontmatterReport builds the per-field block, or "" when nothing differs.
//
// A string rather than writes to a stream, so a test can read it without
// capturing a file descriptor -- and because it is a block on *stderr*, which
// ui has only Hint for and which is not what these lines are: they are the
// command's primary answer for the frontmatter half.
func frontmatterReport(r result, s styler) string {
	if len(r.fields) == 0 {
		return ""
	}

	var b strings.Builder
	fmt.Fprintf(&b, "frontmatter differs (%s -> page %s):\n", r.file, r.page.ID)
	width := 0
	for _, d := range r.fields {
		if len(d.Field) > width {
			width = len(d.Field)
		}
	}
	for _, d := range r.fields {
		pad := strings.Repeat(" ", width-len(d.Field))
		gap := strings.Repeat(" ", width)
		confluence := quote(d.Confluence)
		if !d.Comparable {
			confluence = "(could not be read)"
		}
		fmt.Fprintf(&b, "  %s%s  confluence  %s\n", d.Field, pad, s.removed(confluence))
		fmt.Fprintf(&b, "  %s  local       %s%s\n", gap, s.added(quote(d.Local)), source(d))
		if d.Note != "" {
			fmt.Fprintf(&b, "  %s  %s\n", gap, d.Note)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// source is the parenthesised provenance: which file to go and edit. Omitted
// when there is nothing to say, which is a field nothing declared -- reachable
// only for a project-file default whose label carries its own wording.
func source(d difference) string {
	if d.Source == "" {
		return ""
	}
	return "  (" + d.Source + ")"
}

// quote spells a value for the report. A list is already bracketed and reads
// worse quoted; a scalar is quoted so that a trailing space, an empty string
// and the literal word null are all visible.
func quote(v string) string {
	if strings.HasPrefix(v, "[") {
		return v
	}
	return fmt.Sprintf("%q", v)
}

// renderDiff colours a unified diff line by line.
//
// Line by line over the library's own output rather than re-rendering its
// structured hunks: the @@ -a,b +c,d arithmetic is exactly what a library was
// chosen to get right, so what is printed stays byte-identical to what it
// produced, with colour laid on top.
func renderDiff(text string, s styler) string {
	if text == "" {
		return ""
	}
	// A unified diff ends with a newline, so the split leaves a trailing empty
	// element; rebuilding with Join puts the newline back and adds none.
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		switch classify(line) {
		case lineAdded:
			lines[i] = s.added(line)
		case lineRemoved:
			lines[i] = s.removed(line)
		case lineHunk, lineHeader:
			lines[i] = s.meta(line)
		}
	}
	return strings.Join(lines, "\n")
}
