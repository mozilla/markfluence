// Package actionlog is the append-only record of what markfluence published,
// kept per project root, so a later run can tell "I changed this" from "they
// changed it" (#149).
//
// The problem it exists for: `update` cannot see whether the page it is about
// to overwrite has moved on since the local copy was made. No page-side state
// can answer that, because the page cannot know what a given local copy was
// derived from -- it is a per-copy fact, a merge base, and the copy is the only
// place it can live. So each publish records the page version it produced and a
// sha of what it sent, and the last such line for a file is that copy's base.
//
// # Not committed
//
// A shared repository is itself a declaration that the repository is the source
// of truth, and in that arrangement `--force` is the answer and no base is
// consulted. So the log serves exactly one arrangement -- a local copy, source
// of truth in Confluence -- where per-checkout state is the right shape and
// another person's sync point is irrelevant to mine. Committing it would buy
// nothing and would conflict on every concurrent publish, tail-appended.
//
// # Advisory, never load-bearing
//
// Nothing here may fail a command. A missing, unreadable, corrupt or
// half-written log degrades the check that reads it and never the run: Base
// answers "no base" and the caller publishes, which is what markfluence did
// before any of this existed. That is why Read skips a line it cannot parse
// instead of returning an error, and why a failed Append is a warning at the
// call site rather than a failed result -- the page is already published by the
// time the line is written, so failing would report that it was not.
package actionlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/project"
)

// Dirname is the per-root directory holding markfluence's local state, and
// Filename is the log inside it.
//
// A directory rather than a bare file at the root, for three reasons: one
// ignore entry covers every future piece of local state, compaction can rename
// within it, and the git-specific knowledge stays confined to a file whose
// absence breaks nothing.
const (
	Dirname  = ".markfluence"
	Filename = "log.jsonl"
)

// gitignore is planted beside the log the first time one is written: a
// directory that ignores itself, so nothing in it is ever committed and the
// entry does not have to be added to a .gitignore markfluence does not own.
const gitignore = "# markfluence's local state. Not committed: see internal/actionlog.\n*\n"

// Statuses a line may carry. Only StatusOK establishes a base; a failure is
// recorded so the history is complete for debugging and is skipped by Base.
const (
	StatusOK     = "ok"
	StatusFailed = "failed"
)

// Actions a line may name. Only these three establish a base, because only
// these three pair a local file with a page at a known version.
const (
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionExport = "export"
)

// Entry is one line of the log.
//
// Two fields carry the base and the rest is provenance. PageVersion answers
// "did the page move past my copy" and PublishSHA256 answers "would publishing
// change anything"; they are read independently, so a line carrying one and not
// the other still answers half the question (an export line written during the
// walk, before the pass that computes its sha, is exactly that shape).
//
// Nothing ever compares Time. It is here because "which build published this,
// and when" is the first question about a surprising page, and because a log is
// unreadable without one -- not because any decision consults a clock. Deciding
// by clock is the defect #149 exists to remove.
type Entry struct {
	Time   string `json:"time"`
	Action string `json:"action"`
	Status string `json:"status"`
	// File is the root-relative lexical key pages: is keyed by, from
	// pagemeta.KeyFor. Lexical and root-relative because L2 forbids a key
	// whose meaning depends on how a checkout is laid out.
	File   string `json:"file"`
	PageID string `json:"page_id"`
	// PageVersion is the version the page was left at, 0 when unknown.
	// Confluence numbers versions from 1, so 0 cannot collide with a real one.
	PageVersion int `json:"page_version"`
	// PublishSHA256 is Sum over what the body PUT sent, "" when unknown.
	PublishSHA256 string `json:"publish_sha256"`
	// Markfluence is the version that wrote the line.
	Markfluence string `json:"markfluence"`
}

