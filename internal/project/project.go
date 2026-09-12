// Package project discovers the root of a markfluence project: the directory
// holding markfluence.yaml, found by walking up from a starting directory.
//
// The marker file declares project-wide settings (Config, #100), and a file
// that cannot be understood is not a valid marker: discovery fails rather than
// walking on to an ancestor or falling back to the starting directory.
//
// What the file still never does is authorize anything. It is read, never
// executed, and discovery decides only where the root is -- it does not grant
// a project the ability to redirect credentials, which is why Config holds no
// url or token and why a setting there answers "what is this content" rather
// than "who are you". See _plans/026's security review for why that separation
// is deliberate, and _plans/038 for the sharper form of it: basic auth goes to
// whatever host the resolved URL names, so a committed, walked-up file naming
// one would decide where the token is sent.
//
// Discover is called from two different starting points for two different
// reasons, which is why this package returns a Root rather than a bare string:
// once per invocation from the working directory, to locate .env before any
// file is touched, and once per markdown file, from that file's own
// directory, to bound its reads and name its attachments. The two coincide
// whenever the file sits under the project the working directory is in, and
// diverge only when there is no markfluence.yaml at all, or a file belongs to
// a different project than the working directory does -- both legitimate,
// neither an error.
package project

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Filename is the marker file Discover looks for.
const Filename = "markfluence.yaml"

// Root is a discovered (or defaulted) markfluence project root.
type Root struct {
	// Dir is the root directory, absolute.
	Dir string
	// File is the absolute path to markfluence.yaml, or "" when none was
	// found and Dir fell back to the starting directory.
	File string
	// Config is what that file declares, zero-valued when there is no file
	// (or when the file declares nothing, which is the shape that ships).
	Config Config
	// FS scopes every read to Dir: a path cannot escape it, even via a
	// symlink partway down its traversal, which a lexical containment check
	// cannot see but os.Root refuses outright. Callers close it when done.
	FS *os.Root
}

// Discover walks up from startDir looking for markfluence.yaml, stopping at
// the first ancestor that has one or at the filesystem root. Reaching the
// filesystem root with no hit is not an error: Dir falls back to startDir
// itself, and File stays "".
//
// It stats a filename at each level rather than listing the directory, which
// needs only execute (search) permission on each ancestor -- guaranteed, or
// startDir itself would be unreachable. An ancestor that can't be stat'd for
// permissions is treated as "not here" rather than fatal: the walk keeps
// going rather than failing a command over a directory it was never going to
// read anything else from.
//
// Discover does not follow symlinks in the walk itself -- it climbs lexically
// via filepath.Dir on an absolute path, so a symlinked ancestor is never
// substituted in. That closes the class of failure 025 names for the
// pre-model code: comparing paths after resolving one side and not the other.
func Discover(startDir string) (*Root, error) {
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return nil, err
	}

	dir := abs
	for {
		hit, candidate, err := probeMarker(dir)
		if err != nil {
			return nil, err
		}
		if hit {
			return open(dir, candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached the filesystem root with no hit.
			return open(abs, "")
		}
		dir = parent
	}
}

// probeMarker stats markfluence.yaml directly inside dir, reporting whether
// it's a hit and, if so, its path. Factored out of Discover so Cache's own
// walk (cache.go) can consult its cache at every level using the identical
// per-directory check, rather than re-deriving it.
func probeMarker(dir string) (hit bool, file string, err error) {
	candidate := filepath.Join(dir, Filename)
	info, statErr := os.Stat(candidate)
	switch {
	case statErr == nil && !info.IsDir():
		return true, candidate, nil
	case statErr == nil:
		// A directory happens to be named markfluence.yaml -- not a hit; keep
		// walking as if nothing were there.
		return false, "", nil
	case errors.Is(statErr, fs.ErrNotExist), errors.Is(statErr, fs.ErrPermission):
		// Not here, or we can't tell -- keep walking.
		return false, "", nil
	default:
		return false, "", statErr
	}
}

// open builds a Root for dir, opening an os.Root scoped to it and loading the
// project file's settings when there is one.
//
// The load happens here because this is the single place a Root is built from a
// marker: Discover, Cache.walkAndCache and FromPath all reach it, so the rule
// that a project file which cannot be understood is not a valid marker exists
// in one copy and the three cannot come to disagree about it. Nothing keeps
// walking upward for a better marker and nothing falls back to the starting
// directory: the root decides every attachment name, bounds every read, and
// anchors the link index, so a file that cannot be understood means the
// project's boundary is unknown, and guessing is worse than stopping (#100).
//
// Loading precedes OpenRoot so a refusal leaks no handle.
func open(dir, file string) (*Root, error) {
	cfg := Config{}
	if file != "" {
		var err error
		if cfg, err = loadConfig(file); err != nil {
			return nil, err
		}
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Root{Dir: dir, File: file, Config: cfg, FS: root}, nil
}

// FromPath builds a Root directly from an explicit directory, bypassing
// discovery entirely. It exists for --root, which overrides discovery for the
// whole invocation with one value applied uniformly to every file. dir must
// exist and be a directory; a markfluence.yaml inside it is still noted in
// File, for accurate reporting, even though --root's meaning doesn't depend
// on one being there.
func FromPath(dir string) (*Root, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("--root %s is not a directory", dir)
	}

	file := ""
	candidate := filepath.Join(abs, Filename)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		file = candidate
	}
	return open(abs, file)
}

// Resolve is the entry point a command should use once it has both an
// optional --root override and a starting directory: override wins,
// bypassing discovery via FromPath; an empty override discovers normally via
// Discover.
func Resolve(override, startDir string) (*Root, error) {
	if override != "" {
		return FromPath(override)
	}
	return Discover(startDir)
}
