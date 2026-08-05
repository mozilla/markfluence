package read

import "testing"

func TestRenderFrontmatter(t *testing.T) {
	// Fields come out in the canonical order (title, space, parent, page_id, then
	// the rest) regardless of the order renderFrontmatter writes them.
	got := renderFrontmatter("My Page", "ENG", "456", "123456", "max")
	want := "---\ntitle: My Page\nspace: ENG\nparent: 456\npage_id: 123456\npage_width: max\n---\n"
	if got != want {
		t.Errorf("renderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterTopLevelParent(t *testing.T) {
	// A top-level page carries parent: null.
	got := renderFrontmatter("T", "ENG", "null", "1", "max")
	want := "---\ntitle: T\nspace: ENG\nparent: null\npage_id: 1\npage_width: max\n---\n"
	if got != want {
		t.Errorf("renderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterOmitsEmptyFields(t *testing.T) {
	got := renderFrontmatter("T", "", "", "1", "")
	want := "---\ntitle: T\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("renderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFrontmatterQuotesWhenNeeded(t *testing.T) {
	// A title with a leading '#' would be read as a comment unless quoted.
	got := renderFrontmatter("# Sharp", "", "", "1", "")
	want := "---\ntitle: '# Sharp'\npage_id: 1\n---\n"
	if got != want {
		t.Errorf("renderFrontmatter =\n%q\nwant\n%q", got, want)
	}
}
