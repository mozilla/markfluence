package attachref

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// rootWith makes a documentation root holding d/x.png and opens it.
func rootWith(t *testing.T) (dir string, root *os.Root) {
	t.Helper()
	dir = t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "d", "x.png"), []byte("inside"), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.Close() })
	return dir, root
}

func read(t *testing.T, a LocalAttachment) string {
	t.Helper()
	f, err := a.Open()
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestOpenThroughTheRoot(t *testing.T) {
	dir, root := rootWith(t)
	a := LocalAttachment{Path: filepath.Join(dir, "d", "x.png"), Source: "d/x.png", Root: root}
	if got := read(t, a); got != "inside" {
		t.Errorf("read %q, want inside", got)
	}
}

// TestOpenRefusesAnEscapeMadeAfterTheCheck is #186: the directory on the path
// becomes a symbolic link out of the root after the root was opened (and the
// converter checked the file). Opening by Path would follow it; opening
// through the root refuses.
func TestOpenRefusesAnEscapeMadeAfterTheCheck(t *testing.T) {
	dir, root := rootWith(t)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "x.png"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "d")); err != nil {
		t.Fatal(err)
	}

	a := LocalAttachment{Path: filepath.Join(dir, "d", "x.png"), Source: "d/x.png", Root: root}
	f, err := a.Open()
	if err == nil {
		b, _ := io.ReadAll(f)
		_ = f.Close()
		t.Fatalf("Open read %q from outside the root, want an error", b)
	}
	if want := a.Path + " is outside the documentation root"; err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

// TestOpenRefusesWhatIsNotAFile: checked on the handle, and a FIFO must be
// refused rather than block the open until something writes to it.
func TestOpenRefusesWhatIsNotAFile(t *testing.T) {
	dir, root := rootWith(t)
	if err := syscall.Mkfifo(filepath.Join(dir, "d", "pipe.png"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{"d/pipe.png", "d"} {
		a := LocalAttachment{Path: filepath.Join(dir, source), Source: source, Root: root}
		if f, err := a.Open(); err == nil {
			_ = f.Close()
			t.Errorf("%s: Open succeeded, want it refused", source)
		} else if !strings.Contains(err.Error(), "not a regular file") {
			t.Errorf("%s: err = %v", source, err)
		}
		a.Root = nil
		if f, err := a.Open(); err == nil {
			_ = f.Close()
			t.Errorf("%s without a root: Open succeeded, want it refused", source)
		}
	}
}

// TestOpenWithoutARootUsesPath: attachment-upload's files, which no root
// bounds.
func TestOpenWithoutARootUsesPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "anywhere.png")
	if err := os.WriteFile(path, []byte("given"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := read(t, LocalAttachment{Path: path, Source: "renamed.png"}); got != "given" {
		t.Errorf("read %q, want given", got)
	}
}

// TestOpenRefusesASymlinkInsideTheRoot: os.Root would follow a link that stays
// inside the root, and markfluence follows none, so a link swapped in at the
// file's own name cannot publish another file in the project under its name.
func TestOpenRefusesASymlinkInsideTheRoot(t *testing.T) {
	dir, root := rootWith(t)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("CONFLUENCE_TOKEN=x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "d", "x.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../.env", filepath.Join(dir, "d", "x.png")); err != nil {
		t.Fatal(err)
	}
	a := LocalAttachment{Path: filepath.Join(dir, "d", "x.png"), Source: "d/x.png", Root: root}
	f, err := a.Open()
	if err == nil {
		b, _ := io.ReadAll(f)
		_ = f.Close()
		t.Fatalf("Open read %q through an in-root symlink, want an error", b)
	}
	if !strings.Contains(err.Error(), "symbolic link") {
		t.Errorf("err = %v", err)
	}
}
