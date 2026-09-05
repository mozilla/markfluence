package convert

import "testing"

// TestAttachmentFilename pins the mapping: the name is the path's base name.
func TestAttachmentFilename(t *testing.T) {
	cases := []struct {
		src  string
		name string
	}{
		{"x.png", "x.png"},
		{"assets/x.png", "x.png"},
		{"a/b/c/deep.png", "deep.png"},

		// Nothing in the name is escaped any more, so characters that used to be
		// the encoding's business are ordinary text.
		{"a%2Fb.png", "a%2Fb.png"},
		{"100%.png", "100%.png"},
		{"%25.png", "%25.png"},

		// A shared asset above the page is a supported layout, and its name is
		// the file's, not the route taken to it.
		{"../assets/logo.png", "logo.png"},

		{"my docs/a b.png", "a b.png"},

		// No path, no name.
		{"", ""},
		{".", ""},
	}
	for _, c := range cases {
		if got := AttachmentFilename(c.src); got != c.name {
			t.Errorf("AttachmentFilename(%q) = %q, want %q", c.src, got, c.name)
		}
	}
}

// TestAttachmentFilenameCollidesOnBaseName documents the mapping's deliberate
// loss, so that a future reader finds it stated rather than discovers it.
//
// The old percent-encoding was injective and this is not: two assets in
// different directories share a name. That is refused where it can be seen with
// both paths in hand -- renderImage, for one page -- rather than designed around
// here, because the name is the attachment's identity and an encoded name churns
// every time a path moves. See _plans/029.
func TestAttachmentFilenameCollidesOnBaseName(t *testing.T) {
	for _, pair := range [][2]string{
		{"arch/diagram.png", "deploy/diagram.png"},
		{"x.png", "assets/x.png"},
		{"assets/x.png", "../x.png"},
	} {
		a, b := AttachmentFilename(pair[0]), AttachmentFilename(pair[1])
		if a != b {
			t.Errorf("AttachmentFilename(%q) = %q and (%q) = %q; expected them to collide",
				pair[0], a, pair[1], b)
		}
	}
}

// TestAttachmentFilenameNormalizes folds equivalent spellings of one path onto a
// single name, so the same file is not uploaded twice under different names.
func TestAttachmentFilenameNormalizes(t *testing.T) {
	cases := []struct{ src, want string }{
		{"./x.png", "x.png"},
		{"./assets/x.png", "x.png"},
		{"assets/./x.png", "x.png"},
		{"assets/../assets/x.png", "x.png"},
		// Resolution joins src onto the page directory, so a leading "/" was never
		// really absolute.
		{"/assets/x.png", "x.png"},
	}
	for _, c := range cases {
		if got := AttachmentFilename(c.src); got != c.want {
			t.Errorf("AttachmentFilename(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}
