// Package pagemeta resolves a markdown file's page metadata from the two places
// it may live: the file's own frontmatter, and a pages: entry in the project's
// markfluence.yaml (#139).
//
// A package rather than a helper because update, create, fix, check *and*
// internal/linkindex all need the identical merge, and a per-command copy is
// exactly how two commands come to publish one file to two different pages.
// It imports internal/frontmatter and internal/project and nothing else, so it
// stays out of every import cycle -- which is also why it validates no value:
// internal/pagewidth and internal/labels are unreachable from here, and the
// commands that need them already call them on the maps this returns.
//
// Both locations are legal and agreement is silent. That is what makes
// migration incremental: copy values into the manifest, verify, delete them
// from the files later, with everything working throughout. There is no project
// "mode" and no all-or-nothing switch.
//
// Frontmatter and an entry are two spellings of *one* level, not two levels of
// a precedence chain. When both speak, the rule is not "the higher wins" but
// the grading in Resolve: a coordinate disagreement can publish over a live
// page, so it fails the file, while a visible and recoverable one warns.
package pagemeta

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

// Source says where a file's metadata came from, and is what --json reports as
// metadata_source. Debugging "why did it publish to *that* page" in CI
// otherwise means reproducing the resolution by hand.
type Source string

const (
	// FromFrontmatter: the file carries markfluence keys.
	FromFrontmatter Source = "frontmatter"
	// FromManifest: a pages: entry supplied them and the file carries none.
	FromManifest Source = "manifest"
	// FromBoth: both locations speak. Reported as "frontmatter" to a caller
	// asking which one won, since frontmatter does for every soft field -- see
	// Resolved.MetadataSource.
	FromBoth Source = "both"
	// Unmanaged: neither location says anything about this file.
	Unmanaged Source = ""
)

// coordinates are the fields whose disagreement is destructive rather than
// merely visible. A page_id copy-pasted from an old file publishes over a live
// page; space and parent decide where a create lands, and #10 wants update to
// enforce and move, so they are coordinates now rather than after a rule
// change.
var coordinates = map[string]bool{"page_id": true, "space": true, "parent": true}

// Resolved is a file's effective metadata.
//
// Fields and Lists are the merged maps, in exactly the shape
// frontmatter.MarkdownFile carries them, so every existing consumer --
// pagewidth.Declared, labels.Declared, pageref.IsDigits -- works on them
// unchanged.
type Resolved struct {
	Fields map[string]string
	Lists  map[string][]string
	// Source is where the metadata came from.
	Source Source
	// Warnings are the soft disagreements, in field order.
	Warnings []string

	managed bool
}

// MetadataSource is the value --json reports. FromBoth collapses to
// "frontmatter", because that is the location that won every field it could
// win: the schema's question is "which location decided this?", and a third
// enum value would make every consumer handle a case that never changes what
// was published.
func (r Resolved) MetadataSource() Source {
	if r.Source == FromBoth {
		return FromFrontmatter
	}
	return r.Source
}

// InFile reports whether the file's own frontmatter contributed any field
// markfluence understands. It is what check's half-and-half lint asks: a file
// carrying inline keys in a project that has chosen the manifest is the shape
// "no half-and-half" is about.
func (r Resolved) InFile() bool { return r.Source == FromFrontmatter || r.Source == FromBoth }

// InManifest reports whether a pages: entry claimed this file.
func (r Resolved) InManifest() bool { return r.Source == FromManifest || r.Source == FromBoth }

// Managed reports whether this file is claimed: it has a manifest entry, or its
// frontmatter names a page_id. A file nothing claims is skipped rather than
// failed -- repositories legitimately hold markdown that is not published,
// drafts are a normal state, and a glob-driven CI run must not go red because
// somebody added a file.
//
// Deliberately narrower than "declares any markfluence field". A page_id is the
// only field that identifies a page, and an entry is an explicit claim; a file
// carrying `title:` and nothing else has not told anyone which page it is, and
// #139 is precise that a *registered* file lacking a page_id errors -- where
// registered means an entry exists. It is also why Source and this are computed
// from different predicates rather than one: Source answers "who contributed
// metadata", for reporting, and this answers "should update act on it".
func (r Resolved) Managed() bool { return r.managed }

