package convert_test

// Symlink refusal at the image leaf, and the os.Root backstop for an escape
// through a symlinked intermediate directory. Built programmatically rather
// than as checked-in golden fixtures -- a checked-in symlink is fragile across
// platforms and git's core.symlinks setting -- mirroring how
// internal/attachfile tests the same class of escape.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

func skipIfNoSymlinks(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
}

func TestRenderImageRefusesSymlinkedLeaf(t *testing.T) {
	skipIfNoSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(outside, "real.png")
	if err := os.WriteFile(target, []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "logo.png")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	md := frontmatter.Parse(filepath.Join(root, "main.md"), "![logo](logo.png)\n")
	r, err := project.FromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.FS.Close() }()

	idx := testIndex(t, r)
	page, err := convert.MdToConfluence(md, r, idx, "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if len(page.Attachments) != 0 {
		t.Errorf("a symlinked leaf must not become an attachment, got %v", page.Attachments)
	}
	if len(page.Broken) != 1 || !strings.Contains(page.Broken[0], "symlink") {
		t.Errorf("broken = %v, want one entry naming a symlink", page.Broken)
	}
	if strings.Contains(page.HTML, "ri:attachment") {
		t.Errorf("published body references an attachment for a refused symlink:\n%s", page.HTML)
	}
}

func TestRenderImageRefusesEscapeThroughSymlinkedDirectory(t *testing.T) {
	skipIfNoSymlinks(t)
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "logo.png"), []byte("PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "assets" looks like an ordinary subdirectory of root; it is actually a
	// link leading outside it. A lexical check (filepath.Rel on root vs.
	// root/assets/logo.png) sees this as contained -- only os.Root, which
	// resolves the symlink and refuses one leading outside its scope, catches
	// it. This is the backstop 025 assigns to os.Root, distinct from the leaf
	// refusal above.
	if err := os.Symlink(outside, filepath.Join(root, "assets")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	md := frontmatter.Parse(filepath.Join(root, "main.md"), "![logo](assets/logo.png)\n")
	r, err := project.FromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.FS.Close() }()

	idx := testIndex(t, r)
	page, err := convert.MdToConfluence(md, r, idx, "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if len(page.Attachments) != 0 {
		t.Errorf("an escape through a symlinked directory must not become an attachment, got %v", page.Attachments)
	}
	if len(page.Broken) != 1 || !strings.Contains(page.Broken[0], "outside the documentation root") {
		t.Errorf("broken = %v, want one entry naming the escape", page.Broken)
	}
}
