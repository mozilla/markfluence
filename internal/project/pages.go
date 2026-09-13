package project

// The pages: key -- per-file page metadata living in the project file instead
// of in the markdown, so a .md can be published while staying pristine (#139).

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
)

// Entry is one file's page metadata, shaped exactly like a parsed frontmatter
// block: the same two maps frontmatter.MarkdownFile carries, scalars by key and
// sequences by key.
//
// That shape is the whole design and not a convenience. It makes #139's "an
// entry is a whole frontmatter block that lives elsewhere" literal -- the same
// keys, the same value domains, the same validation -- and it is the only shape
// that avoids a cycle: internal/pagewidth and internal/labels both reach
// internal/client, which holds a *Cache, so this package can never import
// them. With two maps it does not need to. labels.Declared(e.Lists, e.Fields),
// pagewidth.Declared(e.Fields) and pageref.IsDigits(e.Fields["page_id"]) all
// work on an entry unchanged, in the commands that already call them.
//
// Which is also why nothing here validates a *value*. See entryFields.
type Entry struct {
	Fields map[string]string
	Lists  map[string][]string
}

// entryFields is the manifest's schema: the field names an entry may hold and
// whether each is a scalar or a list. The only place it is written down.
//
// It mirrors frontmatter's own fields deliberately -- adding one there without
// adding it here would make a field expressible in a file and not in an entry,
// which is the "same keys" half of the equivalence above.
var entryFields = map[string]kind{
	"title":      kindScalar,
	"space":      kindScalar,
	"parent":     kindScalar,
	"page_id":    kindScalar,
	"page_width": kindScalar,
	"labels":     kindList,
}

// readPages reads the pages: mapping into entries keyed by normalized path.
//
// What is checked here is *structure*: the value is a mapping of mappings, each
// key is a legal path, each field name is known, and each field's value has the
// right shape. What is not checked is any field's *value* -- a non-numeric
// page_id, an invalid page_width, an invalid label -- because #139 requires a
// semantically bad entry to be reported only when that entry's file is one of
// the arguments. One bad entry must not block every invocation in the repo,
// and this function has no idea which files the command was given.
//
// An unknown field *name* is checked here rather than per-file, and the line is
// worth stating: it is the same typo class as an unknown top-level setting --
// `titel:` silently ignored is wrong forever, for that page -- and it means the
// manifest was written against a different markfluence, which #100 settles as
// fatal. A bad value is one entry's problem; an unrecognized schema is the
// file's.
func readPages(items []frontmatter.Item) (map[string]Entry, error) {
	pages := map[string]Entry{}
	// Which spelling each normalized key came from, so a collision can name
	// both rather than only the survivor.
	origin := map[string]string{}

	for _, it := range items {
		if it.Map == nil {
			return nil, fmt.Errorf("page %q must be a mapping of fields", it.Key)
		}
		key, err := NormalizePageKey(it.Key)
		if err != nil {
			return nil, err
		}
		if first, dup := origin[key]; dup {
			return nil, fmt.Errorf(
				"pages %q and %q both name %q; give it one spelling", first, it.Key, key)
		}
		entry, err := readEntry(it.Key, it.Map)
		if err != nil {
			return nil, err
		}
		origin[key] = it.Key
		pages[key] = entry
	}
	return pages, nil
}

// readEntry reads one entry's fields. named is the key as written, so a message
// points at the spelling the author would search for.
func readEntry(named string, fields []frontmatter.Item) (Entry, error) {
	e := Entry{Fields: map[string]string{}, Lists: map[string][]string{}}
	for _, f := range fields {
		want, ok := entryFields[f.Key]
		if !ok {
			return Entry{}, fmt.Errorf(
				"page %q has an unknown field %q (known: %s) -- an unrecognized field may "+
					"mean this project needs a newer markfluence", named, f.Key,
				strings.Join(knownEntryFields(), ", "))
		}
		if f.Map != nil {
			return Entry{}, fmt.Errorf("page %q field %q must be a value, not a mapping",
				named, f.Key)
		}
		switch want {
		case kindList:
			if f.List == nil {
				// Refused rather than read as a one-element list, matching
				// internal/labels' reasoning: labels is destructive, and a
				// `labels:` with nothing after it would otherwise mean "strip
				// this page" on an entry where somebody typed a key and stopped.
				return Entry{}, fmt.Errorf("page %q field %q must be a list", named, f.Key)
			}
			e.Lists[f.Key] = f.List
		default:
			if f.List != nil {
				return Entry{}, fmt.Errorf("page %q field %q must be a single value, not a list",
					named, f.Key)
			}
			e.Fields[f.Key] = f.Value
		}
	}
	return e, nil
}

