// Package pagestatus models the page_status frontmatter field -- the coloured
// lozenge Confluence shows next to a page title.
//
// The field holds a status's **display name**, matched against the ones the
// page's own space offers:
//
//	page_status: Ready for review
//
// Atlassian's API calls this a content *state*, and the package is named for the
// field rather than the route on purpose: "state" appears nowhere a reader has
// seen, since the UI calls it a status, and markfluence's frontmatter already
// speaks of page_id and page_width. Keep the other two meanings of the word
// straight while working here -- a page's content *status* is
// current/archived/trashed, and `markfluence status` (#148) is something else
// again.
//
// Declared is pure and offline, which is what lets `check` use it. Resolve
// turns a name into the status to write; Apply and Read orchestrate the client.
// The division that matters is that a **name is never sent to Confluence**: see
// Resolve, and client.SetPageState.
//
// Evidence for all of it: docs/confluence/page-status.md.
package pagestatus

import (
	"fmt"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
)

// Field is the frontmatter key.
const Field = "page_status"

// Declared reports the status name a file declares, and whether it declared one
// at all.
//
// An absent key is ("", false, nil) and means the page's status is left alone --
// no request of any kind is made for it.
//
// A **present but empty** value is an error rather than "leave it alone" or
// "clear it". Every null spelling reads as "" by the time it arrives here
// (page_status:, page_status: ~, page_status: null), so an empty value cannot be
// told apart from an author who typed the key and stopped -- the same input
// labels refuses a scalar for. There is deliberately no spelling that clears a
// status: labels can offer one because `labels: []` is distinguishable from a
// null scalar, and a scalar field has no equivalent.
func Declared(fields map[string]string) (string, bool, error) {
	raw, present := fields[Field]
	if !present {
		return "", false, nil
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", false, fmt.Errorf(
			"frontmatter %q has no value; remove the key to leave the page's "+
				"status alone, or give it the name of a status the space offers", Field)
	}
	return name, true, nil
}

// Resolve turns a declared name into the status to write, asking what pageID
// itself may be given.
//
// It must be the page the status will be written to, and that is not a
// convenience: the vocabulary is per **(caller, page)**, not per space. The
// same account, in one space, was offered four statuses on a page it had
// created and three on a page it had not -- and the write enforces the
// difference, with `User is not permitted to use this ContentState on this
// content.` Asking a different page in the same space is therefore a guess,
// and caching one page's answer for another is a wrong one.
//
// The match is case-insensitive, and this is the one place that decides so.
// Unlike labels -- which repairs case and warns, because it sends the author's
// string to the server -- nothing here transmits the file's spelling: what
// travels is the id. So a case variant cannot reach the page, cannot fail to
// converge, and is not worth a warning. Two of the space's own statuses
// differing only in case are refused as ambiguous rather than resolved by
// guessing.
//
// A name that matches nothing is a local failure carrying the space's list,
// because that list is the only place an author can learn it: the vocabulary is
// per-space server state, so there is nothing for --help to document and nothing
// completion can offer (it may not call Confluence).
func Resolve(c *client.ConfluenceClient, pageID, name string) (client.ContentState, error) {
	available, err := c.AvailableStates(pageID)
	if err != nil {
		return client.ContentState{}, err
	}

	matches := matching(available.Space, name)
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return client.ContentState{}, unknownStatus(name, available)
	default:
		return client.ContentState{}, fmt.Errorf(
			"%s %q matches more than one of this page's statuses (%s); "+
				"write it exactly as Confluence spells it",
			Field, name, strings.Join(names(matches), ", "))
	}
}

// matching finds every status whose name matches, case-insensitively.
func matching(states []client.ContentState, name string) []client.ContentState {
	want := strings.ToLower(strings.TrimSpace(name))
	var out []client.ContentState
	for _, state := range states {
		if strings.ToLower(state.Name) == want {
			out = append(out, state)
		}
	}
	return out
}

