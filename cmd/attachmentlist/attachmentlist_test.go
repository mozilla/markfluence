package attachmentlist

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func TestHumanSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{171, "171 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{24680, "24.1 KB"},
		{1024 * 1024, "1.0 MB"},
		{1258291, "1.2 MB"},
		{1024 * 1024 * 1024, "1.0 GB"},
		{1024 * 1024 * 1024 * 1024, "1.0 TB"},
	}
	for _, c := range cases {
		if got := humanSize(c.n); got != c.want {
			t.Errorf("humanSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestTable(t *testing.T) {
	managed := client.Attachment{ID: "att1", Title: "assets%2Fx.png"}
	managed.Metadata.Comment = "markfluence: sha256=abc path=assets/x.png"
	managed.Version.Number = 3
	managed.Extensions.MediaType = "image/png"
	managed.Extensions.FileSize = 24680

	hand := client.Attachment{ID: "att2", Title: "notes.pdf"}
	hand.Version.Number = 1
	hand.Extensions.MediaType = "application/pdf"
	hand.Extensions.FileSize = 171

	got := table([]client.Attachment{managed, hand})
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header plus 2 rows:\n%s", len(lines), got)
	}
	if !strings.HasPrefix(lines[0], "NAME") || !strings.Contains(lines[0], "SOURCE") {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.Contains(lines[1], "assets/x.png") {
		t.Errorf("managed row is missing its source: %q", lines[1])
	}
	// A hand-uploaded attachment reads as a dash, not a blank.
	if !strings.HasSuffix(lines[2], "-") {
		t.Errorf("hand-uploaded row should end in a dash: %q", lines[2])
	}
	for i, l := range lines {
		if l != strings.TrimRight(l, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, l)
		}
	}
}

// TestTableAlignsColumns checks the columns actually line up, which is the only
// reason to measure widths at all.
func TestTableAlignsColumns(t *testing.T) {
	short := client.Attachment{ID: "a", Title: "a.png"}
	short.Extensions.MediaType = "image/png"
	long := client.Attachment{ID: "b", Title: "a-considerably-longer-name.png"}
	long.Extensions.MediaType = "image/png"

	lines := strings.Split(table([]client.Attachment{short, long}), "\n")
	col := strings.Index(lines[0], "SIZE")
	for i, l := range lines[1:] {
		if strings.Index(l, "B") < col {
			t.Errorf("row %d size column starts before the header's: %q", i, l)
		}
	}
}