// Resolve merges a file's frontmatter with its pages: entry, if it has one.
//
// key is the file's path normalized by project.NormalizePageKey -- the caller
// normalizes, because it is the caller that knows the file's path relative to
// the root, and a mismatch here means a silent skip.
//
// A coordinate disagreement is an error, so the caller fails that file (or, for
// create, aborts the batch: it preflights everything, and a coordinate wrong in
// any file means the batch's shape is not what the author thinks). A soft
// disagreement is a warning and frontmatter wins.
//
// A *blank* value on either side is not a disagreement. Every null spelling
// reads as "" already, so `parent:` with nothing after it says no more than an
// absent key does -- treating it as a conflicting answer would fail files that
// say nothing at all.
func Resolve(key string, mf *frontmatter.MarkdownFile, root *project.Root) (Resolved, error) {
	entry, hasEntry := entryFor(key, root)

	r := Resolved{Fields: map[string]string{}, Lists: map[string][]string{}}
	if !hasEntry {
		// The common case, and the one that must stay byte-for-byte what it was
		// before this package existed.
		for k, v := range mf.Frontmatter {
			r.Fields[k] = v
		}
		for k, v := range mf.Lists {
			r.Lists[k] = v
		}
		r.Source = sourceOf(declaresPageField(mf.Frontmatter, mf.Lists), false)
		r.managed = hasPageID(mf.Frontmatter)
		return r, nil
	}

	// Start from the entry, then let frontmatter override -- after checking
	// that where both speak they agree about anything destructive.
	for k, v := range entry.Fields {
		r.Fields[k] = v
	}
	for k, v := range entry.Lists {
		r.Lists[k] = v
	}

	var conflicts []string
	for _, k := range sortedKeys(mf.Frontmatter, entry.Fields) {
		file, inFile := nonBlank(mf.Frontmatter, k)
		manifest, inEntry := nonBlank(entry.Fields, k)
		switch {
		case inFile && inEntry && file != manifest:
			if coordinates[k] {
				conflicts = append(conflicts, fmt.Sprintf(
					"%s: frontmatter says %q, %s says %q", k, file, project.Filename, manifest))
				continue
			}
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"%s: frontmatter %q overrides %s's %q", k, file, project.Filename, manifest))
			r.Fields[k] = file
		case inFile:
			r.Fields[k] = file
		}
	}
	for _, k := range sortedListKeys(mf.Lists, entry.Lists) {
		file, inFile := mf.Lists[k]
		manifest, inEntry := entry.Lists[k]
		if inFile && inEntry && !sameList(file, manifest) {
			// labels is the only list field, and it is soft: declaring it
			// asserts a set, which is visible on the page and reversible.
			r.Warnings = append(r.Warnings, fmt.Sprintf(
				"%s: frontmatter [%s] overrides %s's [%s]",
				k, strings.Join(file, ", "), project.Filename, strings.Join(manifest, ", ")))
		}
		if inFile {
			r.Lists[k] = file
		}
	}
	if len(conflicts) > 0 {
		return Resolved{}, fmt.Errorf(
			"%s and this file disagree about where this page is: %s. "+
				"Correct one of them; markfluence will not guess",
			project.Filename, strings.Join(conflicts, "; "))
	}

	// inEntry is "an entry exists", not "an entry declares something": `a.md: {}`
	// is somebody claiming the path, which #139 says must error for want of a
	// page_id rather than be skipped as unmanaged.
	r.Source = sourceOf(declaresPageField(mf.Frontmatter, mf.Lists), true)
	r.managed = true
	return r, nil
}

// entryFor looks up a file's manifest entry.
func entryFor(key string, root *project.Root) (project.Entry, bool) {
	if root == nil || root.Config.Pages == nil {
		return project.Entry{}, false
	}
	e, ok := root.Config.Pages[key]
	return e, ok
}

// HasManifest reports whether the project has chosen the manifest -- a pages:
// key, even an empty one. It is what decides where *new* metadata is written
// (D9): a project that has chosen the manifest never accidentally grows
// frontmatter, and a project without one behaves exactly as it did before.
func HasManifest(root *project.Root) bool {
	return root != nil && root.Config.Pages != nil
}

// declaresPageField reports whether either map holds a non-blank value under a
// field markfluence understands.
//
// Restricted to known fields on purpose: a docs tree carrying Jekyll or Hugo
// frontmatter (layout:, date:, draft:) has said nothing about Confluence, and
// counting any key at all would report every such file as having contributed
// metadata it never had.
func declaresPageField(fields map[string]string, lists map[string][]string) bool {
	for k, v := range fields {
		if project.IsPageField(k) && strings.TrimSpace(v) != "" {
			return true
		}
	}
	for k := range lists {
		if project.IsPageField(k) {
			return true
		}
	}
	return false
}

// hasPageID reports whether a page_id is named and non-blank.
func hasPageID(fields map[string]string) bool {
	_, ok := nonBlank(fields, "page_id")
	return ok
}

func sourceOf(inFile, inEntry bool) Source {
	switch {
	case inFile && inEntry:
		return FromBoth
	case inFile:
		return FromFrontmatter
	case inEntry:
		return FromManifest
	default:
		return Unmanaged
	}
}

// nonBlank reads a key, reporting absent for a blank value: every null spelling
// reads as "" already, so a key with nothing after it says no more than no key.
func nonBlank(m map[string]string, k string) (string, bool) {
	v, ok := m[k]
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	return v, v != ""
}

func sortedKeys(a map[string]string, b map[string]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range []map[string]string{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

func sortedListKeys(a map[string][]string, b map[string][]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, m := range []map[string][]string{a, b} {
		for k := range m {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// sameList compares two declared lists as *sets*, not in order.
//
// labels is the only list field and Confluence has no label order, so
// `[a, b]` and `[b, a]` say the same thing -- warning that one "overrides" the
// other would be noise about a difference that cannot reach the page.
func sameList(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

// KeyFor returns the manifest key for a file: its path relative to the root, in
// the normalized slash form pages: is keyed by.
//
// One copy, used by every command that looks a file up, because the manifest
// side and the argument side must agree exactly -- a mismatch is a silent skip
// (Managed), not an error, so there is nothing to notice if they drift.
//
// A file outside the root has no key and is not an error: a batch may span more
// than one project (docs/root-model.md), and a file belonging to a different
// root than the one being consulted simply has no entry there.
func KeyFor(root *project.Root, absPath string) (string, bool) {
	if root == nil {
		return "", false
	}
	rel, err := filepath.Rel(root.Dir, absPath)
	if err != nil {
		return "", false
	}
	key, err := project.NormalizePageKey(filepath.ToSlash(rel))
	if err != nil {
		return "", false
	}
	return key, true
}
