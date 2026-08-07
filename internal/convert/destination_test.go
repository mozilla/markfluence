package convert

import "testing"

// TestImageSrcRoundTrip is the codec's core guarantee: every filesystem path
// encodes to a destination that decodes back to exactly that path. Without it,
// publishing an image and reading the page back would rename its source file.
func TestDestinationRoundTrip(t *testing.T) {
	cases := []struct {
		path string
		dest string
	}{
		{"x.png", "x.png"},
		{"assets/x.png", "assets/x.png"},
		{"a/b/c/deep.png", "a/b/c/deep.png"},

		// The case issue #20 is about: a space is not legal in a destination.
		{"my image.png", "my%20image.png"},
		{"assets/my image.png", "assets/my%20image.png"},

		// The same codec resolves links to sibling docs (issue #62), so ".md"
		// destinations round-trip identically.
		{"my doc.md", "my%20doc.md"},
		{"docs/my doc.md", "docs/my%20doc.md"},

		// Non-ASCII. Broken before decoding existed, for the same reason.
		{"café.png", "caf%C3%A9.png"},
		{"图片.png", "%E5%9B%BE%E7%89%87.png"},

		// The escape character itself, and a path that merely looks encoded.
		{"100%.png", "100%25.png"},
		{"my%20image.png", "my%2520image.png"},

		// An unbalanced parenthesis would end the destination early, so both
		// are escaped -- url.PathEscape leaves them alone.
		{"shot (1).png", "shot%20%281%29.png"},
		{"weird).png", "weird%29.png"},

		// "?" and "#" would otherwise start a query or fragment.
		{"what?.png", "what%3F.png"},
		{"a#b.png", "a%23b.png"},

		// A shared asset above the page is a supported layout: ".." must survive
		// as a path segment rather than being escaped into meaninglessness.
		{"../assets/logo.png", "../assets/logo.png"},
		{"../my shared/logo.png", "../my%20shared/logo.png"},
	}
	for _, c := range cases {
		if got := encodeDestination(c.path); got != c.dest {
			t.Errorf("encodeDestination(%q) = %q, want %q", c.path, got, c.dest)
		}
		if got := decodeDestination(c.dest); got != c.path {
			t.Errorf("decodeDestination(%q) = %q, want %q", c.dest, got, c.path)
		}
	}
}

// TestDecodeImageSrcAcceptsInvalidEscapes covers the destinations that are not
// valid percent-encoding at all. A literal "%" is legal in a filename and
// nobody encoded it, so these are paths as written, not errors.
func TestDecodeDestinationAcceptsInvalidEscapes(t *testing.T) {
	for _, s := range []string{"100%.png", "%.png", "50%off.png", "a%zz.png", "trailing%"} {
		if got := decodeDestination(s); got != s {
			t.Errorf("decodeDestination(%q) = %q, want it unchanged", s, got)
		}
	}
}

// TestEncodeImageSrcNeverYieldsABareSpace guards the property that actually
// broke export: a destination containing a space does not parse as an image,
// so whatever the recorded source path was, the encoded form must be spaceless.
func TestEncodeDestinationNeverYieldsABareSpace(t *testing.T) {
	for _, p := range []string{"my image.png", "a/b c/d e.png", " leading.png", "trailing .png"} {
		got := encodeDestination(p)
		for _, r := range got {
			if r == ' ' || r == '\t' || r == '\n' {
				t.Errorf("encodeDestination(%q) = %q, which contains whitespace", p, got)
				break
			}
		}
		if decodeDestination(got) != p {
			t.Errorf("encodeDestination(%q) = %q does not round-trip", p, got)
		}
	}
}
