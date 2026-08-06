// Package attachfile places a Confluence attachment on the filesystem: it
// decides where an attachment goes under a destination directory, and writes it
// there.
//
// It exists because more than one command does this -- `attachment-download`
// and `export` -- and they must agree. In particular Resolve carries the
// traversal clamp, which is the one piece of this codebase that must not exist
// in two copies.
package attachfile

import (
	"fmt"
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
	// Root is the destination directory. It must already be absolute; callers
	// resolve it once so every attachment is clamped against the same path.
	Root string
	// Flat writes each attachment under its stored name, ignoring the source
	// path recorded in its comment.
	Flat bool
	// Force overwrites a file that already exists instead of skipping it.
	Force bool
	// DryRun reports what would be written without creating anything.
	DryRun bool
}

// Resolve decides where an attachment is written, and is the only place a
// server-controlled string becomes a filesystem path.
//
// The recorded source path is used, not a decode of the attachment name: there
// is no way to tell a hand-uploaded "a%2Fb.png" from one markfluence published,
// so decoding by default would scatter a literally-named file into a/b.png. An
// attachment with no recorded source keeps its stored name.
//
// The result must stay inside root. A source path may legitimately contain ".."
// -- an image in a directory above its page is a supported layout -- so ".."
// cannot simply be refused; the resolved path is compared against root instead.
// Escaping is an error rather than a silent clip, because the path comes from an
// attachment comment, which anyone who can edit the page controls.
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
func Write(c *client.ConfluenceClient, a client.Attachment, opts Options) Outcome {
	res := Outcome{Name: a.Title}

	path, err := Resolve(opts.Root, a, opts.Flat)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeValidation
		return res
	}
	res.DestPath = path

	if _, err := os.Stat(path); err == nil && !opts.Force {
		res.Status = StatusSkipped
		return res
	}
	if opts.DryRun {
		res.Status = StatusDownloaded
		return res
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeIO
		return res
	}
	f, err := os.Create(path)
	if err != nil {
		res.Status, res.Err, res.Code = StatusFailed, err, jsonout.CodeIO
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
