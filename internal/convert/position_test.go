package convert_test

// Where an attachment's markdown destination points, given where the page's file
// sits. The four rows of _plans/029 §Layout, which is the table the L5
// round-trip rests on: a recorded path is relative to the root while a markdown
// destination is relative to the file carrying it, and those coincide only for a
// page at the top level.

import (
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
)

func TestAttachmentDestinationIsPositioned(t *testing.T) {
	const sourced = `<p><ac:image ac:alt="b"><ri:attachment ri:filename="brand.png" /></ac:image></p>`
	const native = `<p><ac:image ac:alt="d"><ri:attachment ri:filename="diagram.png" /></ac:image></p>`
	recorded := map[string]string{"brand.png": "assets/brand.png"}

	cases := []struct {
		name          string
		storage       string
		sources       map[string]string
		pageDir       string
		attachmentDir string
		want          string
	}{
		{
			name:    "recorded path, page below the root",
			storage: sourced, sources: recorded,
			pageDir: "home", attachmentDir: "home/child",
			want: "![b](../assets/brand.png)\n",
		},
		{
			name:    "recorded path, page at the root",
			storage: sourced, sources: recorded,
			pageDir: "", attachmentDir: "child",
			want: "![b](assets/brand.png)\n",
		},
		{
			name:    "no recorded path, page below the root",
			storage: native,
			pageDir: "home", attachmentDir: "home/child",
			want: "![d](child/diagram.png)\n",
		},
		{
			name:    "no recorded path, page at the root",
			storage: native,
			pageDir: "", attachmentDir: "child",
			want: "![d](child/diagram.png)\n",
		},
		{
			// What read passes when it has no tree at all, and what every caller
			// passed before positions existed.
			name:    "no position at all",
			storage: native,
			want:    "![d](diagram.png)\n",
		},
		{
			name:    "deeper page reaching further up",
			storage: sourced, sources: recorded,
			pageDir: "home/team/eng", attachmentDir: "home/team/eng/child",
			want: "![b](../../../assets/brand.png)\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := convert.StorageToMarkdown(c.storage, convert.StorageOptions{
				Sources:       c.sources,
				PageDir:       c.pageDir,
				AttachmentDir: c.attachmentDir,
			})
			if err != nil {
				t.Fatal(err)
			}
			if got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestPositionedDestinationSurvivesEncoding checks the two transforms compose in
// the right order: the path is made relative first and percent-encoded second,
// so a "../" prefix stays a path segment rather than becoming %2E%2E%2F.
func TestPositionedDestinationSurvivesEncoding(t *testing.T) {
	const storage = `<p><ac:image ac:alt="b"><ri:attachment ri:filename="my brand.png" /></ac:image></p>`
	got, err := convert.StorageToMarkdown(storage, convert.StorageOptions{
		Sources: map[string]string{"my brand.png": "shared assets/my brand.png"},
		PageDir: "home",
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := "![b](../shared%20assets/my%20brand.png)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
