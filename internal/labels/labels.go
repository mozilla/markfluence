// Package labels models the labels frontmatter field and its mapping to
// Confluence's page labels.
//
// The field is a YAML sequence of names:
//
//	labels: [ci/cd, howto, runbook]
//
// Its semantics mirror page_width's asymmetry, for the same reason: a declared
// field is asserted, an absent one is left alone. Declared means the set is
// made exact -- a label on the page that is not in the file is removed -- and
// "labels: []" means remove every managed label. An absent key means
// markfluence does not touch the page's labels at all, so a page labeled by
// hand is not silently stripped by a run that never mentioned labels.
//
// markfluence manages the "global" prefix and nothing else. A my:, team: or
// system: label is read and displayed, never written and never removed: a
// colon cannot appear in a label name at all, so there is no frontmatter
// spelling for those and removing one would delete data the file could not
// have expressed. Every label in the SRE space is global (see
// docs/confluence/labels.md).
//
// Validation mirrors the server's own reject set rather than inventing an
// allowlist, so real labels like ci/cd, dataops_reports and héllo-wörld publish
// unchanged. It refuses rather than repairs, with one exception: case, which is
// lowercased locally with a warning, because Confluence lowercases server-side
// and a file that disagreed would never converge.
//
// The vocabulary logic is pure; Apply and Read orchestrate the client calls.
package labels

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"

	"github.com/mozilla/markfluence/internal/client"
)

// Field is the frontmatter key this package owns.
const Field = "labels"

// ManagedPrefix is the only label namespace markfluence writes or removes.
const ManagedPrefix = "global"

// RejectChars is the set of characters Confluence refuses in a label name,
// taken from its own 400 body rather than guessed:
//
//	label.contains.invalid.chars … space ! # & ( ) * , . : ; < > ? @ [ ] ^
//
// The space and the comma are the dangerous two. Confluence treats them as
// *separators*, splitting a name and validating the pieces, so "Runbook Two"
// posts as two labels with a 200 and no warning -- which under the
// assert-exactly rule reads back as neither and is re-added on every run
// forever. Refusing them is the whole point of validating at all.
//
// A colon being in the set is why there is no prefix syntax, and a "." being
// in it is why a version-shaped label (v1.2) is impossible. That is
// Confluence's rule, not markfluence's, which is why the error quotes the set
// instead of leaving an author to guess.
const RejectChars = ` !#&()*,.:;<>?@[]^`

// MaxNameUnits is the longest label name Confluence accepts, in **UTF-16 code
// units** -- not bytes and not runes. Measured: 255 × "é" (510 bytes) is
// accepted, 256 × "é" is not, and 128 emoji (128 runes, 256 units) is not. A
// byte-based check would refuse a legal 255-character accented label and a
// rune-based one would accept an emoji label the server rejects.
const MaxNameUnits = 255

// Set is the label set a file asserts: the normalized names, whether the key
// was present at all, and any warning raised while normalizing.
//
// One struct rather than four return values because Declared is the call every
// command makes, and a signature of (names, declared, warnings, err) is four
// opportunities to drop the warnings on the floor.
type Set struct {
	// Names is sorted and deduplicated, and every entry is valid.
	Names []string

	// Declared reports whether the file had a labels key at all. False means
	// the live page's labels are not touched; it is not the same as an empty
	// Names, which means remove them all.
	Declared bool

	// Warnings are author-facing notes about what normalization changed.
	Warnings []string
}

// Action is the outcome for one label in an asserted set.
type Action struct {
	Name   string
	Action string // "added", "removed", or "unchanged"
}

// Action values.
const (
	ActionAdded     = "added"
	ActionRemoved   = "removed"
	ActionUnchanged = "unchanged"
)

