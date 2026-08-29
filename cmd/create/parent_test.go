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
	if p.kind != parentTop {
		t.Errorf("kind = %q, want top", p.kind)
	}
}

// TestResolveParentIgnoresDirectoryNesting is guarantee L8 (no-layout-inference,
// docs/guarantees.md): hierarchy is never inferred from disk layout. The
// directory shape here is deliberately the one most tempting to "helpfully"
// infer from -- a file named after its parent directory, one level down from a
// same-named sibling file -- and it must still resolve to no parent at all
// when nothing said so.
func TestResolveParentIgnoresDirectoryNesting(t *testing.T) {
	root := rootFor(t, t.TempDir())
	if err := os.MkdirAll(filepath.Join(root.Dir, "section"), 0o755); err != nil {
		t.Fatal(err)
	}
	// "section.md" looks exactly like an index/parent for the "section/" the
	// nested file lives under; neither has a parent: field.
	if err := os.WriteFile(filepath.Join(root.Dir, "section.md"),
		[]byte("---\npage_id: 100\ntitle: Section\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(root.Dir, "section", "page.md")

	p, err := resolveParent(nested, map[string]string{}, nil, nil, "", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != parentTop {
		t.Errorf("kind = %q, want top: nesting under a directory matching a sibling file's "+
			"name must not imply a parent", p.kind)
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
	if p.kind != parentInSet || p.abs != parentPath {
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
	if p.kind != parentPublished || p.id != "100" {
		t.Errorf("p = %+v, want kind=published id=100", p)
	}
}

// TestResolveParentExternalID is the plain "--parent 500" case: a bare id,
// never a .md path, resolved straight through checkParentInSpace.
func TestResolveParentExternalID(t *testing.T) {
	root := rootFor(t, t.TempDir())
	c := parentServer(t, map[string]string{"500": `{"id":"500","spaceId":"space1"}`}, nil)

	parentOpt = "500"
	t.Cleanup(func() { parentOpt = "" })

	p, err := resolveParent(filepath.Join(root.Dir, "a.md"), map[string]string{}, nil, c, "space1", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != parentExternal || p.id != "500" || p.parentType != "page" {
		t.Errorf("p = %+v, want kind=external id=500 parentType=page", p)
	}
}

// TestResolveParentExternalFolderID: --parent works identically for a folder,
// which every v2 page route would 404 on -- the whole point of #68.
func TestResolveParentExternalFolderID(t *testing.T) {
	root := rootFor(t, t.TempDir())
	c := parentServer(t, nil, map[string]string{"600": `{"id":"600","type":"folder","spaceId":"space1"}`})

	parentOpt = "600"
	t.Cleanup(func() { parentOpt = "" })

	p, err := resolveParent(filepath.Join(root.Dir, "a.md"), map[string]string{}, nil, c, "space1", root)
	if err != nil {
		t.Fatal(err)
	}
	if p.kind != parentExternal || p.id != "600" || p.parentType != "folder" {
		t.Errorf("p = %+v, want kind=external id=600 parentType=folder", p)
	}
}

// TestResolveParentBothSetIsAnError: --parent and a frontmatter parent can't
// both name the parent, since one would silently win over the other.
func TestResolveParentBothSetIsAnError(t *testing.T) {
	root := rootFor(t, t.TempDir())
	parentOpt = "500"
	t.Cleanup(func() { parentOpt = "" })

	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "other.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "both --parent and a frontmatter 'parent' are set") {
		t.Errorf("err = %v, want the both-set conflict error", err)
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

// TestResolveParentEscapeThroughSymlinkedDirectoryNamesTheEscape mirrors
// internal/convert's TestRenderImageRefusesEscapeThroughSymlinkedDirectory: a
// lexical containment check sees the parent as inside root, and only os.Root
// -- which resolves the symlink -- refuses it. The message must name the
// escape, not read as a plain "not found" the way any other Lstat error does;
// an author chasing that message would go looking for a typo instead of
// realizing the parent escapes the documentation root.
func TestResolveParentEscapeThroughSymlinkedDirectoryNamesTheEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevated privileges on Windows")
	}
	root := rootFor(t, t.TempDir())
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "parent.md"),
		[]byte("---\npage_id: 1\n---\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// "assets" looks like an ordinary subdirectory of root; it actually leads
	// outside it.
	if err := os.Symlink(outside, filepath.Join(root.Dir, "assets")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, err := resolveParent(filepath.Join(root.Dir, "a.md"),
		map[string]string{"parent": "assets/parent.md"}, nil, nil, "", root)
	if err == nil || !strings.Contains(err.Error(), "outside the documentation root") {
		t.Errorf("err = %v, want a hard error naming the escape, not \"not found\"", err)
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
