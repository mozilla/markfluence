package client

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
)

// Space metadata, for space-info (#170). Everything here is v1 except the page
// walk: v1 is where a space's operations and its description live, and the
// operations are the whole read/write answer.
//
// Evidence: docs/confluence/spaces.md.

// SpaceOperation is one thing the calling account may do in a space, as the
// ?expand=operations expansion reports it.
type SpaceOperation struct {
	Operation  string `json:"operation"`
	TargetType string `json:"targetType"`
}

// SpaceSummary is a space's identity plus what the calling account may do in it.
//
// Operations is **nil when the expansion did not come back**, which is not the
// same as an empty list: the first means "could not be determined", the second
// would mean "may do nothing at all". A caller reporting access has to keep
// those apart.
type SpaceSummary struct {
	ID            string
	Key           string
	Name          string
	Type          string
	Status        string
	HomepageID    string
	HomepageTitle string
	Description   string
	Labels        []string
	Operations    []SpaceOperation
}

// CanCreatePages reports whether the account may create pages in the space, and
// whether that could be determined at all.
//
// This is deliberately not called "can write". `create:page` is a *space* grant;
// permission to edit an existing page is not in this list and is not a space
// property at all (#168) -- docs/confluence/page-status.md records an account
// that could create pages in a space and could not edit a page in it. A caller
// that renders this as "write" is promising something Confluence does not.
func (s *SpaceSummary) CanCreatePages() (allowed, known bool) {
	if s.Operations == nil {
		return false, false
	}
	return s.hasOperation("create", "page"), true
}

// CanRead reports whether the account may read the space, and whether that
// could be determined. A space that answered this request at all is readable,
// so the interesting case is only the unknown one.
func (s *SpaceSummary) CanRead() (allowed, known bool) {
	if s.Operations == nil {
		return false, false
	}
	return s.hasOperation("read", "space"), true
}

// IsAdmin reports whether the account administers the space, and whether that
// could be determined.
//
// `administer:space` alone, not a heuristic over the set: an admin's row also
// carries archive/delete/export/manage_* operations, so any of them would do,
// and picking the one Atlassian names for the permission keeps this from
// drifting when the others change.
func (s *SpaceSummary) IsAdmin() (allowed, known bool) {
	if s.Operations == nil {
		return false, false
	}
	return s.hasOperation("administer", "space"), true
}

func (s *SpaceSummary) hasOperation(operation, target string) bool {
	for _, op := range s.Operations {
		if op.Operation == operation && op.TargetType == target {
			return true
		}
	}
	return false
}

// GetSpace looks a space up by key, returning nil when there is no such space.
//
// One request answers identity, homepage, description, labels *and* what the
// calling account may do, which is why this is the v1 route rather than v2's
// /spaces?keys=: the operations expansion exists only here. An unknown key is
// nil rather than an error, so the caller can report a typo in its own words.
//
// The GET on this route is **undocumented** -- Atlassian's OpenAPI document
// carries only PUT and DELETE for it -- so its scope is observed rather than
// derived, the same footing as the three v1 child-listing routes
// (docs/confluence/api.md#scopes). It works with the union markfluence already
// requires.
func (c *ConfluenceClient) GetSpace(spaceKey string) (*SpaceSummary, error) {
	// The id is a **number** here where every v2 route reports it as a string,
	// and homepage.id in the same response is a string -- so json.Number, and
	// do not "tidy" these into plain strings. Measured: a plain string field
	// fails with "cannot unmarshal number into Go struct field .id".
	var out struct {
		ID         json.Number      `json:"id"`
		Key        string           `json:"key"`
		Name       string           `json:"name"`
		Type       string           `json:"type"`
		Status     string           `json:"status"`
		Operations []SpaceOperation `json:"operations"`
		// The homepage expansion carries its title, so a space's homepage costs
		// no second request.
		Homepage struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"homepage"`
		Description struct {
			Plain struct {
				Value string `json:"value"`
			} `json:"plain"`
		} `json:"description"`
		Metadata struct {
			Labels struct {
				Results []Label `json:"results"`
			} `json:"labels"`
		} `json:"metadata"`
	}
	params := url.Values{"expand": {"operations,description.plain,metadata.labels,homepage"}}
	path := c.baseURL + "/wiki/rest/api/space/" + url.PathEscape(spaceKey)
	if err := c.doJSON(http.MethodGet, path, params, nil, &out, timeoutRead); err != nil {
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	space := SpaceSummary{
		ID:            out.ID.String(),
		Key:           out.Key,
		Name:          out.Name,
		Type:          out.Type,
		Status:        out.Status,
		HomepageID:    out.Homepage.ID,
		HomepageTitle: out.Homepage.Title,
		Description:   out.Description.Plain.Value,
		Operations:    out.Operations,
	}
	for _, l := range out.Metadata.Labels.Results {
		space.Labels = append(space.Labels, l.Name)
	}
	return &space, nil
}

// SpaceStateSettings returns the page statuses a space is configured with, or
// nil when the caller may not see them.
//
// This is the authoritative answer to "what statuses does this space offer" and
// it is **space-admin only**: an ordinary author, including a collaborator who
// can create pages, gets a 403 (measured -- docs/confluence/page-status.md).
// That is reported as (nil, nil) rather than an error, because for this route
// being refused is the normal case rather than a failure.
//
// Do not reach for /space/{key}/state instead. It answers 200 to anyone and
// returns the four product defaults whatever the space is configured with, so
// it is wrong in the direction that looks right.
func (c *ConfluenceClient) SpaceStateSettings(spaceKey string) ([]ContentState, error) {
	var out struct {
		SpaceContentStates []ContentState `json:"spaceContentStates"`
	}
	path := c.baseURL + "/wiki/rest/api/space/" + url.PathEscape(spaceKey) + "/state/settings"
	if err := c.doJSON(http.MethodGet, path, nil, nil, &out, timeoutRead); err != nil {
		var he *HTTPError
		if errors.As(err, &he) && he.StatusCode == http.StatusForbidden {
			return nil, nil
		}
		if notFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return out.SpaceContentStates, nil
}

// WalkSpacePages hands every page in a space to visit, one response page at a
// time, following the v2 cursor.
//
// A callback rather than a slice because the one caller counts and discards: a
// 20k-page space should not be held in memory to be measured. It shares
// listV2's cursor handling through walkV2 rather than reimplementing it.
//
// **The route's default includes archived pages** -- a space with 37 current
// and 1 archived answers 38 -- so a caller wanting only current pages must read
// each row's Status rather than assume the collection is filtered.
func (c *ConfluenceClient) WalkSpacePages(spaceID string, visit func(Page) error) error {
	return walkV2(c, "/wiki/api/v2/spaces/"+spaceID+"/pages", nil, func(pages []Page) error {
		for _, p := range pages {
			if err := visit(p); err != nil {
				return err
			}
		}
		return nil
	})
}
