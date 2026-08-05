package convert

import "testing"

// TestAttachmentNameRoundTrip is the core guarantee: every source path encodes to
// an attachment name that decodes back to exactly that path. The cases include
// the ones a naive "/" -> "__" substitution gets wrong.
func TestAttachmentNameRoundTrip(t *testing.T) {
	cases := []struct {
		src  string
		name string
	}{
		{"x.png", "x.png"},
		{"assets/x.png", "assets%2Fx.png"},
		{"a/b/c/deep.png", "a%2Fb%2Fc%2Fdeep.png"},

		// "_" is ordinary text, so a path and a name that merely looks flattened
		// stay distinct -- the collision the old "/" -> "_" encoding produced.
		{"a/b.png", "a%2Fb.png"},
		{"a_b.png", "a_b.png"},

		// A leading "__" must not decode to an absolute path.
		{"__a.png", "__a.png"},
		// A component ending in "_" must not shift the separator.
		{"a_/b.png", "a_%2Fb.png"},
		// A literal "%2F" in the filename must not decode to a separator.
		{"a%2Fb.png", "a%252Fb.png"},
		// The escape character itself.
		{"100%.png", "100%25.png"},
		{"%25.png", "%2525.png"},

		// A shared asset above the page is a supported layout.
		{"../assets/logo.png", "..%2Fassets%2Flogo.png"},

		// Spaces and other characters are left alone -- only "/" is illegal.
		{"my docs/a b.png", "my docs%2Fa b.png"},
	}
	for _, c := range cases {
		if got := attachmentFilename(c.src); got != c.name {
			t.Errorf("attachmentFilename(%q) = %q, want %q", c.src, got, c.name)
		}
		got, ok := attachmentSource(c.name)
		if !ok {
			t.Errorf("attachmentSource(%q) refused a name we produced", c.name)
			continue
		}
		if got != c.src {
			t.Errorf("attachmentSource(%q) = %q, want %q (round trip)", c.name, got, c.src)
		}
	}
}

// TestAttachmentFilenameIsInjective is what makes the dedupe in renderImage sound:
// no two distinct sources may share one attachment name.
func TestAttachmentFilenameIsInjective(t *testing.T) {
	srcs := []string{
		"a/b.png", "a_b.png", "a__b.png", "a%2Fb.png", "__a.png", "a_/b.png",
		"x.png", "assets/x.png", "../x.png", "100%.png",
	}
	seen := map[string]string{}
	for _, src := range srcs {
		name := attachmentFilename(src)
		if prev, dup := seen[name]; dup {
			t.Errorf("%q and %q both encode to %q", prev, src, name)
		}
		seen[name] = src
	}
}

// TestAttachmentFilenameNormalizes folds equivalent spellings of one path onto a
// single name, so the same file is not uploaded twice under different names.
func TestAttachmentFilenameNormalizes(t *testing.T) {
	cases := []struct{ src, want string }{
		{"./x.png", "x.png"},
		{"./assets/x.png", "assets%2Fx.png"},
		{"assets/./x.png", "assets%2Fx.png"},
		{"assets/../assets/x.png", "assets%2Fx.png"},
		// Resolution joins src onto the page directory, so a leading "/" was never
		// really absolute; dropping it keeps names from decoding to absolute paths.
		{"/assets/x.png", "assets%2Fx.png"},
	}
	for _, c := range cases {
		if got := attachmentFilename(c.src); got != c.want {
			t.Errorf("attachmentFilename(%q) = %q, want %q", c.src, got, c.want)
		}
	}
}

// TestAttachmentSourceRefusesAbsolute covers names markfluence never produces:
// a hand-uploaded attachment must not be able to steer a reader at an absolute
// path (which is what #37's export would then write to).
func TestAttachmentSourceRefusesAbsolute(t *testing.T) {
	for _, name := range []string{"%2Fetc%2Fpasswd.png", "%2F.png", ""} {
		if got, ok := attachmentSource(name); ok {
			t.Errorf("attachmentSource(%q) = %q, true; want refusal", name, got)
		}
	}
}

// TestAttachmentSourceDecodesForeignNames documents best-effort behavior for
// attachments markfluence did not upload: they decode like any other name, since
// there is no way to tell them apart.
func TestAttachmentSourceDecodesForeignNames(t *testing.T) {
	for _, c := range []struct{ name, want string }{
		{"hand-uploaded.png", "hand-uploaded.png"},
		{"screenshot 2026.png", "screenshot 2026.png"},
		{"..%2Fup.png", "../up.png"},
	} {
		got, ok := attachmentSource(c.name)
		if !ok || got != c.want {
			t.Errorf("attachmentSource(%q) = %q, %v; want %q, true", c.name, got, ok, c.want)
		}
	}
}
