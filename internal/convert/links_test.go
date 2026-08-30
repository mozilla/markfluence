package convert

import (
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
)

// TestResolveDocKeyDecodesBeforeResolving covers the lookup key the link index
// is consulted with. The index is keyed by root-relative path as it appears on
// disk, so a destination has to be decoded down to that spelling before it will
// match. Getting this wrong is silent: the link is simply not rewritten, and a
// relative href that means nothing on Confluence is published -- though now, at
// least, with a warning (minimal R1).
func TestResolveDocKeyDecodesBeforeResolving(t *testing.T) {
	root := t.TempDir()
	r := &storageRenderer{baseDir: root, root: &project.Root{Dir: root}}

	cases := []struct {
		dest    string
		key     string
		escapes bool
	}{
		{"plain.md", "plain.md", false},
		{"docs/plain.md", "docs/plain.md", false},
		{"../plain.md", "../plain.md", true}, // escapes root -- returned as-is, not refused

		// The bug: a filename with a space is spelled with "%20".
		{"my%20doc.md", "my doc.md", false},
		{"docs/my%20doc.md", "docs/my doc.md", false},
		{"./my%20doc.md", "my doc.md", false},

		// Non-ASCII filenames encode the same way.
		{"caf%C3%A9.md", "café.md", false},

		// A literal "%" in a filename is not an escape sequence, so an
		// undecodable destination is a filename as written.
		{"100%.md", "100%.md", false},
		{"50%off.md", "50%off.md", false},

		// A destination that merely looks encoded resolves to the literal name.
		{"my%2520doc.md", "my%20doc.md", false},
	}
	for _, c := range cases {
		got, escapes := r.resolveDocKey(c.dest)
		if got != c.key {
			t.Errorf("resolveDocKey(%q) = %q, want %q", c.dest, got, c.key)
		}
		if escapes != c.escapes {
			t.Errorf("resolveDocKey(%q) escapes = %v, want %v", c.dest, escapes, c.escapes)
		}
	}
}

// TestResolveDocKeyAgreesOnBothSpellings is the property that matters more
// than any single mapping: the two legal ways to write a destination
// containing a space have to reach the same entry, or one of them silently
// fails to resolve.
func TestResolveDocKeyAgreesOnBothSpellings(t *testing.T) {
	root := t.TempDir()
	r := &storageRenderer{baseDir: root, root: &project.Root{Dir: root}}

	for _, pair := range [][2]string{
		{"my%20doc.md", "my doc.md"},
		{"docs/my%20doc.md", "docs/my doc.md"},
		{"caf%C3%A9.md", "café.md"},
	} {
		encoded, _ := r.resolveDocKey(pair[0])
		literal, _ := r.resolveDocKey(pair[1])
		if encoded != literal {
			t.Errorf("resolveDocKey(%q) = %q but resolveDocKey(%q) = %q; both spellings must agree",
				pair[0], encoded, pair[1], literal)
		}
	}
}

// TestResolveDocKeyDistinguishesSameBasenameInDifferentDirectories is
// Scenario A's fix, at the resolveDocKey level: two links spelled
// "overview.md" from two different directories must resolve to two different
// keys, one per directory -- not collide on the bare filename the way the old
// basename-only docKey did.
func TestResolveDocKeyDistinguishesSameBasenameInDifferentDirectories(t *testing.T) {
	root := t.TempDir()

	fromRoot := &storageRenderer{baseDir: root, root: &project.Root{Dir: root}}
	fromSub := &storageRenderer{baseDir: filepath.Join(root, "setup"), root: &project.Root{Dir: root}}

	top, _ := fromRoot.resolveDocKey("overview.md")
	nested, _ := fromSub.resolveDocKey("overview.md")
	if top == nested {
		t.Errorf("resolveDocKey(overview.md) from two directories collided on %q", top)
	}
	if want := "overview.md"; top != want {
		t.Errorf("top-level resolveDocKey = %q, want %q", top, want)
	}
	if want := "setup/overview.md"; nested != want {
		t.Errorf("nested resolveDocKey = %q, want %q", nested, want)
	}
}

// TestResolveDocKeyFollowsUpAndAcrossDirectories covers link direction: up
// from a subdirectory to a sibling of root, and back down into another
// subdirectory -- both must land on the same root-relative key a sibling file
// would resolve to from its own directory, and neither is an escape: "../"
// syntax in the destination doesn't mean the resolved path leaves root, only
// that it climbs above the referencing file's own directory.
func TestResolveDocKeyFollowsUpAndAcrossDirectories(t *testing.T) {
	root := t.TempDir()
	fromTeam := &storageRenderer{baseDir: filepath.Join(root, "team"), root: &project.Root{Dir: root}}

	if got, escapes := fromTeam.resolveDocKey("../index.md"); got != "index.md" || escapes {
		t.Errorf("resolveDocKey(../index.md) = %q, escapes %v, want %q, false", got, escapes, "index.md")
	}
	if got, escapes := fromTeam.resolveDocKey("../ops/runbook.md"); got != "ops/runbook.md" || escapes {
		t.Errorf("resolveDocKey(../ops/runbook.md) = %q, escapes %v, want %q, false", got, escapes, "ops/runbook.md")
	}
}