// Sum is the publish sha: a hash of exactly what the body PUT would send, which
// is the resolved title and the rendered storage body.
//
// The scope is the point. The sha gates that one request, so it must cover
// everything the request carries and nothing it does not -- page width, labels
// and attachments each have their own pass that runs whether or not the body is
// republished, and folding any of them in would bump the page version for a
// change that never touched the body. The title is in because
// convert.ConfluencePage does not carry one: update passes it to UpdatePage
// separately, so hashing the body alone would skip the publish of a file whose
// only change was its title.
//
// Each part is length-prefixed rather than joined by a separator, so no title
// can be confused with the start of a body. That framing is part of the
// persisted format: changing it invalidates every recorded base, which costs
// one republish per page and is self-healing, but is a format change and
// belongs in a commit message rather than in a refactor.
func Sum(title, body string) string {
	h := sha256.New()
	writePart(h, "title", title)
	writePart(h, "body", body)
	return hex.EncodeToString(h.Sum(nil))
}

// writePart writes one length-prefixed component of the preimage. hash.Hash
// documents that Write never returns an error, which is why this one is
// dropped rather than plumbed.
func writePart(h hash.Hash, name, value string) {
	_, _ = fmt.Fprintf(h, "%s:%d:%s", name, len(value), value)
}

// Log is one root's action log. A nil *Log is usable and does nothing, which is
// what a root with no project file resolves to.
type Log struct {
	// root is held for its FS, an os.Root scoped to the project root: every
	// read and write below goes through it rather than through bare os calls.
	// That is what keeps S1 (no-write-outside-root) true of this package --
	// see ensureDir.
	root *project.Root
	// bases is the last ok line per key, read once on first use. A batch
	// publishing a hundred files under one root reads the log once.
	bases map[string]Entry
	// readErr is why bases is empty, reported once by the caller rather than
	// per file: an unreadable log is one fact about the project.
	readErr error
	loaded  bool
	// Malformed counts lines skipped as unparseable, for --debug.
	Malformed int
}

// For returns the log for root, or nil when root declares no project file.
//
// Nil is the whole of the "no root, no log" rule: project.Root.File is "" when
// Discover found no markfluence.yaml walking up from the file's own directory,
// and Root.Dir is then a fallback -- the file's own directory, not a root
// anybody declared. Planting state there would scatter a .markfluence per
// directory across a tree, each log keyed by a bare filename. #139 already
// settled the general form: a root with no project file refuses rather than
// creating one, since whether markfluence may create one is #5's question.
func For(root *project.Root) *Log {
	if root == nil || root.File == "" {
		return nil
	}
	return &Log{root: root}
}

// relPath is the log's path relative to the project root, which is the form
// every os.Root call takes.
func relPath() string { return filepath.Join(Dirname, Filename) }

// Path is the log file's absolute path, for a message naming it. Reads and
// writes never use it -- they go through root.FS. Valid on a nil *Log,
// returning "".
func (l *Log) Path() string {
	if l == nil {
		return ""
	}
	return filepath.Join(l.root.Dir, relPath())
}

