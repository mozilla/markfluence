package client

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
)

// Account lookups for user-info (#171): who a token is, who an account id is,
// and where an account may write.
//
// Both identity routes are v1 and need only read:confluence-user, which every
// working token carries -- unlike the user *directory* SearchUsers uses, whose
// granular read:content-details:confluence is implied by nothing (users.md).
// So these answer on tokens where user-find 401s.

// SpaceRef is a space named from somewhere else -- an account's personal space,
// or one row of the operations survey.
type SpaceRef struct {
	ID   string
	Key  string
	Name string
}

// User is an account as the v1 user routes describe it.
//
// PersonalSpace is nil when the account has none, which is a real answer rather
// than a failure: an app account (a service-account token) has no personal
// space, and a person normally does.
type User struct {
	AccountID              string
	DisplayName            string
	PublicName             string
	Email                  string
	AccountType            string
	AccountStatus          string
	IsExternalCollaborator bool
	IsGuest                bool
	PersonalSpace          *SpaceRef
}

// userResponse is the shape both identity routes return.
type userResponse struct {
	AccountID              string `json:"accountId"`
	DisplayName            string `json:"displayName"`
	PublicName             string `json:"publicName"`
	Email                  string `json:"email"`
	AccountType            string `json:"accountType"`
	AccountStatus          string `json:"accountStatus"`
	IsExternalCollaborator bool   `json:"isExternalCollaborator"`
	IsGuest                bool   `json:"isGuest"`
	PersonalSpace          *struct {
		ID   json.Number `json:"id"`
		Key  string      `json:"key"`
		Name string      `json:"name"`
	} `json:"personalSpace"`
}

func (u userResponse) user() *User {
	out := &User{
		AccountID:              u.AccountID,
		DisplayName:            u.DisplayName,
		PublicName:             u.PublicName,
		Email:                  u.Email,
		AccountType:            u.AccountType,
		AccountStatus:          u.AccountStatus,
		IsExternalCollaborator: u.IsExternalCollaborator,
		IsGuest:                u.IsGuest,
	}
	if u.PersonalSpace != nil {
		out.PersonalSpace = &SpaceRef{
			ID: u.PersonalSpace.ID.String(), Key: u.PersonalSpace.Key, Name: u.PersonalSpace.Name,
		}
	}
	return out
}

// personalSpaceExpand is the expansion that carries an account's personal
// space. Its key **cannot be constructed** from the account id: one instance
// carries personal spaces keyed by email (~someone@example.com) alongside
// id-keyed ones (~60c36d07…), both live, so the key is a lookup and never a
// format (users.md).
const personalSpaceExpand = "personalSpace"

// CurrentUser reports the account the configured credentials belong to.
//
// The whole reason user-info exists: nothing else in markfluence can say which
// account a token is, and that is the first question when a publish lands
// somewhere unexpected or a page's history names an id you do not recognise.
func (c *ConfluenceClient) CurrentUser() (*User, error) {
	var out userResponse
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/rest/api/user/current",
		url.Values{"expand": {personalSpaceExpand}}, nil, &out, timeoutRead)
	if err != nil {
		return nil, err
	}
	return out.user(), nil
}

// UserInfo reports an account by id, or nil when there is no such account.
//
// Deliberately not merged with LookupUser, which answers the same route with
// only a display name: that one is called per mention inside a render loop and
// wants the narrowest result, and pagedoc.UserCache stores exactly that.
//
// This route sees a **deactivated** account, which the user directory behind
// user-find cannot under any sitePermissionTypeFilter value. That asymmetry is
// half the reason the one-argument form exists: "who was this person on a
// three-year-old page" has exactly one route (users.md).
func (c *ConfluenceClient) UserInfo(accountID string) (*User, error) {
	var out userResponse
	err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/rest/api/user",
		url.Values{"accountId": {accountID}, "expand": {personalSpaceExpand}},
		nil, &out, timeoutRead)
	switch {
	case err == nil:
		return out.user(), nil
	case notFound(err):
		return nil, nil
	default:
		return nil, err
	}
}

// spacePageSize is what the space collection is asked for per request. Not
// v1PageSize, because this route does not go through listV1 -- see
// WalkSpaceOperations.
const spacePageSize = 250

// maxSpacePages bounds the walk. Termination here is an empty page, so a server
// that clamped start would return rows forever; this is searchCQLBounded's
// guard for the same hazard.
const maxSpacePages = 200

// WalkSpaceOperations hands every space the account can see, with what it may
// do there, to visit.
//
// **It must not go through listV1, and this is the whole reason it exists
// separately.** listV1 stops when a page comes back shorter than it asked for,
// and this route returns short pages that are not the end: asked for 250 from
// start=0 it answered **200**, and start=200 then answered 250 more, against
// 525 spaces in total. Under listV1 that reports 200 spaces and truncates the
// survey silently -- which the first version of this probe did, producing a
// confident wrong answer. Termination is an **empty** page, nothing else.
//
// The operations expansion works on the collection, not just the single-space
// route, which is what makes "where can this account write?" one walk rather
// than one request per space.
func (c *ConfluenceClient) WalkSpaceOperations(visit func(SpaceRef, []SpaceOperation) error) error {
	for page, start := 0, 0; page < maxSpacePages; page++ {
		var out struct {
			Results []struct {
				ID         json.Number      `json:"id"`
				Key        string           `json:"key"`
				Name       string           `json:"name"`
				Operations []SpaceOperation `json:"operations"`
			} `json:"results"`
		}
		q := url.Values{
			"expand": {"operations"},
			"limit":  {strconv.Itoa(spacePageSize)},
			"start":  {strconv.Itoa(start)},
		}
		if err := c.doJSON(http.MethodGet, c.baseURL+"/wiki/rest/api/space",
			q, nil, &out, timeoutRead); err != nil {
			return err
		}
		if len(out.Results) == 0 {
			return nil
		}
		for _, row := range out.Results {
			ref := SpaceRef{ID: row.ID.String(), Key: row.Key, Name: row.Name}
			if err := visit(ref, row.Operations); err != nil {
				return err
			}
		}
		// By rows returned, not by the limit asked for: the two differ here,
		// which is the trap above.
		start += len(out.Results)
	}
	return nil
}
