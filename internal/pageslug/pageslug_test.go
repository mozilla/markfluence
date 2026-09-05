package pageslug

import (
	"strings"
	"testing"
)

// TestSlug pins the mapping, including the distinction that decides which
// titles collide: punctuation is dropped rather than separated, so equivalence
// runs through whitespace alone.
func TestSlug(t *testing.T) {
	for _, c := range []struct{ title, want string }{
		{"Title 1", "title-1"},
		{"Title: 1", "title-1"},
		{"Title:1", "title1"},
		{"Title-1", "title-1"},
		{"Deploy: Prod", "deploy-prod"},
		{"Deploy Prod", "deploy-prod"},
		{"  spaced  out  ", "spaced-out"},
		{"Über Café", "über-café"},
		{"Q3 (2026)", "q3-2026"},
		{"?!", ""},
	} {
		if got := Slug(c.title); got != c.want {
			t.Errorf("Slug(%q) = %q, want %q", c.title, got, c.want)
		}
	}
}

// TestForFallsBackToTheID covers a title with nothing sluggable in it: an id is
// always usable, and is what keeps such a page exportable at all.
func TestForFallsBackToTheID(t *testing.T) {
	if got := For("………", "2848423944"); got != "2848423944" {
		t.Errorf("For = %q, want the id", got)
	}
	if got := Filename("", "123"); got != "123.md" {
		t.Errorf("Filename = %q, want 123.md", got)
	}
}

// TestSlugCaps keeps a very long title from producing a filename the
// filesystem rejects, and must not leave a trailing hyphen when the cut lands
// on a word boundary.
func TestSlugCaps(t *testing.T) {
	got := Slug(strings.Repeat("long title ", 40))
	if len([]rune(got)) > Max {
		t.Errorf("slug is %d runes, want <= %d", len([]rune(got)), Max)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug %q ends in a hyphen", got)
	}
}

// TestSlugNeverProducesAPath is the safety property: --file is the only way
// to write outside the destination directory's top level.
func TestSlugNeverProducesAPath(t *testing.T) {
	for _, title := range []string{"../escape", "/etc/passwd", "a/b/c", `a\b`} {
		got := Slug(title)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("Slug(%q) = %q, which contains a path separator", title, got)
		}
	}
}