// Append records one line, creating the directory, its .gitignore and the log
// itself as needed. A nil *Log records nothing and reports no error.
//
// Time and Markfluence are filled in here rather than by the caller, so every
// line agrees about its clock and its build.
//
// One O_APPEND write per line and no lock, deliberately. The same optimistic
// posture as project.SetPageEntry and client.SetContentProperty: a lock file
// brings stale-lock handling with it, which is more machinery than a verb a
// person invokes by hand warrants. A concurrent append can in principle tear a
// line, and Read is built to skip one rather than to prevent it -- the right
// trade for a file that is advisory by construction.
func (l *Log) Append(e Entry) error {
	if l == nil {
		return nil
	}
	if e.Time == "" {
		e.Time = time.Now().UTC().Format(time.RFC3339)
	}
	if e.Markfluence == "" {
		e.Markfluence = buildinfo.Version
	}
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := l.ensureDir(); err != nil {
		return err
	}
	f, err := l.root.FS.OpenFile(relPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	// Keep the in-memory view consistent with the file, so a second command
	// in one process -- and every test -- sees what was just written without
	// re-reading. Only a success establishes a base, matching Read.
	if l.loaded && e.Status == StatusOK && e.File != "" {
		l.bases[e.File] = e
	}
	return nil
}

// ensureDir creates .markfluence and plants its .gitignore.
//
// Through root.FS rather than os.MkdirAll and os.WriteFile, and that is the
// security-relevant part of this package rather than a style choice. A bare
// os.OpenFile follows a symlink, so a `.markfluence/log.jsonl` symlinked
// anywhere -- planted by whoever can write the project directory -- would have
// markfluence append JSON to a file outside the root. Measured, not assumed:
// the bare call wrote straight through such a link. S1
// (no-write-outside-root) is stated as Holds, and this is the one write in the
// tree that did not go through the os.Root every other path uses. An os.Root
// refuses an escape even through a symlinked intermediate directory, which a
// lexical check cannot see.
//
// The ignore file is written only when it is absent, never overwritten: a
// project that has edited it has said something, and this is the one file here
// somebody might reasonably have opinions about.
func (l *Log) ensureDir() error {
	if err := l.root.FS.Mkdir(Dirname, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	path := filepath.Join(Dirname, ".gitignore")
	if _, err := l.root.FS.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return l.root.FS.WriteFile(path, []byte(gitignore), 0o644)
}

// Base returns the merge base for a file: the most recent successful line
// naming it. The second result is false when there is none, which every caller
// must read as "unknown" and never as "unchanged" -- unknown means publish.
//
// An entry naming a different page than the file resolves to now is the
// caller's to discard, not this function's: it has no way to know what the file
// resolves to. The rule is in the plan and belongs at the comparison.
func (l *Log) Base(key string) (Entry, bool) {
	if l == nil || key == "" {
		return Entry{}, false
	}
	l.load()
	e, ok := l.bases[key]
	return e, ok
}

// ReadError reports why the log could not be read, or nil. It is a fact about
// the project rather than about any one file, so a caller reports it once per
// run instead of per file.
func (l *Log) ReadError() error {
	if l == nil {
		return nil
	}
	l.load()
	return l.readErr
}

// load reads the log once, keeping the last ok line per key.
func (l *Log) load() {
	if l.loaded {
		return
	}
	l.loaded = true
	l.bases = map[string]Entry{}

	f, err := l.root.FS.Open(relPath())
	if err != nil {
		// An absent log is the normal state of a project that has not
		// published yet, and is not an error to report. Anything else is.
		if !errors.Is(err, fs.ErrNotExist) {
			l.readErr = err
		}
		return
	}
	defer func() { _ = f.Close() }()
	l.bases, l.Malformed, l.readErr = read(f)
}

// read parses a log, keeping the last ok line per key and counting the lines it
// could not use.
//
// It never fails on content. A truncated final line from an interrupted append,
// a line from a future markfluence carrying a field this build does not know, a
// blank line -- each is skipped or absorbed, because a corrupt advisory file
// must not be load-bearing. Only the read itself can fail, and even then the
// lines already parsed are kept: a log that is readable up to a bad sector
// still answers for every file before it.
func read(r io.Reader) (map[string]Entry, int, error) {
	bases := map[string]Entry{}
	malformed := 0
	sc := bufio.NewScanner(r)
	// A line is small, but an attachment-heavy future field or a pathological
	// file should not make this the thing that fails.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Entry
		// Unknown fields are accepted on purpose: a line written by a newer
		// markfluence must not make this one refuse the whole log, which is
		// what lets a field be added later with no format version.
		if err := json.Unmarshal(line, &e); err != nil {
			malformed++
			continue
		}
		if e.Status != StatusOK || e.File == "" {
			continue
		}
		bases[e.File] = e
	}
	if err := sc.Err(); err != nil {
		return bases, malformed, err
	}
	return bases, malformed, nil
}
