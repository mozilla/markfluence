package actionlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
)

// testRoot builds a project root with a markfluence.yaml, which is what makes
// For return a Log at all.
func testRoot(t *testing.T) *project.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte("# marker\n"), 0o644); err != nil {
		t.Fatalf("planting marker: %v", err)
	}
	root, err := project.FromPath(dir)
	if err != nil {
		t.Fatalf("building root: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

// rootWithoutMarker is the fallback shape: Discover found nothing, so Dir is
// the starting directory and File is empty.
func rootWithoutMarker(t *testing.T) *project.Root {
	t.Helper()
	root, err := project.Discover(t.TempDir())
	if err != nil {
		t.Fatalf("discovering: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	if root.File != "" {
		t.Fatalf("expected no project file, got %q", root.File)
	}
	return root
}

func TestAppendCreatesTheDirectoryAndItsIgnoreFile(t *testing.T) {
	root := testRoot(t)
	l := For(root)
	e := Entry{Action: ActionUpdate, Status: StatusOK, File: "a.md", PageID: "1", PageVersion: 4}
	if err := l.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}

	ignore := filepath.Join(root.Dir, Dirname, ".gitignore")
	data, err := os.ReadFile(ignore)
	if err != nil {
		t.Fatalf("reading .gitignore: %v", err)
	}
	// The self-ignoring directory is the whole mechanism: without the "*" the
	// log would be committed, and a committed log conflicts on every
	// concurrent publish and serves an arrangement that does not need it.
	if !strings.Contains(string(data), "*") {
		t.Errorf(".gitignore does not ignore the directory's contents: %q", data)
	}
	if _, err := os.Stat(l.Path()); err != nil {
		t.Errorf("log not written: %v", err)
	}
}

// An edited .gitignore is somebody's, and this is the one file here anyone
// might reasonably have opinions about.
func TestAppendDoesNotOverwriteAnEditedIgnoreFile(t *testing.T) {
	root := testRoot(t)
	l := For(root)
	if err := os.MkdirAll(filepath.Join(root.Dir, Dirname), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	ignore := filepath.Join(root.Dir, Dirname, ".gitignore")
	if err := os.WriteFile(ignore, []byte("mine\n"), 0o644); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := l.Append(Entry{Action: ActionUpdate, Status: StatusOK, File: "a.md"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	data, _ := os.ReadFile(ignore)
	if string(data) != "mine\n" {
		t.Errorf("overwrote an existing .gitignore: %q", data)
	}
}

func TestAppendStampsTimeAndVersion(t *testing.T) {
	l := For(testRoot(t))
	if err := l.Append(Entry{Action: ActionUpdate, Status: StatusOK, File: "a.md"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, ok := l.Base("a.md")
	if !ok {
		t.Fatal("no base recorded")
	}
	if got.Time == "" {
		t.Error("Time not stamped")
	}
	if got.Markfluence == "" {
		t.Error("Markfluence version not stamped")
	}
}

func TestBaseIsTheLastSuccessfulLine(t *testing.T) {
	l := For(testRoot(t))
	for _, e := range []Entry{
		{Action: ActionExport, Status: StatusOK, File: "a.md", PageID: "1", PageVersion: 4},
		{Action: ActionUpdate, Status: StatusOK, File: "a.md", PageID: "1", PageVersion: 5, PublishSHA256: "aa"},
		{Action: ActionUpdate, Status: StatusOK, File: "b.md", PageID: "2", PageVersion: 9},
	} {
		if err := l.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// Re-read from disk rather than trusting Append's in-memory update.
	fresh := For(testRoot2(t, l))
	got, ok := fresh.Base("a.md")
	if !ok {
		t.Fatal("no base for a.md")
	}
	if got.PageVersion != 5 || got.PublishSHA256 != "aa" {
		t.Errorf("got v%d sha %q, want the last line (v5, aa)", got.PageVersion, got.PublishSHA256)
	}
	if _, ok := fresh.Base("nobody.md"); ok {
		t.Error("a file with no line reported a base")
	}
}

// testRoot2 reopens the same directory as another Log, so a test can read back
// what Append wrote rather than what it cached.
func testRoot2(t *testing.T, l *Log) *project.Root {
	t.Helper()
	root, err := project.FromPath(filepath.Dir(l.dir))
	if err != nil {
		t.Fatalf("reopening root: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

// A failed publish must be visible in the history and must never become a
// base: a run that only ever failed for a file leaves history, not a claim
// about what that copy was derived from.
func TestAFailedLineIsHistoryAndNotABase(t *testing.T) {
	l := For(testRoot(t))
	if err := l.Append(Entry{Action: ActionUpdate, Status: StatusOK, File: "a.md", PageVersion: 4}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := l.Append(Entry{Action: ActionUpdate, Status: StatusFailed, File: "a.md", PageVersion: 9}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	fresh := For(testRoot2(t, l))
	got, ok := fresh.Base("a.md")
	if !ok {
		t.Fatal("no base for a.md")
	}
	if got.PageVersion != 4 {
		t.Errorf("got v%d, want the last *successful* line (v4)", got.PageVersion)
	}

	data, err := os.ReadFile(l.Path())
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	if n := strings.Count(strings.TrimSpace(string(data)), "\n") + 1; n != 2 {
		t.Errorf("log has %d lines, want both the success and the failure", n)
	}
}

// The log is advisory, so nothing in its content may fail a command. A
// truncated final line is what an interrupted append leaves behind.
func TestReadSkipsWhatItCannotUseAndKeepsGoing(t *testing.T) {
	const log = `{"status":"ok","file":"a.md","page_version":1}
{"status":"ok","file":"b.md","page_ver
not json at all
{"status":"ok","file":"c.md","page_version":3,"a_field_from_the_future":true}

{"status":"ok","page_version":4}
`
	bases, malformed, err := read(strings.NewReader(log))
	if err != nil {
		t.Fatalf("read returned an error for bad content: %v", err)
	}
	if malformed != 2 {
		t.Errorf("counted %d malformed lines, want 2", malformed)
	}
	if _, ok := bases["a.md"]; !ok {
		t.Error("dropped a good line before the bad one")
	}
	// A line from a newer markfluence carrying an unknown field must still be
	// usable, or adding a field later would need a format version.
	if e, ok := bases["c.md"]; !ok || e.PageVersion != 3 {
		t.Error("refused a line carrying an unknown field")
	}
	if len(bases) != 2 {
		t.Errorf("kept %d bases, want a.md and c.md only", len(bases))
	}
}

// The two comparisons are independent, so a line carrying one field still
// answers half the question -- which is the shape an export line has between
// the walk and the pass that computes its sha.
func TestALineWithNoShaStillCarriesItsVersion(t *testing.T) {
	l := For(testRoot(t))
	e := Entry{Action: ActionExport, Status: StatusOK, File: "a.md", PageID: "1", PageVersion: 7}
	if err := l.Append(e); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, ok := For(testRoot2(t, l)).Base("a.md")
	if !ok {
		t.Fatal("no base")
	}
	if got.PageVersion != 7 {
		t.Errorf("PageVersion = %d, want 7", got.PageVersion)
	}
	if got.PublishSHA256 != "" {
		t.Errorf("PublishSHA256 = %q, want empty", got.PublishSHA256)
	}
}

// A root with no markfluence.yaml gets no log, and every method stays usable.
func TestNoProjectFileMeansNoLog(t *testing.T) {
	if l := For(rootWithoutMarker(t)); l != nil {
		t.Fatalf("got a log for a root with no project file: %q", l.dir)
	}
	var l *Log
	if err := l.Append(Entry{File: "a.md"}); err != nil {
		t.Errorf("Append on a nil Log: %v", err)
	}
	if _, ok := l.Base("a.md"); ok {
		t.Error("a nil Log reported a base")
	}
	if err := l.ReadError(); err != nil {
		t.Errorf("ReadError on a nil Log: %v", err)
	}
	if p := l.Path(); p != "" {
		t.Errorf("Path on a nil Log = %q", p)
	}
}

func TestAnAbsentLogIsNotAReadError(t *testing.T) {
	l := For(testRoot(t))
	if _, ok := l.Base("a.md"); ok {
		t.Error("reported a base with no log on disk")
	}
	if err := l.ReadError(); err != nil {
		t.Errorf("an absent log reported an error: %v", err)
	}
}

func TestAnUnreadableLogReportsItselfOnce(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads anything")
	}
	root := testRoot(t)
	l := For(root)
	if err := l.Append(Entry{Action: ActionUpdate, Status: StatusOK, File: "a.md"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := os.Chmod(l.Path(), 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(l.Path(), 0o644) })

	fresh := For(testRoot2(t, l))
	if _, ok := fresh.Base("a.md"); ok {
		t.Error("reported a base from a log it could not read")
	}
	if fresh.ReadError() == nil {
		t.Error("an unreadable log reported no error")
	}
}

// The framing is what stops a title from being confused with the start of a
// body: without it, ("ab", "c") and ("a", "bc") hash the same.
func TestSumFramesItsPartsSoTheyCannotRunTogether(t *testing.T) {
	if Sum("ab", "c") == Sum("a", "bc") {
		t.Error("a title and a body run together in the preimage")
	}
	// Determinism matters more here than it looks: the value is persisted and
	// compared against a later run's, so a hash varying between calls would
	// republish every page forever.
	first, second := Sum("t", "b"), Sum("t", "b")
	if first != second {
		t.Error("not deterministic")
	}
	if Sum("t", "b") == Sum("t2", "b") {
		t.Error("the title does not affect the sum")
	}
	if Sum("t", "b") == Sum("t", "b2") {
		t.Error("the body does not affect the sum")
	}
	if len(Sum("", "")) != 64 {
		t.Errorf("not a sha256 hex digest: %q", Sum("", ""))
	}
}

func TestCacheHandsOutOneLogPerRoot(t *testing.T) {
	c := NewCache()
	root := testRoot(t)
	if a, b := c.Get(root), c.Get(root); a != b {
		t.Error("built two logs for one root")
	}
	other := testRoot(t)
	if c.Get(root) == c.Get(other) {
		t.Error("shared one log across two roots")
	}
	if c.Get(rootWithoutMarker(t)) != nil {
		t.Error("built a log for a root with no project file")
	}
	if len(c.Logs()) != 2 {
		t.Errorf("Logs() returned %d, want the two real roots", len(c.Logs()))
	}
}
