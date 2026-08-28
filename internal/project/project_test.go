package project

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeProjectFile(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverFindsProjectFile(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root)
	sub := filepath.Join(root, "team", "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(sub)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.FS.Close() }()

	if got.Dir != root {
		t.Errorf("Dir = %q, want %q", got.Dir, root)
	}
	if want := filepath.Join(root, Filename); got.File != want {
		t.Errorf("File = %q, want %q", got.File, want)
	}
}

func TestDiscoverNearestAncestorWins(t *testing.T) {
	outer := t.TempDir()
	writeProjectFile(t, outer)
	inner := filepath.Join(outer, "team")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	writeProjectFile(t, inner)
	sub := filepath.Join(inner, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(sub)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.FS.Close() }()

	if got.Dir != inner {
		t.Errorf("Dir = %q, want the nearer project file's directory %q", got.Dir, inner)
	}
}

func TestDiscoverFallsBackToStartDir(t *testing.T) {
	// No markfluence.yaml anywhere above a fresh temp directory. This walks
	// the real filesystem up to "/", which is fine -- it's a handful of Stat
	// calls -- and relies on the test host not having a markfluence.yaml
	// somewhere above the OS temp directory, same assumption every upward
	// project-file scanner (go.mod, .git, .editorconfig) makes.
	start := t.TempDir()

	got, err := Discover(start)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.FS.Close() }()

	if got.Dir != start {
		t.Errorf("Dir = %q, want the fallback %q", got.Dir, start)
	}
	if got.File != "" {
		t.Errorf("File = %q, want empty (no project file found)", got.File)
	}
}

func TestDiscoverIgnoresADirectoryNamedLikeTheMarker(t *testing.T) {
	root := t.TempDir()
	// A directory happens to be named markfluence.yaml -- not a hit.
	if err := os.Mkdir(filepath.Join(root, Filename), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := Discover(sub)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.FS.Close() }()

	if got.File != "" {
		t.Errorf("File = %q, want empty -- a directory is not a hit", got.File)
	}
}

func TestDiscoverEACCESKeepsWalking(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits don't apply")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}

	base := t.TempDir()
	writeProjectFile(t, base) // the project file this test expects to still find
	blocked := filepath.Join(base, "blocked")
	deep := filepath.Join(blocked, "c", "d")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	// Deny search permission on "blocked" itself: resolving any path through
	// it -- including deep, several levels below -- now fails with EACCES,
	// not just a stat of "blocked" directly.
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(blocked, 0o755) }() // let TempDir cleanup remove it

	got, err := Discover(deep)
	if err != nil {
		t.Fatalf("Discover returned an error instead of walking past the blocked ancestor: %v", err)
	}
	defer func() { _ = got.FS.Close() }()

	if got.Dir != base {
		t.Errorf("Dir = %q, want %q (the project file above the blocked ancestor)", got.Dir, base)
	}
}

func TestDiscoverRootFSIsScopedToDir(t *testing.T) {
	root := t.TempDir()
	writeProjectFile(t, root)

	got, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = got.FS.Close() }()

	f, err := got.FS.Open(Filename)
	if err != nil {
		t.Fatalf("opening %s through FS: %v", Filename, err)
	}
	_ = f.Close()
}
