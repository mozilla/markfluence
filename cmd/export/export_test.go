package export

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func TestMissingReferences(t *testing.T) {
	referenced := map[string]bool{"here.png": true, "gone.png": true, "also-gone.png": true}
	atts := []client.Attachment{{Title: "here.png"}}

	got := missingReferences(referenced, atts)
	if len(got) != 2 {
		t.Fatalf("got %d warnings, want 2: %v", len(got), got)
	}
	// Sorted, because map iteration order would otherwise vary run to run.
	if !strings.HasPrefix(got[0], "also-gone.png") || !strings.HasPrefix(got[1], "gone.png") {
		t.Errorf("warnings not sorted: %v", got)
	}
}

func TestMissingReferencesNoneWhenAllPresent(t *testing.T) {
	referenced := map[string]bool{"here.png": true}
	atts := []client.Attachment{{Title: "here.png"}, {Title: "extra.png"}}
	if got := missingReferences(referenced, atts); len(got) != 0 {
		t.Errorf("got %v, want no warnings", got)
	}
}

func TestPageFilename(t *testing.T) {
	cases := []struct {
		name, title, id, override, want string
	}{
		{"plain title", "markfluence test page", "123", "", "markfluence-test-page.md"},
		{"punctuation dropped", "Q3 Planning: 2026!", "123", "", "q3-planning-2026.md"},
		{"collapses whitespace", "a   b\tc", "123", "", "a-b-c.md"},
		{"strips path separators", "docs/notes", "123", "", "docsnotes.md"},
		{"unicode letters kept", "Café Plans", "123", "", "café-plans.md"},
		{"id fallback when slug empty", "………", "2848423944", "", "2848423944.md"},
		{"id fallback when title empty", "", "2848423944", "", "2848423944.md"},
		{"override wins", "markfluence test page", "123", "custom.md", "custom.md"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			page := &client.Page{ID: c.id, Title: c.title}
			if got := pageFilename(page, c.override); got != c.want {
				t.Errorf("pageFilename(%q) = %q, want %q", c.title, got, c.want)
			}
		})
	}
}

// TestSlugifyCaps keeps a very long title from producing a filename the
// filesystem rejects, and must not leave a trailing hyphen when the cut lands
// on a word boundary.
func TestSlugifyCaps(t *testing.T) {
	got := slugify(strings.Repeat("long title ", 40))
	if len([]rune(got)) > slugMax {
		t.Errorf("slug is %d runes, want <= %d", len([]rune(got)), slugMax)
	}
	if strings.HasSuffix(got, "-") {
		t.Errorf("slug %q ends in a hyphen", got)
	}
}

// TestSlugifyNeverProducesAPath is the safety property: --file is the only way
// to write outside the destination directory's top level.
func TestSlugifyNeverProducesAPath(t *testing.T) {
	for _, title := range []string{"../escape", "/etc/passwd", "a/b/c", `a\b`} {
		got := slugify(title)
		if strings.ContainsAny(got, `/\`) {
			t.Errorf("slugify(%q) = %q, which contains a path separator", title, got)
		}
	}
}

func TestWritePageSkipsExistingUnlessForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "page.md")
	if err := os.WriteFile(path, []byte("ORIGINAL"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { force, dryRun = false, false })

	status, err := writePage(path, "NEW")
	if err != nil || status != "skipped" {
		t.Fatalf("status = %q, %v; want skipped", status, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "ORIGINAL" {
		t.Errorf("contents = %q; a skip must not write", b)
	}

	force = true
	if status, err := writePage(path, "NEW"); err != nil || status != statusWrote {
		t.Fatalf("status = %q, %v; want wrote", status, err)
	}
	if b, _ := os.ReadFile(path); string(b) != "NEW" {
		t.Errorf("contents = %q, want NEW", b)
	}
}

func TestWritePageDryRunCreatesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "page.md")
	dryRun = true
	t.Cleanup(func() { dryRun = false })

	status, err := writePage(path, "BODY")
	if err != nil || status != statusWrote {
		t.Fatalf("status = %q, %v; want wrote", status, err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Error("dry run created a directory")
	}
}