// volumeRelative reports whether p carries a Windows drive letter ("C:/x"),
// which is absolute in meaning while not starting with a separator.
func volumeRelative(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// IsPageField reports whether name is a field markfluence understands on a
// page, in frontmatter or in an entry.
//
// Exported because telling a markfluence key from one it merely preserves is
// not a judgment a caller should make for itself. internal/frontmatter keeps
// keys markfluence knows nothing about on purpose (a test there pins
// `reviewers: [ana, bo]` surviving a write), so a caller that counted any key
// at all would read a file carrying only those as claimed.
func IsPageField(name string) bool { return entryFields[name] != 0 }

// knownEntryFields lists the recognized field names, sorted so a message is
// stable.
func knownEntryFields() []string {
	out := make([]string, 0, len(entryFields))
	for key := range entryFields {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// NormalizePageKey turns a path as written into the form pages: is keyed by and
// arguments are looked up as: lexically cleaned, root-relative, slash form.
//
// Callers use it on both sides -- the manifest's keys and the path of a file
// being published -- because a mismatch now means a silent *skip* rather than
// an error, so the two must agree exactly.
//
// Lexical only, no symlink resolution, matching withinRoot, attachfile.Resolve
// and attachment-download's destPath. A symlinked docs/ is legitimate, and
// resolving it would make a key depend on the checkout's layout, which L2
// (invocation-independent) forbids.
//
// A key escaping the root is an error rather than a miss: the project file
// declares the project's boundary, so a key outside it is the manifest being
// wrong, not a file being absent. So is an absolute path, which names a
// location no root can contain.
//
// No case folding. Known limit, beside pageslug's NFD/NFC note: on a
// case-insensitive filesystem Docs/a.md opens the file but matches no docs/a.md
// key, so it reads as unmanaged and is skipped.
func NormalizePageKey(p string) (string, error) {
	if p == "" {
		return "", fmt.Errorf("a page key cannot be empty")
	}
	// Separators first, then absoluteness: judging the raw string let
	// "\\foo.md", "C:\\foo.md" and "\\\\server\\share\\a.md" through as keys, which
	// normalize to absolute paths KeyFor can never produce -- a silently
	// unreachable entry, which is worse than an error because nothing says so.
	slashed := strings.ReplaceAll(p, "\\", "/")
	if path.IsAbs(slashed) || strings.HasPrefix(slashed, "/") || volumeRelative(slashed) {
		return "", fmt.Errorf("page %q must be relative to the project root, not absolute", p)
	}
	clean := path.Clean(slashed)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("page %q is outside the project root", p)
	}
	if clean == "." {
		return "", fmt.Errorf("page %q does not name a file", p)
	}
	return clean, nil
}

// SetPageEntry records a file's page metadata in the project file's pages:
// block, creating the block and the entry as needed.
//
// key must already be normalized (NormalizePageKey); the caller has it from
// pagemeta.KeyFor, and normalizing again here would hide a caller that skipped
// it. entry's fields are written in frontmatter's canonical order.
//
// The read-modify-write is deliberate, and so is doing it once per page rather
// than once per run: cmd/create writes each file's frontmatter as that page is
// published, so a run that dies partway leaves every already-created page
// recorded. The manifest has to keep that property, and one small file read and
// written per created page is the price (#139 D10).
//
// Two verifications, not one. frontmatter.SetNested re-reads its own output and
// checks each field landed at the path -- the shape a miscomputed indent
// silently breaks. Then this re-runs the *loader* over the result, so a write
// that produced a file markfluence could not read, or could read as something
// else, fails before anything is written to disk. Nothing is more annoying than
// a tool that corrupts the file it was recording success in.
func (r *Root) SetPageEntry(key string, entry Entry) error {
	if r.File == "" {
		return fmt.Errorf("no %s to write an entry to", Filename)
	}
	// Two attempts, because the read-modify-write is not serialized and this
	// file is shared by every page in the project.
	//
	// Before the manifest each page's metadata went into its own file, so two
	// concurrent creates could not collide. They share one file now, which
	// reintroduces the lost update: A reads, B reads, A writes, B writes, and
	// A's entry is gone while A's page exists -- "page created, entry not
	// recorded" again, arriving from concurrency rather than from a refusal.
	//
	// Optimistic rather than locked, and the precedent is client's:
	// SetContentProperty retries once on top of a versioned PUT for the same
	// shape of reason. A lock file would be stronger and would bring
	// stale-lock handling with it, which is more machinery than a verb a person
	// invokes by hand warrants. One retry is enough because the window is a
	// single file rewrite: the loser re-reads the winner's file and merges into
	// it, and two writers colliding twice in that window is not a case worth
	// designing for.
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		cfg, written, err := r.trySetPageEntry(key, entry)
		if err != nil {
			return err
		}
		if written {
			r.Config = cfg
			return nil
		}
		lastErr = &ConfigError{File: r.File, Err: fmt.Errorf(
			"%s changed while the entry for %q was being written", Filename, key)}
	}
	return lastErr
}

// trySetPageEntry attempts one read-modify-write, reporting whether it landed.
// written is false when the file changed underneath, which is the caller's cue
// to try again against the new content.
func (r *Root) trySetPageEntry(key string, entry Entry) (cfg Config, written bool, err error) {
	before, err := os.ReadFile(r.File)
	if err != nil {
		return Config{}, false, &ConfigError{File: r.File, Err: errors.New(readFailure(err))}
	}
	// The BOM comes off before the writer sees it and goes back on after.
	// parseConfig strips one deliberately (#100), so a BOM-prefixed project
	// file is a shape markfluence accepts -- but leaving it in made the first
	// key parse as "\ufeffpages", so the writer created a *second* pages: block
	// beside it and the reload then refused the file. Dropping it instead would
	// silently change a file the author's editor wrote.
	body := string(before)
	bom := ""
	if strings.HasPrefix(body, "\ufeff") {
		bom, body = "\ufeff", strings.TrimPrefix(body, "\ufeff")
	}
	// The existing spelling of the key, when there is one. pages: keys are
	// compared after normalization, so an entry written as "./b.md" *is* the
	// entry for "b.md" -- matching on the raw text appended a second one, and
	// the reload then refused the file for having two keys naming one path.
	path := []string{"pages", existingKeySpelling(body, key)}
	after, err := frontmatter.SetNested(body, path, entryFieldList(entry))
	if err != nil {
		return Config{}, false, &ConfigError{File: r.File, Err: err}
	}
	after = bom + after
	// The loader, not just the parser: an unknown field or a wrong shape must
	// fail here rather than on somebody's next invocation.
	cfg, err = parseConfig(r.File, after)
	if err != nil {
		return Config{}, false, err
	}
	if _, ok := cfg.Pages[key]; !ok {
		return Config{}, false, &ConfigError{File: r.File, Err: fmt.Errorf(
			"the rewritten file has no entry for %q", key)}
	}
	if beforeReplace != nil {
		beforeReplace()
	}
	// Re-read immediately before replacing. Not a guarantee -- another writer
	// can still land between this and the rename -- but it closes the window
	// that matters, which is the seconds a create spends publishing a page
	// between reading the file and writing it back.
	current, err := os.ReadFile(r.File)
	if err != nil {
		return Config{}, false, &ConfigError{File: r.File, Err: errors.New(readFailure(err))}
	}
	if string(current) != string(before) {
		return Config{}, false, nil
	}
	if err := replaceFile(r.File, after); err != nil {
		return Config{}, false, &ConfigError{File: r.File, Err: errors.New(readFailure(err))}
	}
	return cfg, true, nil
}

// beforeReplace runs just before the re-read that detects a concurrent write.
// A test hook, nil in every real run, and the same arrangement SetRetryLogger
// and SetSecurityWarner use: the give-up path is otherwise only reachable by
// racing a real writer, which makes for a slow and flaky test of a branch that
// exists precisely so a collision is reported rather than silently resolved.
var beforeReplace func()

// existingKeySpelling returns the spelling an equivalent key is already written
// under, or key itself when there is none.
//
// Keys are compared after NormalizePageKey, so "./b.md", "docs//a.md" and a
// backslash form all name entries that already exist -- and appending a second,
// normalized spelling made the file hold two keys naming one path, which the
// loader refuses. Updating the entry that is there is both correct and what the
// author would expect.
func existingKeySpelling(body, key string) string {
	items, err := dialect.ReadMapping(body)
	if err != nil {
		return key
	}
	for _, it := range items {
		if it.Key != "pages" || it.Map == nil {
			continue
		}
		for _, entry := range it.Map {
			if normalized, err := NormalizePageKey(entry.Key); err == nil && normalized == key {
				return entry.Key
			}
		}
	}
	return key
}

// replaceFile writes content over path via a temporary file in the same
// directory, then renames.
//
// os.WriteFile truncates first, so a write interrupted by a full disk or a
// signal leaves the project file truncated -- and unlike the frontmatter path,
// which risks one page's metadata, this file holds *every* entry in the
// project. A rename is atomic on the same filesystem, so an interrupted run
// leaves the original untouched.
//
// The mode of an existing file is preserved; a new one gets 0o644, matching
// every other file markfluence writes.
func replaceFile(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	// Same directory, so the rename cannot cross a filesystem boundary.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }() // no-op once the rename succeeds
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// entryFieldList turns an Entry into frontmatter Fields, in canonical order.
//
// A list field is passed as a non-nil List even when empty, since an empty
// labels: is a declaration meaning "remove them all" and a nil one means the
// key is absent -- the distinction internal/labels rests on.
func entryFieldList(e Entry) []frontmatter.Field {
	var fields []frontmatter.Field
	for _, key := range knownEntryFields() {
		if entryFields[key] == kindList {
			if l, ok := e.Lists[key]; ok {
				fields = append(fields, frontmatter.Field{Key: key, List: l})
			}
			continue
		}
		if v, ok := e.Fields[key]; ok {
			fields = append(fields, frontmatter.Field{Key: key, Value: v})
		}
	}
	return fields
}
