package attachmentupload

import (
	"os"
	"path"
	"path/filepath"
	"strings"
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
	file := writeFile(t, dir, "docs/assets/x.png")

	got, err := localAttachments([]string{file}, "", project.NewCache(""))
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
	if got[0].Path != file {
		t.Errorf("path = %q, want %q", got[0].Path, file)
	}
}

// TestLocalAttachmentsSourceIsRootRelative is the point of the change: under a
// declared root spanning more than the file's own directory, the recorded
// source keeps the subdirectory structure instead of flattening to a bare
// basename -- matching what publishing a page that references the same file
// would record (internal/convert/images.go).
func TestLocalAttachmentsSourceIsRootRelative(t *testing.T) {
	root := t.TempDir()
	file := writeFile(t, root, "docs/assets/x.png")

	got, err := localAttachments([]string{file}, "", project.NewCache(root))
	if err != nil {
		t.Fatal(err)
	}
	if want := "docs/assets/x.png"; got[0].Source != want {
		t.Errorf("source = %q, want %q", got[0].Source, want)
	}
	if want := "x.png"; got[0].Filename != want {
		t.Errorf("filename = %q, want %q", got[0].Filename, want)
	}
}

// TestLocalAttachmentsNameTakesAPath is the point of --name taking a path: the
// user writes the path the markdown uses and markfluence produces the
// attachment a publish of ![](assets/x.png) would resolve to -- the base name,
// with the path itself kept as the recorded source.
func TestLocalAttachmentsNameTakesAPath(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "somewhere/else.png")

	got, err := localAttachments([]string{file}, "assets/x.png", project.NewCache(""))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Filename != "x.png" {
		t.Errorf("filename = %q, want x.png", got[0].Filename)
	}
	if got[0].Source != "assets/x.png" {
		t.Errorf("source = %q, want assets/x.png", got[0].Source)
	}
}

// TestLocalAttachmentsNameIsTheSourcesBaseName is the lockstep invariant, in
// the form the basename scheme gives it: the stored name is exactly the base
// name of the recorded source. If the two could disagree, a later publish would
// upload a second attachment while a restoring download put this one where the
// markdown never references it.
//
// It replaces an invariant stated the other way round -- that the source is
// always a decode of the name -- which held only while the name carried the
// whole path. Decoding a base name back into a source would discard the path,
// and the comment is now the only place it is written down.
func TestLocalAttachmentsNameIsTheSourcesBaseName(t *testing.T) {
	dir := t.TempDir()
	file := writeFile(t, dir, "f.png")

	for _, name := range []string{"", "assets/x.png", "./a/./b.png", "../shared/logo.png", "plain.png"} {
		got, err := localAttachments([]string{file}, name, project.NewCache(""))
		if err != nil {
			t.Fatalf("--name %q: %v", name, err)
		}
		if want := path.Base(got[0].Source); got[0].Filename != want {
			t.Errorf("--name %q: stored name %q is not the base name of source %q (%q)",
				name, got[0].Filename, got[0].Source, want)
		}
	}
}

// TestLocalAttachmentsRefusesABatchCollision is the batch counterpart of the
// converter's refusal. Nothing downstream would catch it: planAttachments reads
// what is on the page once, before the loop, so two files claiming one name both
// plan "created" and the second upload lands on top of the first with both
// reported as successful.
func TestLocalAttachmentsRefusesABatchCollision(t *testing.T) {
	root := t.TempDir()
	a := writeFile(t, root, "arch/diagram.png")
	b := writeFile(t, root, "deploy/diagram.png")
	writeFile(t, root, "markfluence.yaml")

	_, err := localAttachments([]string{a, b}, "", project.NewCache(root))
	if err == nil {
		t.Fatal("want a refusal when two files want one attachment name")
	}
	for _, want := range []string{"arch/diagram.png", "deploy/diagram.png", "diagram.png"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestLocalAttachmentsAllowsTheSameFileTwice keeps the refusal to genuine
// collisions: naming one file twice is one attachment, not two claims on a name.
func TestLocalAttachmentsAllowsTheSameFileTwice(t *testing.T) {
	root := t.TempDir()
	f := writeFile(t, root, "assets/x.png")

	got, err := localAttachments([]string{f, f}, "", project.NewCache(root))
	if err != nil {
		t.Fatalf("localAttachments: %v", err)
	}
	if len(got) != 2 || got[0].Filename != got[1].Filename {
		t.Errorf("got %v, want the same name twice rather than a refusal", got)
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
	file := writeFile(t, fileDir, "x.png")

	if _, err := localAttachments([]string{file}, "", project.NewCache(unrelatedRoot)); err == nil {
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
