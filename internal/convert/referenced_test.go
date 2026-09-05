package convert_test

// The storage-side scan: what attachment names a page refers to. It lives in
// internal/convert because both directions need it -- `export` asks it of a
// page it fetched, and the converter asks it of the raw storage a markdown body
// pastes through -- and two copies of "what does this page reference" would be
// two things to keep in step.

import (
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
)

// TestReferencedAttachmentNamesMatchesEveryContext is why the scan reads raw storage
// rather than walking the parsed tree: the converter special-cases ac:image
// only, so an attachment link's target or a reference inside a pass-through
// macro would be dropped from the export.
func TestReferencedAttachmentNamesMatchesEveryContext(t *testing.T) {
	const storage = `<p><ac:image><ri:attachment ri:filename="x.png" /></ac:image></p>` +
		`<p><ac:link><ri:attachment ri:filename="spec.pdf" /></ac:link></p>` +
		`<ac:structured-macro ac:name="viewfile">` +
		`<ac:parameter ac:name="name"><ri:attachment ri:filename="data.csv" /></ac:parameter>` +
		`</ac:structured-macro>`

	got := convert.ReferencedAttachmentNames(storage)
	for _, want := range []string{"x.png", "spec.pdf", "data.csv"} {
		if !got[want] {
			t.Errorf("%q not detected as referenced", want)
		}
	}
	if len(got) != 3 {
		t.Errorf("got %d names, want 3: %v", len(got), got)
	}
}

func TestReferencedAttachmentNamesEmptyBody(t *testing.T) {
	if got := convert.ReferencedAttachmentNames("<p>nothing here</p>"); len(got) != 0 {
		t.Errorf("got %v, want no references", got)
	}
}

// TestReferencedAttachmentNamesUnescapes covers a name carrying an XML entity: storage is
// XHTML, so an ampersand in a filename arrives escaped and must be compared
// against the attachment title in its decoded form.
func TestReferencedAttachmentNamesUnescapes(t *testing.T) {
	got := convert.ReferencedAttachmentNames(`<ri:attachment ri:filename="a&amp;b.png" />`)
	if !got["a&b.png"] {
		t.Errorf("got %v, want the decoded a&b.png", got)
	}
}