// Declared reads and validates the labels field from a file's frontmatter.
//
// It takes both maps because a labels key can arrive in either, and the two
// mean different things. A sequence is the field; a *scalar* is an error rather
// than a one-element list, because this field is destructive -- declaring it
// removes every label not listed -- so "labels:" with nothing after it would
// otherwise mean "strip every label off this page" on a file where an author
// most likely typed a key and got distracted. "labels: []" stays the one way to
// say that, and it has to be written on purpose.
func Declared(lists map[string][]string, frontmatter map[string]string) (Set, error) {
	if raw, scalar := frontmatter[Field]; scalar {
		// An empty or null value gets its own message. Pointing an author at
		// "labels: []" when that is nearly what they already wrote would read
		// as a formatting nit, when the actual point is that the two mean
		// opposite things: one is unfinished, the other strips the page.
		if strings.TrimSpace(raw) == "" {
			return Set{}, fmt.Errorf(
				"frontmatter %q has no value; remove the key to leave the page's "+
					"labels alone, or write %s [] to remove every label", Field, Field+":")
		}
		return Set{}, fmt.Errorf(
			"frontmatter %q must be a list, not a single value: write %s [%s]",
			Field, Field+":", strings.TrimSpace(raw))
	}
	declared, ok := lists[Field]
	if !ok {
		return Set{}, nil
	}

	seen := make(map[string]bool, len(declared))
	set := Set{Declared: true, Names: make([]string, 0, len(declared))}
	for _, raw := range declared {
		// Validated as written *first*, so the message quotes the label the
		// author can actually find in their file. Lowercasing before validating
		// reported `label "runbook two"` for a file that says "Runbook Two",
		// which sends them searching for a string that is not there.
		if err := Validate(raw); err != nil {
			return Set{}, err
		}
		name, changed := Normalize(raw)
		// Re-checked after normalizing because case folding can change length
		// in UTF-16 units: "İ" (U+0130) lowercases to two code points. Nothing
		// realistic reaches this, and a label that passed as written and fails
		// lowercased would otherwise be refused by the server instead.
		if err := Validate(name); err != nil {
			return Set{}, err
		}
		if changed {
			set.Warnings = append(set.Warnings, fmt.Sprintf(
				"label %q is not lowercase; publishing it as %q, which is what "+
					"Confluence stores. Update the file to match.", raw, name))
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		set.Names = append(set.Names, name)
	}
	sort.Strings(set.Names)
	return set, nil
}

// Normalize lowercases a label name, reporting whether that changed it.
//
// Lowercasing is the only repair this package makes, and it is a repair rather
// than a refusal because Confluence lowercases server-side: a file declaring
// "Runbook" would read back "runbook", differ from itself, and be rewritten on
// every run. Lowercasing locally is what makes the comparison converge. The
// caller warns, so the author can make the file say what will happen.
//
// Note what is *not* trimmed. A leading or trailing space is refused by
// Validate like an inner one, because a space is a separator server-side and
// silently trimming it would repair exactly the class of input that produced
// the "continuous" + "delivery" pair in the SRE space.
func Normalize(name string) (string, bool) {
	lowered := strings.ToLower(name)
	return lowered, lowered != name
}

// Validate reports why a label name is unusable, or nil.
//
// Offline and exact: every rule here is one the server enforces, so a name that
// passes is a name that publishes as itself. The reject set is quoted in the
// message because half of it is surprising -- a "." means v1.2 is impossible --
// and an author should not have to bisect their own label to find out.
func Validate(name string) error {
	if strings.TrimSpace(name) == "" {
		if name == "" {
			return fmt.Errorf("label is empty")
		}
		return fmt.Errorf("label %q is only whitespace", name)
	}
	if i := strings.IndexAny(name, RejectChars); i >= 0 {
		return fmt.Errorf(
			"label %q contains %q, which Confluence refuses; a space or comma is "+
				"a separator there, not a character, so %q would publish as "+
				"several labels. Invalid: %s",
			name, string(name[i]), name, describeRejectChars())
	}
	// A tab is refused server-side too and is not in the reject set, so the
	// whitespace check is separate rather than folded into it.
	for _, r := range name {
		if unicode.IsSpace(r) {
			return fmt.Errorf("label %q contains whitespace (%q), which Confluence refuses",
				name, string(r))
		}
	}
	if n := len(utf16.Encode([]rune(name))); n > MaxNameUnits {
		return fmt.Errorf("label %q is %d UTF-16 code units; Confluence allows %d",
			name, n, MaxNameUnits)
	}
	return nil
}

// describeRejectChars renders the reject set for an error message, naming the
// space rather than printing one where it would be invisible.
func describeRejectChars() string {
	parts := []string{"space"}
	for _, r := range RejectChars {
		if r != ' ' {
			parts = append(parts, string(r))
		}
	}
	return strings.Join(parts, " ")
}

// Global returns the names of the managed labels in a page's label list,
// sorted. The unmanaged ones are dropped here rather than by the client,
// because info needs the full list.
func Global(live []client.Label) []string {
	out := make([]string, 0, len(live))
	for _, l := range live {
		if l.Prefix == ManagedPrefix {
			out = append(out, l.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Unmanaged returns the labels markfluence will not touch, as "prefix:name",
// sorted. Only info shows these.
func Unmanaged(live []client.Label) []string {
	out := make([]string, 0, len(live))
	for _, l := range live {
		if l.Prefix != ManagedPrefix {
			out = append(out, l.Prefix+":"+l.Name)
		}
	}
	sort.Strings(out)
	return out
}

// Diff reports what asserting declared over live requires: what to add, what to
// remove, and what is already right.
//
// Sets, not sequences: order and duplicates carry no meaning to Confluence, so
// a file whose list is merely differently ordered needs no write at all. Every
// returned slice is sorted, because neither label GET returns a useful order
// and unsorted output would be unstable across runs for no reason a reader
// could explain.
//
// This lives here so no command reimplements it. Three commands need the same
// answer, and three copies of a set difference is three chances to leave a
// label behind.
func Diff(declared, live []string) (add, remove, unchanged []string) {
	inLive := make(map[string]bool, len(live))
	for _, l := range live {
		inLive[l] = true
	}
	inDeclared := make(map[string]bool, len(declared))
	for _, d := range declared {
		inDeclared[d] = true
	}
	for d := range inDeclared {
		if inLive[d] {
			unchanged = append(unchanged, d)
		} else {
			add = append(add, d)
		}
	}
	for l := range inLive {
		if !inDeclared[l] {
			remove = append(remove, l)
		}
	}
	sort.Strings(add)
	sort.Strings(remove)
	sort.Strings(unchanged)
	return add, remove, unchanged
}

// Actions renders a diff as the per-label report both the human and --json
// paths print: the full declared set plus whatever was removed, sorted by name.
//
// The full set rather than only the changes, so a consumer can read a page's
// labels off a publish without a second call.
func Actions(add, remove, unchanged []string) []Action {
	out := make([]Action, 0, len(add)+len(remove)+len(unchanged))
	for _, n := range add {
		out = append(out, Action{Name: n, Action: ActionAdded})
	}
	for _, n := range remove {
		out = append(out, Action{Name: n, Action: ActionRemoved})
	}
	for _, n := range unchanged {
		out = append(out, Action{Name: n, Action: ActionUnchanged})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Read returns a page's labels, unfiltered.
func Read(c *client.ConfluenceClient, pageID string) ([]client.Label, error) {
	return c.ListLabels(pageID)
}

// Apply asserts s over a page's managed labels, returning one Action per label
// in the declared set plus one per label removed.
//
// A set that is already right makes no write: additions go out as one batched
// POST and each removal is its own DELETE, both skipped when there is nothing
// to do. Adding before removing is deliberate -- if the run dies between the
// two, the page is left over-labeled rather than under-labeled, and the next
// run converges either way.
//
// Calling this with an undeclared Set is a caller bug rather than a no-op with
// a plausible reading: it would mean "remove every label" for a file that said
// nothing about labels.
func Apply(c *client.ConfluenceClient, pageID string, s Set) ([]Action, error) {
	if !s.Declared {
		return nil, fmt.Errorf("internal: labels.Apply called for a file that declares none")
	}
	live, err := c.ListLabels(pageID)
	if err != nil {
		return nil, err
	}
	add, remove, unchanged := Diff(s.Names, Global(live))
	if err := c.AddLabels(pageID, add); err != nil {
		return nil, err
	}
	for _, name := range remove {
		if err := c.RemoveLabel(pageID, name); err != nil {
			return nil, err
		}
	}
	return Actions(add, remove, unchanged), nil
}

// Plan is Apply's dry run: the same Actions, with nothing written.
func Plan(c *client.ConfluenceClient, pageID string, s Set) ([]Action, error) {
	if !s.Declared {
		return nil, fmt.Errorf("internal: labels.Plan called for a file that declares none")
	}
	live, err := c.ListLabels(pageID)
	if err != nil {
		return nil, err
	}
	return Actions(Diff(s.Names, Global(live))), nil
}
