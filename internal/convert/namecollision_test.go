package convert_test

// Two assets in one file wanting one attachment name. The name is the base name
// (attachname.go), so this is reachable whenever two directories hold a
// same-named image -- and it is refused rather than reported, because there is
// no correct way to publish it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

// convertBody renders body as root/main.md, returning the page or the error.
func convertBody(t *testing.T, root, body string, images ...string) (*convert.ConfluencePage, error) {
	t.Helper()
	for _, img := range images {
		path := filepath.Join(root, filepath.FromSlash(img))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("PNG"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	md, err := frontmatter.Parse(filepath.Join(root, "main.md"), body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := project.FromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.FS.Close() }()
	return convert.MdToConfluence(md, r, testIndex(t, r), "https://wiki.example.net", "ENG", "vtest")
}

// TestRefusesTwoAssetsWithOneName is the refusal itself. It names both paths,
// with the line each was written on, because "diagram.png is ambiguous" would
// leave the author hunting for the second one.
func TestRefusesTwoAssetsWithOneName(t *testing.T) {
	root := t.TempDir()
	body := "intro\n\n![arch](arch/diagram.png)\n\nmore\n\n![deploy](deploy/diagram.png)\n"

	page, err := convertBody(t, root, body, "arch/diagram.png", "deploy/diagram.png")
	if err == nil {
		t.Fatalf("want a refusal, got a page: %s", page.HTML)
	}
	for _, want := range []string{
		"arch/diagram.png", "deploy/diagram.png", `"diagram.png"`, "line 3: ", "line 7: ",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestRefusalIsNotMerelyBroken guards the distinction the whole refusal rests
// on: Broken is reported and published anyway (nothing in cmd/update blocks on
// it), so a collision reported that way would put the page up with one image
// rendered twice and one of the two paths recorded.
func TestRefusalIsNotMerelyBroken(t *testing.T) {
	root := t.TempDir()
	body := "![a](a/x.png)\n\n![b](b/x.png)\n"

	if _, err := convertBody(t, root, body, "a/x.png", "b/x.png"); err == nil {
		t.Fatal("a name collision must fail the conversion, not report a Broken entry")
	}
}

// TestSameAssetTwiceIsOneAttachment is the case the refusal must not catch: one
// image referenced twice is one upload, which is why seen compares source paths
// rather than only names.
func TestSameAssetTwiceIsOneAttachment(t *testing.T) {
	root := t.TempDir()
	body := "![one](assets/x.png)\n\n![again](assets/x.png)\n\n![spelled](./assets/x.png)\n"

	page, err := convertBody(t, root, body, "assets/x.png")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if len(page.Attachments) != 1 {
		t.Fatalf("attachments = %v, want exactly one", page.Attachments)
	}
	if got := page.Attachments[0].Filename; got != "x.png" {
		t.Errorf("filename = %q, want x.png", got)
	}
	if got := strings.Count(page.HTML, `ri:filename="x.png"`); got != 3 {
		t.Errorf("body references the attachment %d times, want 3", got)
	}
}

// TestSameNameInDifferentFilesIsFine keeps the refusal scoped to one page.
// Attachment names are unique per page, so two pages may each carry a
// diagram.png -- and a renderer is built per conversion, so nothing leaks.
func TestSameNameInDifferentFilesIsFine(t *testing.T) {
	root := t.TempDir()
	for _, src := range []string{"arch/diagram.png", "deploy/diagram.png"} {
		if _, err := convertBody(t, root, "![d]("+src+")\n", src); err != nil {
			t.Errorf("converting %s alone: %v", src, err)
		}
	}
}

// TestWarnsWhenAConvertedImageTakesAPastedName covers the reference the shield
// hides from renderImage: raw storage naming an attachment that a converted
// image is about to publish over. Warned, not refused -- a pasted reference may
// legitimately mean this very attachment -- but not silent, since publishing
// changes what a part of the page the author never edited displays.
func TestWarnsWhenAConvertedImageTakesAPastedName(t *testing.T) {
	root := t.TempDir()
	body := "<ac:image><ri:attachment ri:filename=\"diagram.png\" /></ac:image>\n\n" +
		"![arch](arch/diagram.png)\n"

	page, err := convertBody(t, root, body, "arch/diagram.png")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if len(page.Warnings) != 1 || !strings.Contains(page.Warnings[0], "raw storage") {
		t.Fatalf("warnings = %v, want one naming the raw-storage reference", page.Warnings)
	}
	if !strings.Contains(page.Warnings[0], "arch/diagram.png") {
		t.Errorf("warning %q does not name the image's path", page.Warnings[0])
	}
}

// TestNoWarningWhenPastedNamesDoNotCollide keeps the scan from firing on the
// ordinary case: a page mixing pasted storage and images that share no name.
func TestNoWarningWhenPastedNamesDoNotCollide(t *testing.T) {
	root := t.TempDir()
	body := "<ac:image><ri:attachment ri:filename=\"native.png\" /></ac:image>\n\n" +
		"![arch](arch/diagram.png)\n"

	page, err := convertBody(t, root, body, "arch/diagram.png")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if len(page.Warnings) != 0 {
		t.Errorf("warnings = %v, want none", page.Warnings)
	}
}
