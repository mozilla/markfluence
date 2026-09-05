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
