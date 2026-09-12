package project

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/ui"
)

// write puts a project file in a fresh directory and returns the directory.
func write(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDiscoverReadsSettings(t *testing.T) {
	dir := write(t, "space: ENG\npage_width: wide\n")
	root, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	defer root.FS.Close()
	if root.Config.Space != "ENG" {
		t.Errorf("Space = %q, want ENG", root.Config.Space)
	}
	if root.Config.PageWidth != "wide" {
		t.Errorf("PageWidth = %q, want wide", root.Config.PageWidth)
	}
}

// The marker that ships today is a comment and nothing else, and export plants
// exactly that. Both it and an empty file must stay valid markers declaring no
// settings -- a project file gaining keys must not invalidate every existing one.
func TestDiscoverAcceptsAMarkerWithNoSettings(t *testing.T) {
	for name, body := range map[string]string{
		"empty": "",
		"comment only": "# Marks the root of a markfluence project. Image and link paths are recorded\n" +
			"# relative to this directory. https://github.com/mozilla/markfluence\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := write(t, body)
			root, err := Discover(dir)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			defer root.FS.Close()
			if root.Config != (Config{}) {
				t.Errorf("Config = %#v, want zero", root.Config)
			}
			if root.Dir != dir {
				t.Errorf("Dir = %q, want %q", root.Dir, dir)
			}
		})
	}
}

func TestDiscoverRefusesAnUnknownSetting(t *testing.T) {
	dir := write(t, "space: ENG\nspce: OPS\n")
	_, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover succeeded on an unknown setting, want an error")
	}
	msg := err.Error()
	for _, want := range []string{
		filepath.Join(dir, Filename) + ":2",
		`unknown setting "spce"`,
		"known: page_width, space",
		"newer markfluence",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error = %q, want it to contain %q", msg, want)
		}
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("error spans lines:\n%s", msg)
	}
}

func TestDiscoverRefusesAMalformedFile(t *testing.T) {
	tests := map[string]struct{ body, want string }{
		// A goccy parse error carries its own [line:col] and no noun of ours;
		// ConfigError is what names the file.
		"parse error":       {"space: [ENG\n", "sequence end token"},
		"top-level list":    {"- ENG\n", "must be a flat mapping of setting: value pairs"},
		"list where scalar": {"space: [ENG, OPS]\n", `setting "space" must be a single value, not a list`},
		"nested mapping":    {"space:\n  key: ENG\n", `setting "space" must be a single scalar value`},
		"duplicate key":     {"space: ENG\nspace: OPS\n", "already defined"},
		"second document":   {"space: ENG\n...\nspace: OPS\n", "must be a single document"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := write(t, tc.body)
			_, err := Discover(dir)
			if err == nil {
				t.Fatalf("Discover succeeded on %q, want an error", tc.body)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err, tc.want)
			}
			var cfgErr *ConfigError
			if !errors.As(err, &cfgErr) {
				t.Errorf("error is %T, want it to unwrap to *ConfigError", err)
			}
		})
	}
}

// The abort has to be immediate. Walking on to an ancestor that happens to
// have a good project file would publish under a root the author did not
// declare, which is the guess #100 exists to prevent.
func TestDiscoverDoesNotWalkPastAMalformedFile(t *testing.T) {
	outer := write(t, "space: ENG\n")
	inner := filepath.Join(outer, "docs")
	if err := os.Mkdir(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, Filename), []byte("spce: OPS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Discover(inner); err == nil {
		t.Fatal("Discover fell back to the outer project; a malformed file must stop the walk")
	}
}

// A malformed file is not a marker, so it must not be read as one and then
// have the *starting directory* used as the root either.
func TestDiscoverDoesNotFallBackToStartDirOnAMalformedFile(t *testing.T) {
	dir := write(t, "spce: OPS\n")
	root, err := Discover(dir)
	if err == nil {
		defer root.FS.Close()
		t.Fatalf("Discover returned root %q, want an error", root.Dir)
	}
}

func TestCacheRefusesAMalformedFile(t *testing.T) {
	dir := write(t, "spce: OPS\n")
	c := NewCache("")
	defer c.Close()
	if _, err := c.Resolve(dir); err == nil {
		t.Fatal("Cache.Resolve succeeded, want an error")
	}
}

