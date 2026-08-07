package convert

import "testing"

// TestDocKey covers the lookup key the page and anchor maps are consulted with.
// Both are keyed by os.ReadDir's e.Name(), so a destination has to be decoded
// down to a bare filename before it will match. Getting this wrong is silent:
// the link is simply not rewritten, and a relative href that means nothing on
// Confluence is published with no warning.
func TestDocKey(t *testing.T) {
	cases := []struct {
		dest string
		key  string
	}{
		{"plain.md", "plain.md"},
		{"docs/plain.md", "plain.md"},
		{"../plain.md", "plain.md"},

		// The bug: a filename with a space is spelled with "%20".
		{"my%20doc.md", "my doc.md"},
		{"docs/my%20doc.md", "my doc.md"},
		{"./my%20doc.md", "my doc.md"},

		// Non-ASCII filenames encode the same way.
		{"caf%C3%A9.md", "café.md"},

		// A literal "%" in a filename is not an escape sequence, so an
		// undecodable destination is a filename as written.
		{"100%.md", "100%.md"},
		{"50%off.md", "50%off.md"},

		// A destination that merely looks encoded resolves to the literal name.
		{"my%2520doc.md", "my%20doc.md"},
	}
	for _, c := range cases {
		if got := docKey(c.dest); got != c.key {
			t.Errorf("docKey(%q) = %q, want %q", c.dest, got, c.key)
		}
	}
}

// TestDocKeyAgreesOnBothSpellings is the property that matters more than any
// single mapping: the two legal ways to write a destination containing a space
// have to reach the same entry, or one of them silently fails to resolve.
func TestDocKeyAgreesOnBothSpellings(t *testing.T) {
	for _, pair := range [][2]string{
		{"my%20doc.md", "my doc.md"},
		{"docs/my%20doc.md", "docs/my doc.md"},
		{"caf%C3%A9.md", "café.md"},
	} {
		encoded, literal := docKey(pair[0]), docKey(pair[1])
		if encoded != literal {
			t.Errorf("docKey(%q) = %q but docKey(%q) = %q; both spellings must agree",
				pair[0], encoded, pair[1], literal)
		}
	}
}
