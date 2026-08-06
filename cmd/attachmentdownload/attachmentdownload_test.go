package attachmentdownload

import (
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

// managed builds an attachment carrying a recorded source path.
func TestSelectAttachmentsAll(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}, {Title: "b.png"}}
	got, missing := selectAttachments(all, nil)
	if len(got) != 2 || len(missing) != 0 {
		t.Fatalf("got %d wanted / %d missing, want 2/0", len(got), len(missing))
	}
}

func TestSelectAttachmentsByNamePreservesRequestOrder(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}, {Title: "b.png"}, {Title: "c.png"}}
	got, missing := selectAttachments(all, []string{"c.png", "a.png"})
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want none", missing)
	}
	if got[0].Title != "c.png" || got[1].Title != "a.png" {
		t.Errorf("order = %q/%q, want c.png/a.png", got[0].Title, got[1].Title)
	}
}

func TestSelectAttachmentsReportsMissing(t *testing.T) {
	all := []client.Attachment{{Title: "a.png"}}
	got, missing := selectAttachments(all, []string{"a.png", "nope.png"})
	if len(got) != 1 {
		t.Errorf("wanted = %d, want 1", len(got))
	}
	if len(missing) != 1 || missing[0] != "nope.png" {
		t.Errorf("missing = %v, want [nope.png]", missing)
	}
}

// TestDestPathEscapeMessageNamesTheAttachment keeps the failure actionable: the
// user needs to know which attachment was refused.