// The cache exists so a batch pays for discovery once. A second file under the
// same root must not re-read the project file.
func TestCacheReadsTheProjectFileOnce(t *testing.T) {
	dir := write(t, "space: ENG\n")
	sub := filepath.Join(dir, "docs")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewCache("")
	defer c.Close()

	first, err := c.Resolve(sub)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Make the file unreadable as YAML. A second Resolve that re-read it would
	// now fail; one served from the cache cannot notice.
	if err := os.WriteFile(filepath.Join(dir, Filename), []byte("- broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := c.Resolve(filepath.Join(dir, "other"))
	if err != nil {
		t.Fatalf("second Resolve re-read the project file: %v", err)
	}
	if second != first {
		t.Error("second Resolve built a new Root, want the cached one")
	}
	if second.Config.Space != "ENG" {
		t.Errorf("Space = %q, want ENG", second.Config.Space)
	}
}

// --root overrides discovery, not the file's contents: it would be strange for
// the flag that declares the root to discard the root's own settings.
func TestFromPathReadsSettings(t *testing.T) {
	dir := write(t, "space: ENG\n")
	root, err := FromPath(dir)
	if err != nil {
		t.Fatalf("FromPath: %v", err)
	}
	defer root.FS.Close()
	if root.Config.Space != "ENG" {
		t.Errorf("Space = %q, want ENG", root.Config.Space)
	}
}

func TestFromPathRefusesAMalformedFile(t *testing.T) {
	dir := write(t, "spce: OPS\n")
	if _, err := FromPath(dir); err == nil {
		t.Fatal("FromPath succeeded, want an error")
	}
}

// --root at a directory with no project file has no settings and is not an
// error: that is the whole point of the flag for a tree that will never have one.
func TestFromPathWithNoProjectFile(t *testing.T) {
	dir := t.TempDir()
	root, err := FromPath(dir)
	if err != nil {
		t.Fatalf("FromPath: %v", err)
	}
	defer root.FS.Close()
	if root.File != "" || root.Config != (Config{}) {
		t.Errorf("File = %q, Config = %#v, want empty", root.File, root.Config)
	}
}

// An ancestor that cannot be stat'd is "not here" and the walk continues
// (probeMarker). A project file that was found and cannot be read is different:
// it is a boundary that exists and cannot be established.
func TestDiscoverRefusesAnUnreadableProjectFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read a 0000 file")
	}
	dir := write(t, "space: ENG\n")
	path := filepath.Join(dir, Filename)
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o644) })

	_, err := Discover(dir)
	if err == nil {
		t.Fatal("Discover succeeded on an unreadable project file, want an error")
	}
	if strings.Contains(err.Error(), path+": "+path) {
		t.Errorf("error names the path twice: %s", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error = %q, want it to name %q", err, path)
	}
}

// A declared-but-empty setting says no more than an absent key, and must not
// be an error -- someone typing a key and stopping is a normal half-edit.
func TestLoadConfigTreatsAnEmptySettingAsUnset(t *testing.T) {
	dir := write(t, "space:\npage_width: ~\n")
	root, err := Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	defer root.FS.Close()
	if root.Config != (Config{}) {
		t.Errorf("Config = %#v, want zero", root.Config)
	}
}

// Settings are per-root, so one invocation spanning two projects gets each
// project's own defaults with no special case.
func TestSettingsArePerRoot(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "one")
	two := filepath.Join(base, "two")
	for dir, body := range map[string]string{one: "space: ENG\n", two: "space: OPS\n"} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, Filename), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	c := NewCache("")
	defer c.Close()

	got := map[string]string{}
	for _, dir := range []string{one, two} {
		root, err := c.Resolve(dir)
		if err != nil {
			t.Fatalf("Resolve(%s): %v", dir, err)
		}
		got[filepath.Base(dir)] = root.Config.Space
	}
	if got["one"] != "ENG" || got["two"] != "OPS" {
		t.Errorf("spaces = %#v, want one=ENG two=OPS", got)
	}
}

// A project-wide default takes effect for a file that says nothing about it,
// so "why did this publish to ENG?" has no answer in the file the reader is
// looking at. --debug is where that answer goes.
func TestLoadConfigReportsDeclaredSettingsUnderDebug(t *testing.T) {
	ui.SetDebug(true)
	t.Cleanup(func() { ui.SetDebug(false) })

	dir := write(t, "space: ENG\npage_width: wide\n")
	out := captureStderr(t, func() {
		root, err := Discover(dir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		root.FS.Close()
	})
	for _, want := range []string{filepath.Join(dir, Filename), "space=ENG", "page_width=wide"} {
		if !strings.Contains(out, want) {
			t.Errorf("debug output = %q, want it to contain %q", out, want)
		}
	}
}

// The marker that ships declares nothing, and a line for every root in a batch
// would be noise. The root itself is already reported unconditionally.
func TestLoadConfigSaysNothingForAMarkerWithNoSettings(t *testing.T) {
	ui.SetDebug(true)
	t.Cleanup(func() { ui.SetDebug(false) })

	dir := write(t, "# Marks the root of a markfluence project.\n")
	out := captureStderr(t, func() {
		root, err := Discover(dir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		root.FS.Close()
	})
	if strings.Contains(out, "project file") {
		t.Errorf("debug output = %q, want nothing about the project file", out)
	}
}

// captureStderr runs fn with os.Stderr redirected, returning what it printed.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		_, _ = io.Copy(&b, r)
		done <- b.String()
	}()
	fn()
	os.Stderr = saved
	_ = w.Close()
	out := <-done
	_ = r.Close()
	return out
}
