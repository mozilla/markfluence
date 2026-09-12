package pagedoc

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func TestRenderFrontmatter(t *testing.T) {
	// Fields come out in the canonical order (title, space, parent, page_id, then
	// the rest) regardless of the order renderFrontmatter writes them.
	got := RenderFrontmatter("My Page", "ENG", "456", "123456", "max", nil)
	want := "---\ntitle: My Page\nspace: ENG\nparent: 456\npage_id: 123456\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterTopLevelParent(t *testing.T) {
	// A top-level page carries parent: null.
	got := RenderFrontmatter("T", "ENG", "null", "1", "max", nil)
	want := "---\ntitle: T\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterOmitsEmptyFields(t *testing.T) {
	got := RenderFrontmatter("T", "", "", "1", "", nil)
	want := "---\ntitle: T\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterQuotesWhenNeeded(t *testing.T) {
	// A title with a leading '#' would be read as a comment unless quoted.
	got := RenderFrontmatter("# Sharp", "", "", "1", "", nil)
	want := "---\ntitle: \"# Sharp\"\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

// --- Sources -----------------------------------------------------------------

func TestSourcesFrom(t *testing.T) {
	managed := client.Attachment{Title: "x.png"}
	managed.Metadata.Comment = "markfluence: sha256=abc path=assets/x.png"
	legacy := client.Attachment{Title: "assets_x.png"}
	legacy.Metadata.Comment = "mzcld:checksum: abc"
	hand := client.Attachment{Title: "notes.pdf"}

	got := SourcesFrom([]client.Attachment{managed, legacy, hand})
	if len(got) != 1 {
		t.Fatalf("got %d sources, want 1: %v", len(got), got)
	}
	if got["x.png"] != "assets/x.png" {
		t.Errorf("sources = %v", got)
	}
	// An attachment with no recorded source contributes nothing, so the converter
	// falls back to decoding its name rather than being handed a wrong answer.
	for _, absent := range []string{"assets_x.png", "notes.pdf"} {
		if _, ok := got[absent]; ok {
			t.Errorf("%s should not have a recorded source", absent)
		}
	}
}

// TestSourcesSkipsLookupWithoutReferences pins the optimization: a page with no
// attachment references must not trigger an API call. The nil client would
// panic if one were attempted.
func TestSourcesSkipsLookupWithoutReferences(t *testing.T) {
	page := &client.Page{ID: "1"}
	page.Body.Storage.Value = "<p>no attachments here</p>"
	if got := Sources(nil, page); got != nil {
		t.Errorf("Sources = %v, want nil without any ri:attachment", got)
	}
}

func TestDocString(t *testing.T) {
	d := Doc{Frontmatter: "---\ntitle: T\n---\n", Body: "# T\n"}
	if want := "---\ntitle: T\n---\n\n# T\n"; d.String() != want {
		t.Errorf("String() = %q, want %q", d.String(), want)
	}
}

// TestRenderFrontmatterEmitsLabels pins the field's place in the block: labels
// is not in fieldOrder, so it sorts alphabetically among the trailing keys and
// lands after page_id but before page_width.
func TestRenderFrontmatterEmitsLabels(t *testing.T) {
	got := RenderFrontmatter("T", "ENG", "null", "1", "max", []string{"ci/cd", "runbook"})
	want := "---\ntitle: T\nspace: ENG\nparent: null\npage_id: 1\nlabels: [ci/cd, runbook]\npage_width: max\n---\n"
	if got != want {
		t.Errorf("RenderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

// TestRenderFrontmatterOmitsEmptyLabels: a page with no labels, and a page
// whose labels could not be read, both get no labels: key.
//
// Not "labels: []", which is what update reads as "remove every label". A read
// of a page that has none must not assert that on the author's behalf -- the
// round trip would then strip any label added in the UI in between.
func TestRenderFrontmatterOmitsEmptyLabels(t *testing.T) {
	for name, given := range map[string][]string{
		"fetch failed": nil,
		"none on page": {},
	} {
		got := RenderFrontmatter("T", "", "", "1", "", given)
		if strings.Contains(got, "labels") {
			t.Errorf("%s: RenderFrontmatter = %q, want no labels key", name, got)
		}
	}
}

// TestRenderFrontmatterQuotesALabelThatNeedsIt: labels go through the same
// verified writer as every other value, so a name YAML would misread is
// quoted. No valid Confluence label needs this -- every character that would
// break a flow sequence is in the server's reject set -- which is exactly why
// it is worth pinning that the general path is still being used.
func TestRenderFrontmatterQuotesALabelThatNeedsIt(t *testing.T) {
	got := RenderFrontmatter("T", "", "", "1", "", []string{"a,b"})
	if !strings.Contains(got, `"a,b"`) {
		t.Errorf("RenderFrontmatter = %q, want the comma-bearing label quoted", got)
	}
}
