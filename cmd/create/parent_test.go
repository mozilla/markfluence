package create

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
)

func rootFor(t *testing.T, dir string) *project.Root {
	t.Helper()
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

func TestResolveParentNoneGiven(t *testing.T) {
	root := rootFor(t, t.TempDir())
	p, err := resolveParent(filepath.Join(root.Dir, "a.md"), map[string]string{}, nil, nil, "", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != "top" {
		t.Errorf("kind = %q, want top", p.kind)
	}
}

func TestResolveParentMdFileNotFound(t *testing.T) {
	root := rootFor(t, t.TempDir())
	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "missing.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "parent file not found") {
		t.Errorf("err = %v, want a not-found error", err)
	}
}

func TestResolveParentMdFileNoPageID(t *testing.T) {
	root := rootFor(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root.Dir, "parent.md"),
		[]byte("---\ntitle: P\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "parent.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "not yet published") {
		t.Errorf("err = %v, want a not-yet-published error", err)
	}
}

func TestResolveParentMdFileInSet(t *testing.T) {
	root := rootFor(t, t.TempDir())
	parentPath := filepath.Join(root.Dir, "parent.md")
	if err := os.WriteFile(parentPath, []byte("body\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	inSetAbs := map[string]bool{parentPath: true}
	p, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "parent.md"}, inSetAbs, nil, "", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != "inset" || p.abs != parentPath {
		t.Errorf("p = %+v, want kind=inset abs=%q", p, parentPath)
	}
}

// TestResolveParentMdFilePublished is the ordinary case: an already-published
// parent, reached through root.FS rather than the bare filesystem now, but
// with the same outcome as before.
func TestResolveParentMdFilePublished(t *testing.T) {
	root := rootFor(t, t.TempDir())
	if err := os.WriteFile(filepath.Join(root.Dir, "parent.md"),
		[]byte("---\npage_id: 100\ntitle: Parent\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := parentServer(t, map[string]string{"100": `{"id":"100","spaceId":"space1"}`}, nil)

	p, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "parent.md"}, nil, c, "space1", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != "published" || p.id != "100" {
		t.Errorf("p = %+v, want kind=published id=100", p)
	}
}

// TestResolveParentEscapingRootIsHardError is S2 for parent: -- a parent
// outside the documentation root is refused outright, not left unresolved and
// reported the way an escaping link is: a parent is load-bearing, and
// publishing under the wrong one (or silently under none) is worse than not
// publishing at all.
func TestResolveParentEscapingRootIsHardError(t *testing.T) {
	base := t.TempDir()
	docs := filepath.Join(base, "docs")
	if err := os.Mkdir(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	root := rootFor(t, docs)
	// The parent target sits outside root, in base itself.
	if err := os.WriteFile(filepath.Join(base, "outside.md"),
		[]byte("---\npage_id: 1\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "../outside.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "outside the documentation root") {
		t.Errorf("err = %v, want a hard error naming the escape", err)
	}
}

// TestResolveParentRefusesSymlinkedTarget mirrors the image leaf's refusal:
// even a symlink resolving inside root is not a regular file.
func TestResolveParentRefusesSymlinkedTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := rootFor(t, t.TempDir())
	outside := t.TempDir()
	real := filepath.Join(outside, "real.md")
	if err := os.WriteFile(real, []byte("---\npage_id: 1\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root.Dir, "parent.md")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "parent.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("err = %v, want a symlink refusal", err)
	}
}