// unknownStatus is the error an unmatched name gets. It names every status the
// page can be given, since a validation failure is where most authors will
// first learn what is allowed.
//
// "This page", not "this space", and the wording is deliberate: Confluence
// decides the list per (caller, page), not per space -- one account was offered
// four statuses on a page it had created and three on a page in the same space
// that it had not. Saying "this space offers" would send an author looking at
// space settings for a difference that is not there.
//
// A name matching one of the caller's *custom* statuses gets its own message.
// Those follow the account, so the file works for whoever created the status
// and fails for everyone else -- and a message listing what the page allows
// without mentioning the one the author can plainly see in Confluence's own
// picker is how that becomes an unresolvable bug report.
func unknownStatus(name string, available client.StateVocabulary) error {
	// Before the custom branch: with nothing to list, that branch's message
	// ends "This page can be given " and trails off. A page whose space
	// offers nothing while the caller has a same-named custom status is the
	// narrow case, but it is the one where the author most needs a sentence
	// that finishes.
	if len(available.Space) == 0 {
		if len(matching(available.Custom, name)) > 0 {
			return fmt.Errorf(
				"%s %q is one of your own custom statuses, which cannot be published "+
					"from a file, and this page can be given no others", Field, name)
		}
		return fmt.Errorf("%s %q: this page can be given no statuses", Field, name)
	}
	if len(matching(available.Custom, name)) > 0 {
		return fmt.Errorf(
			"%s %q is one of your own custom statuses, which cannot be published "+
				"from a file: a custom status exists for your account alone, so the "+
				"file would fail for everyone else. This page can be given %s",
			Field, name, strings.Join(names(available.Space), ", "))
	}
	return fmt.Errorf("invalid %s %q; this page can be given %s",
		Field, name, strings.Join(names(available.Space), ", "))
}

// names lists states' display names in the order the space reports them, which
// is the order Confluence's own picker uses.
func names(states []client.ContentState) []string {
	out := make([]string, 0, len(states))
	for _, s := range states {
		out = append(out, s.Name)
	}
	return out
}

// Action reports what Apply did.
type Action struct {
	Name   string
	Action string // "set" or "unchanged"
}

// Apply sets a page's status to state, and reports "unchanged" without writing
// when it already holds it.
//
// The read is not an optimisation. A state write **bumps the page version** --
// minorEdit false, empty message, indistinguishable in page history from an
// ordinary edit -- so a pass that PUT unconditionally would add a version to
// every page on every run for a field that had not changed. Labels are the
// opposite case (a label write bumps nothing), which is why that pass needs no
// equivalent.
func Apply(c *client.ConfluenceClient, pageID string, state client.ContentState) (Action, error) {
	current, err := c.PageState(pageID)
	if err != nil {
		return Action{}, err
	}
	if current != nil && current.ID == state.ID {
		return Action{Name: state.Name, Action: "unchanged"}, nil
	}
	if err := c.SetPageState(pageID, state.ID); err != nil {
		return Action{}, err
	}
	return Action{Name: state.Name, Action: "set"}, nil
}

// VersionAfter reports the page's version once a status has been written,
// falling back to fallback when it cannot be read.
//
// This exists because a status write is the **only** metadata pass that bumps
// the page version. The body PUT's own version is what a publish records as its
// merge base (#149), and the width and label passes that follow leave it alone
// -- a content-property write and a label write both do. A status write does
// not, so recording the body's version would leave every page carrying a status
// one version behind the page itself, and the next update would refuse the file
// as diverged. That is not a hypothetical: it is what the first live create with
// a page_status did.
//
// A read, rather than fallback+1, because the response to the status PUT does
// not carry a page version and inferring one would be wrong the moment somebody
// else edited in between -- which is exactly the case the base exists to catch.
// Called only when a status was actually written, so it costs nothing otherwise.
//
// The error is returned rather than folded into the fallback because failing
// here is not benign: the status write has already landed, so the fallback is a
// version *known* to be behind the page, and recording it makes the next update
// of that file refuse it as diverged -- reporting a conflict for a change
// markfluence itself made. The caller must say so rather than leave that
// inexplicable.
func VersionAfter(c *client.ConfluenceClient, pageID string, fallback int) (int, error) {
	page, err := c.GetPageOrNil(pageID)
	if err != nil {
		return fallback, err
	}
	if page == nil {
		return fallback, fmt.Errorf("page %s could not be found", pageID)
	}
	return page.Version.Number, nil
}

// Read reports a live page's status, or nil when it has none.
func Read(c *client.ConfluenceClient, pageID string) (*client.ContentState, error) {
	return c.PageState(pageID)
}

// Available lists the statuses this page may be given, for reporting rather
// than validation -- info's row. The order is the one the route reports, which
// is the order Confluence's picker shows and therefore the order an author
// recognises.
//
// "This page", not "this space": see Resolve. info is reporting what the page
// in front of it can hold, which is the useful answer and also the only
// accurate one.
func Available(c *client.ConfluenceClient, pageID string) ([]client.ContentState, error) {
	states, err := c.AvailableStates(pageID)
	if err != nil {
		return nil, err
	}
	// The space's own, not the union: a custom status belongs to the account
	// that made it, so reporting one as something a page_status: line may say
	// would be wrong for anyone else reading the same page.
	return states.Space, nil
}

// Names lists states' display names, in the order given.
func Names(states []client.ContentState) []string { return names(states) }
