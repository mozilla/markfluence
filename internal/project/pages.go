package project

// The pages: key -- per-file page metadata living in the project file instead
// of in the markdown, so a .md can be published while staying pristine (#139).

import (
	"fmt"
	"path"
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

// IsPageField reports whether name is a field markfluence understands on a
// page, in frontmatter or in an entry.
//
// Exported because telling a markfluence key from a foreign one is not a
// judgment a caller should make for itself: a docs tree carrying Jekyll's
// layout:/date: frontmatter has said nothing about Confluence, and a caller
// that counted any key at all would read every such file as claimed.
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
	if path.IsAbs(p) || strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("page %q must be relative to the project root, not absolute", p)
	}
	clean := path.Clean(strings.ReplaceAll(p, "\\", "/"))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("page %q is outside the project root", p)
	}
	if clean == "." {
		return "", fmt.Errorf("page %q does not name a file", p)
	}
	return clean, nil
}
