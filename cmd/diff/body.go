package diff

// The body half: the two documents that are diffed, and the unified diff
// itself.

import (
	"strings"

	udiff "github.com/aymanbagabas/go-udiff"
)

// bodyDiff is the result of comparing the two documents.
type bodyDiff struct {
	// Text is the unified diff, uncoloured, empty when the bodies agree.
	Text string
	// Added and Removed count changed lines, excluding the ---/+++ header.
	Added, Removed int
}

// Differs reports whether the bodies disagree.
func (d bodyDiff) Differs() bool { return d.Text != "" }

// documents builds the two texts to diff: the file as it is on disk, and the
// same file with the page's rendered body in place of its own.
//
// The frontmatter block is shared **byte for byte** between them rather than
// being diffed, which buys three things:
//
//   - Hunk line numbers come out file-relative. Diffing the bodies alone would
//     number them from the body's first line, and `patch` would apply them with
//     a "Hunk #1 succeeded at 23 (offset 18 lines)" fudge.
//   - No hunk can touch the frontmatter, since it is identical context. A
//     patch can therefore never rewrite a page_id, and the frontmatter half is
//     reported separately where it can carry provenance a hunk cannot express.
//   - A file with no frontmatter block needs no special case: the prefix is
//     empty and the numbers are already right.
//
// local is mf.Content **exactly**, never normalized, because a patch has to
// apply to the real file: padding a missing final newline would make the last
// hunk's context a line the file does not have. The rendered side, being
// synthetic, is given one trailing newline, and a file that genuinely lacks one
// diffs against it with the "\ No newline at end of file" marker that unified
// diff has for exactly this and that `patch` understands.
func documents(content, body, rendered string) (confluence, local string) {
	// Everything up to and including the closing delimiter line. Empty for a
	// file with no frontmatter, where Body is the whole content.
	prefix := content[:len(content)-len(body)]
	// The newlines the author left between the delimiter and the first line of
	// prose belong to the shared prefix, not to either body: a file with two
	// blank lines there must not diff against a rendered side with one.
	lead := body[:len(body)-len(strings.TrimLeft(body, "\n"))]

	rendered = strings.TrimLeft(rendered, "\n")
	if !strings.HasSuffix(rendered, "\n") {
		rendered += "\n"
	}
	return prefix + lead + rendered, content
}

// compare diffs the two documents, labelling the sides for `patch -p1`.
//
// The labels are paths in the git a/-b/ convention, so -p1 strips the first
// component and lands on the real file, and so the output names itself when it
// is pasted into an issue.
//
// reverse swaps the sides, which is the difference between `patch -R -p1` and a
// plain `patch -p1` for someone pulling the page's edits into their file.
func compare(confluence, local, file string, reverse bool) (bodyDiff, error) {
	from, to := "confluence/"+file, "local/"+file
	before, after := confluence, local
	if reverse {
		from, to = to, from
		before, after = after, before
	}

	text, err := udiff.ToUnified(from, to, before, udiff.Strings(before, after), udiff.DefaultContextLines)
	if err != nil {
		return bodyDiff{}, err
	}

	d := bodyDiff{Text: text}
	for _, line := range strings.Split(text, "\n") {
		switch classify(line) {
		case lineAdded:
			d.Added++
		case lineRemoved:
			d.Removed++
		}
	}
	return d, nil
}

// lineKind is what one line of a unified diff is, which decides how it is
// coloured and whether it counts.
type lineKind int

const (
	lineContext lineKind = iota
	lineAdded
	lineRemoved
	lineHunk
	lineHeader
)

// classify reads one line of unified-diff text.
//
// Done on the rendered text rather than on udiff's structured hunks
// deliberately: re-rendering hunks would mean reimplementing the `@@ -a,b +c,d`
// arithmetic, which is the surface the issue picked a library to avoid getting
// subtly wrong. So the library's own output is the canonical string, and this
// only decides what colour each line is and whether it counts -- which cannot
// disagree with what is printed, because it is what is printed.
//
// The three-character checks come first: a `+++`/`---` header would otherwise
// read as an added or removed line and inflate every count by one.
func classify(line string) lineKind {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		return lineHeader
	case strings.HasPrefix(line, "@@"):
		return lineHunk
	case strings.HasPrefix(line, "+"):
		return lineAdded
	case strings.HasPrefix(line, "-"):
		return lineRemoved
	default:
		// Context, and the "\ No newline at end of file" marker, which belongs
		// to whichever side it follows and is not itself a change.
		return lineContext
	}
}
