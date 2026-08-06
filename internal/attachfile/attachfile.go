// Package attachfile places a Confluence attachment on the filesystem: it
// decides where an attachment goes under a destination directory, and writes it
// there.
//
// It exists because more than one command does this -- `attachment-download`
// and `export` -- and they must agree. Containment is the reason it must not
// exist in two copies: a destination path is derived from an attachment comment,
// which anyone able to edit the page controls.
//
// Containment is enforced in two layers. Resolve rejects absolute paths and
// anything that lexically leaves the destination, which catches the obvious
// cases early and with a message naming the attachment. Write then performs
// every filesystem operation through an os.Root scoped to the destination, which
// is the guarantee that actually holds: os.Root refuses symlinks leading out of
// the root, and a lexical check cannot see those at all.
package attachfile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
)

// Outcomes of writing one attachment.
const (
	StatusDownloaded = "downloaded"
	StatusSkipped    = "skipped"
	StatusFailed     = "failed"
)

// Outcome is what happened to one attachment.
type Outcome struct {
	Name     string // the stored attachment name
	DestPath string // the local path written, or that would be
	Status   string
	Err      error
	Code     jsonout.Code
}

// Options controls how attachments are written.
type Options struct {
	// Root is the destination directory, and the boundary every write is
	// confined to. It should be absolute -- callers resolve it once so every
	// attachment is measured against the same path -- and is cleaned here, so a
	// trailing separator or an interior "." is not a caller's problem.
	Root string
	// Flat writes each attachment under its stored name, ignoring the source
	// path recorded in its comment.
	Flat bool
	// Force overwrites a file that already exists instead of skipping it.
	Force bool
	// DryRun reports what would be written without creating anything.
	DryRun bool
}

// Resolve decides where an attachment is written, and is the first of two
// guards on a server-controlled string becoming a filesystem path.
//
// The recorded source path is used, not a decode of the attachment name: there
// is no way to tell a hand-uploaded "a%2Fb.png" from one markfluence published,
// so decoding by default would scatter a literally-named file into a/b.png. An
// attachment with no recorded source keeps its stored name.
//
// This check is *lexical*. A source path may legitimately contain ".." -- an
// image in a directory above its page is a supported layout -- so ".." cannot
// simply be refused; the cleaned path is compared against root instead.
// Escaping is an error rather than a silent clip, because the path comes from an
// attachment comment, which anyone who can edit the page controls.
//
// Being lexical, it cannot see symlinks: if a directory inside root is a link
// pointing out of it, a path that looks contained resolves elsewhere on disk.
// Write is what actually enforces containment, via os.Root. Resolve exists to
// reject the obvious cases early and with a message naming the attachment, and
// to compute the path for reporting and dry runs -- do not treat a successful
// Resolve as proof a write is safe.
//
// Two properties of filepath.Join are load-bearing here. It expands "." and ".."
// textually, so a path is compared in cleaned form; but a ".." following a
// symlinked component cleans to a different directory than the OS would resolve,
// which is the same reason the lexical check alone is not enough. And it does
// not expand "~" or "$HOME" -- Go has no notion of either -- so a source path of
// "~/.ssh/authorized_keys" yields a literal "~" directory inside root rather
// than touching the home directory.
func Resolve(root string, a client.Attachment, flat bool) (string, error) {
	rel := a.Title
	if !flat {
		if src := a.Meta().Source; src != "" {
			rel = src
		}
	}
	// A stored name is a single path element by construction; guard anyway, since
	// it is server data.
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("attachment %q resolves to the absolute path %q", a.Title, rel)
	}

	// Clean root rather than trusting the caller to. Join cleans its result, so
	// comparing against an uncleaned root mixes the two forms: a trailing
	// separator ("out/", the natural way to type it) or an interior "." would
	// make the prefix test fail and reject every attachment. That is fail-closed
	// rather than a hole, but the symptom -- everything refused, for no visible
	// reason -- is not one worth leaving available.
	root = filepath.Clean(root)
	path := filepath.Join(root, filepath.FromSlash(rel))
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("attachment %q resolves to %q, outside the destination directory",
			a.Title, path)
	}
	if path == root {
		return "", fmt.Errorf("attachment %q has no filename", a.Title)
	}
	return path, nil
}

// Write resolves an attachment's destination and downloads it there, reporting
// what happened. It never returns an error: a per-attachment failure is carried
// in the Outcome so one bad attachment doesn't abort a batch.
//
// Every filesystem operation goes through an os.Root scoped to opts.Root, which
// is what actually confines the write. os.Root follows symlinks but refuses ones
// leading outside the root, closing the hole Resolve's lexical check cannot see:
// a destination containing a symlinked directory (docs/assets -> ../shared, say)
// would otherwise pass the string comparison and land bytes elsewhere on disk.
func Write(c *client.ConfluenceClient, a client.Attachment, opts Options) Outcome {
	res := Outcome{Name: a.Title}

	// Normalize once so the resolved path, the root opened below, and the
	// relative path between them are all in the same form.
	root := filepath.Clean(opts.Root)

	path, err := Resolve(root, a, opts.Flat)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeValidation
		return res
	}
	res.DestPath = path

	// Relative to the root, which is how every os.Root method addresses a file.
	rel, err := filepath.Rel(root, path)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeValidation
		return res
	}

	if _, err := os.Stat(path); err == nil && !opts.Force {
		res.Status = StatusSkipped
		return res
	}
	if opts.DryRun {
		res.Status = StatusDownloaded
		return res
	}

	// The root must exist before it can be opened; this is the one path
	// operation that necessarily happens outside the confinement, and it only
	// ever creates the directory the user named.
	if err := os.MkdirAll(root, 0o755); err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeIO
		return res
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeIO
		return res
	}
	defer func() { _ = rootFS.Close() }()

	if dir := filepath.Dir(rel); dir != "." {
		if err := rootFS.MkdirAll(dir, 0o755); err != nil {
			res.Status, res.Err, res.Code = StatusFailed, writeErr(a, path, err), jsonout.CodeIO
			return res
		}
	}
	f, err := rootFS.Create(rel)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, writeErr(a, path, err), jsonout.CodeIO
		return res
	}
	defer func() { _ = f.Close() }()
	if err := c.DownloadAttachment(a, f); err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeFor(err)
		return res
	}
	res.Status = StatusDownloaded
	return res
}

// writeErr annotates a filesystem failure with the attachment and destination,
// which os.Root's bare messages ("openat x: path escapes from parent") omit.
//
// A refusal to leave the root is called out specifically, because it is a
// blocked escape rather than an ordinary I/O error and the cause -- a symlink in
// the destination -- is not otherwise visible. os.Root reports a blocked
// directory component as "file exists", indistinguishable from a plain file in
// the way, so that case is described in terms covering both.
func writeErr(a client.Attachment, path string, err error) error {
	switch {
	case strings.Contains(err.Error(), "escapes from parent"):
		return fmt.Errorf("attachment %q would be written outside the destination directory "+
			"(a symlink in the destination points out of it); refusing: %w", a.Title, err)
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("writing attachment %q to %s: a file or symlink is already "+
			"in the way: %w", a.Title, path, err)
	default:
		return fmt.Errorf("writing attachment %q to %s: %w", a.Title, path, err)
	}
}
