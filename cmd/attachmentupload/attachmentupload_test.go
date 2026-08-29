package attachmentupload

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/project"
)

func writeFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalAttachmentsUsesBaseName(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "docs/assets/x.png")

	got, err := localAttachments([]string{path}, "", project.NewCache(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d attachments, want 1", len(got))
	}
	if got[0].Filename != "x.png" {
		t.Errorf("filename = %q, want x.png", got[0].Filename)
	}
	if got[0].Source != "x.png" {
		t.Errorf("source = %q, want x.png", got[0].Source)
	}
	if got[0].Path != path {
		t.Errorf("path = %q, want %q", got[0].Path, path)
	}
}

// TestLocalAttachmentsSourceIsRootRelative is the point of the change: under a
// declared root spanning more than the file's own directory, the recorded
// source keeps the subdirectory structure instead of flattening to a bare
// basename -- matching what publishing a page that references the same file
// would record (internal/convert/images.go).
func TestLocalAttachmentsSourceIsRootRelative(t *testing.T) {
	root := t.TempDir()
	path := writeFile(t, root, "docs/assets/x.png")

	got, err := localAttachments([]string{path}, "", project.NewCache(root))
	if err != nil {
		t.Fatal(err)
	}
	if want := "docs/assets/x.png"; got[0].Source != want {
		t.Errorf("source = %q, want %q", got[0].Source, want)
	}
	if want := "docs%2Fassets%2Fx.png"; got[0].Filename != want {
		t.Errorf("filename = %q, want %q", got[0].Filename, want)
	}
}

// TestLocalAttachmentsNameEncodesPath is the point of --name taking a path: the
// user writes a path and markfluence produces the attachment a publish of
// ![](assets/x.png) would resolve to, without them typing an escape.
func TestLocalAttachmentsNameEncodesPath(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "somewhere/else.png")

	got, err := localAttachments([]string{path}, "assets/x.png", project.NewCache(""))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Filename != "assets%2Fx.png" {
		t.Errorf("filename = %q, want assets%%2Fx.png", got[0].Filename)
	}
	if got[0].Source != "assets/x.png" {
		t.Errorf("source = %q, want assets/x.png", got[0].Source)
	}
}

// TestLocalAttachmentsSourceIsAlwaysTheDecodedName is the lockstep invariant:
// if the recorded path and the stored name could disagree, a later publish
// would upload a second attachment while a restoring download put this one
// where the markdown never references it.
func TestLocalAttachmentsSourceIsAlwaysTheDecodedName(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "f.png")

	for _, name := range []string{"", "assets/x.png", "./a/./b.png", "../shared/logo.png", "plain.png"} {
		got, err := localAttachments([]string{path}, name, project.NewCache(""))
		if err != nil {
			t.Fatalf("--name %q: %v", name, err)
		}
		decoded, ok := decodeName(got[0].Filename)
		if !ok {
			t.Errorf("--name %q: stored name %q does not decode", name, got[0].Filename)
			continue
		}
		if decoded != got[0].Source {
			t.Errorf("--name %q: source %q != decode of %q (%q)",
				name, got[0].Source, got[0].Filename, decoded)
		}
	}
}

// TestLocalAttachmentsRejectsRootEscape covers --root naming a directory that
// isn't an ancestor of the file at all: project.Resolve applies the override
// uniformly with no containment check of its own, so rootRelativeSource must
// refuse the resulting "../"-prefixed rel itself, the same way
// internal/convert/images.go's rootRelative and create's resolveParent do,
// rather than encoding it into the attachment name.
func TestLocalAttachmentsRejectsRootEscape(t *testing.T) {
	unrelatedRoot := t.TempDir()
	fileDir := t.TempDir()
	path := writeFile(t, fileDir, "x.png")

	if _, err := localAttachments([]string{path}, "", project.NewCache(unrelatedRoot)); err == nil {
		t.Error("want an error when --root does not contain the file")
	}
}

func TestLocalAttachmentsRejectsMissingAndDirs(t *testing.T) {
	dir := t.TempDir()
	if _, err := localAttachments([]string{filepath.Join(dir, "nope.png")}, "", project.NewCache("")); err == nil {
		t.Error("want an error for a missing file")
	}
	if _, err := localAttachments([]string{dir}, "", project.NewCache("")); err == nil {
		t.Error("want an error for a directory")
	}
}

// TestForcedRewritesSkips covers the dry-run forecast for --force: what would
// have been skipped is reported as updated, matching what a forced run does.
func TestForcedRewritesSkips(t *testing.T) {
	in := []client.SyncAction{
		{Filename: "a.png", Action: "skipped"},
		{Filename: "b.png", Action: "created"},
		{Filename: "c.png", Action: "updated"},
	}
	got := forced(in)
	want := []string{"updated", "created", "updated"}
	for i := range want {
		if got[i].Action != want[i] {
			t.Errorf("[%d] action = %q, want %q", i, got[i].Action, want[i])
		}
	}
	if in[0].Action != "skipped" {
		t.Error("forced mutated its input")
	}
}
