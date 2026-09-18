package client

import (
	"net/http"
	"net/url"
)

// Page status -- the coloured lozenge Confluence shows next to a page title.
//
// Atlassian calls it a "content state", and the word is worth keeping straight:
// a page's content *status* is current/archived/trashed (Page.Status, and the
// ?status= parameter below), while its content *state* is this. The routes are
// v1 only; v2 exposes no state field on a page in any form, and there is no
// expansion that adds one.
//
// Everything here was established against the live instance:
// docs/confluence/page-status.md.

// ContentState is one page status: the lozenge's id, display name and colour.
//
// The id is what identifies it. A name is a display string that Confluence
// matches literally and case-sensitively, and only when no id is supplied --
// see SetPageState.
type ContentState struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

// PageState reports the page's current status, or nil when it has none.
//
// "None" arrives as a *missing* contentState object rather than an empty one: a
// page that never had a status answers {}, and one whose status was removed
// answers {"lastUpdated":"..."} with no contentState. Both must read as nil, so
// the field is a pointer and its absence is the test -- a value type here would
// make "no status" indistinguishable from a status with a zero id.
func (c *ConfluenceClient) PageState(pageID string) (*ContentState, error) {
	var out struct {
		ContentState *ContentState `json:"contentState"`
	}
	path := c.baseURL + "/wiki/rest/api/content/" + pageID + "/state"
	if err := c.doJSON(http.MethodGet, path, statusCurrent(), nil, &out, timeoutRead); err != nil {
		return nil, err
	}
	return out.ContentState, nil
}

// StateVocabulary is what a page may be given as a status, kept in the two
// halves the route reports rather than merged, because they are not the same
// kind of thing.
//
// Space is the space's own configuration, the same for everyone who can see the
// page. Custom follows the **account**: a custom status one person creates shows
// up as available on pages in spaces they do not own (verified). So only Space
// is valid for a frontmatter field -- a file validated against the union would
// publish for its author and fail for a colleague -- and Custom is carried only
// so that refusal can say why, which is otherwise an unresolvable "it works for
// me" bug report.
type StateVocabulary struct {
	Space  []ContentState
	Custom []ContentState
}

// AvailableStates reports what statuses the page can be given.
//
// Two things about this route decide why it is the one used, both measured:
//
// It is per *page*, and the space-scoped alternatives cannot replace it.
// GET /space/{key}/state returns the four product defaults whatever the space
// actually offers -- it reported "Verified" for a space whose real list is three
// states -- and GET /space/{key}/state/settings, which is correct, is space-admin
// only and 403s for an ordinary author. Every page in a space answers
// identically, so any page id is a probe for its space.
//
// And its two halves mean different things: see StateVocabulary.
func (c *ConfluenceClient) AvailableStates(pageID string) (StateVocabulary, error) {
	var out struct {
		SpaceContentStates  []ContentState `json:"spaceContentStates"`
		CustomContentStates []ContentState `json:"customContentStates"`
	}
	path := c.baseURL + "/wiki/rest/api/content/" + pageID + "/state/available"
	if err := c.doJSON(http.MethodGet, path, nil, nil, &out, timeoutRead); err != nil {
		return StateVocabulary{}, err
	}
	return StateVocabulary{Space: out.SpaceContentStates, Custom: out.CustomContentStates}, nil
}

// SetPageState sets a page's status, by id.
//
// The id is the only thing sent, and that is a safety property rather than
// economy. The body's id is *optional*, and a request without one is a create
// path indistinguishable in shape and status code from this one: a PUT carrying
// {"name":"ready for review","color":"#57d9a3"} against a space whose status is
// spelled "Ready for review" answers 200 and **creates a new custom status**,
// which no API route can delete (DELETE /rest/api/content-states/{id} is a 404,
// the collection a 405). With an id present, name and colour are ignored
// entirely -- the server echoes back its own canonical pair -- so sending only
// the id makes that outcome unreachable. Resolve a name locally instead;
// internal/pagestatus does.
//
// The colour requirement is the tell for a request that lost its id: "color in
// body of content state must be a 6 hex digit color like #04df03!" is a 400 from
// the create path validating a new status, and cannot be reached from here.
//
// An id that does not exist (or that a space admin has disabled) is a clean 404
// naming it, with no side effect.
//
// Unlike UpdatePage this carries no version, so a re-sent request after a lost
// response sets the same id again. It needs none of updateLanded's machinery and
// is retryable as the ordinary idempotent PUT it is.
func (c *ConfluenceClient) SetPageState(pageID string, stateID int64) error {
	payload := map[string]int64{"id": stateID}
	path := c.baseURL + "/wiki/rest/api/content/" + pageID + "/state"
	return c.doJSON(http.MethodPut, path, statusCurrent(), payload, nil, timeoutWrite)
}

// statusCurrent is the ?status= parameter every state route takes. It names the
// *content status* whose lozenge is being read or written; markfluence only ever
// deals with current pages.
func statusCurrent() url.Values {
	return url.Values{"status": {StatusCurrent}}
}
