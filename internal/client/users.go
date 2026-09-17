package client

// The user directory: resolving a name to the account ids carrying it.
//
// Deliberately separate from LookupUser/GetUser in client.go, which go the
// other way -- an account id to a display name. The two share a noun and
// nothing else. That route resolves *any* account, a deactivated one included
// (it answers "Mark Reid (Deactivated)"); this one is a search index that
// cannot see a deactivated account at all. Conflating them would make a lookup
// that works today start failing for departed colleagues.
//
// Evidence for every rule here: docs/confluence/users.md.

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// userSearchPath is the user-directory search route.
//
// It accepts only user-specific CQL fields and is *not* /wiki/rest/api/search,
// so none of that route's rules carry over -- neither its cursor paging nor its
// excerpt handling. It also accepts a field it cannot answer (title~"x") with a
// 200 and zero rows rather than an error, so a clause added here must be tested
// by showing it returns *fewer* rows, never merely that the request succeeded.
const userSearchPath = "/wiki/rest/api/search/user"

// userPageSize is how many rows to ask for per page, and the reason this route
// does not go through listV1.
//
// 100 is the route's hard ceiling, and the ceiling is silent: it echoes back
// whatever limit was asked for and returns at most 100 rows regardless
// (measured at 101, 250 and 500 -- all answered 100, all echoed the request).
// listV1 asks for v1PageSize = 250 and treats a page shorter than its request
// as the end of the collection, so pointing it at this route would ask for 250,
// receive 100, and truncate every result set larger than 100 with no error at
// all. Verified 2026-09-15 against a query matching 304 people.
const userPageSize = 100

// UserMatch is one account the user directory matched.
//
// The account id is carried as the server spelled it and is never parsed: two
// shapes are live on one instance -- a 24-character hex string and a
// "712020:"-prefixed UUID -- so a pattern tight enough for one rejects the
// other (#91).
type UserMatch struct {
	AccountID   string
	DisplayName string
}

// SearchUsers finds accounts whose display name matches query, bounded to max
// rows (0 for every match). The bool reports that more matches exist than were
// returned.
//
// Three properties of the route decide the shape of this loop:
//
//   - It pages by start/limit offset, like the v1 child collections and unlike
//     /search, which ignores start. There is never a _links.next, so a short
//     page is the only end-of-results signal there is.
//   - totalSize counts the rows on the *current page*, not the result set
//     (limit=3 answers 3, limit=500 answers 100, with more beyond either), so
//     nothing here may read it. "More exist" comes from asking for one row more
//     than the caller needs, the way searchCQLBounded does.
//   - A deactivated account is absent from the index and cannot be brought back
//     by sitePermissionTypeFilter, whose none/all/externalCollaborator values
//     were each measured making no difference.
//
// The query is a word-prefix match, in order: "kahn" and "william kahn" both
// find William Kahn-Greene, while "ahn" and "kahn william" find nobody. That is
// the server's semantics, reported to the author in the command's help rather
// than worked around here.
func (c *ConfluenceClient) SearchUsers(query string, max int) ([]UserMatch, bool, error) {
	// escapeCQL is not optional: an unescaped quote in a name ends the string
	// literal and the rest becomes query syntax.
	cql := fmt.Sprintf(`user.fullname~"%s"`, escapeCQL(query))
	size := userRequestSize(max)

	var all []UserMatch
	for page := 0; ; page++ {
		var out struct {
			Results []struct {
				User struct {
					AccountID   string `json:"accountId"`
					DisplayName string `json:"displayName"`
				} `json:"user"`
			} `json:"results"`
		}
		params := url.Values{
			"cql":   {cql},
			"limit": {strconv.Itoa(size)},
			"start": {strconv.Itoa(page * size)},
		}
		if err := c.doJSON(http.MethodGet, c.baseURL+userSearchPath,
			params, nil, &out, timeoutRead); err != nil {
			return nil, false, err
		}

		for _, r := range out.Results {
			// A row with no account id cannot be mentioned -- the id is the
			// whole of what a mention stores -- so it is skipped rather than
			// reported as a person nobody can link to.
			if r.User.AccountID == "" {
				continue
			}
			all = append(all, UserMatch{
				AccountID:   r.User.AccountID,
				DisplayName: r.User.DisplayName,
			})
		}
		if max > 0 && len(all) > max {
			return all[:max], true, nil
		}
		// Measured against the rows the server sent, not the ones kept: a
		// skipped row still means the page was full and there may be another.
		if len(out.Results) < size {
			return all, false, nil
		}
	}
}

// userRequestSize is the limit to ask for, given a row bound.
//
// One more than the caller needs, so the surplus answers "are there more?"
// without touching totalSize, which cannot answer it. Never more than the page
// size, since asking for 500 gets 100 anyway.
func userRequestSize(max int) int {
	if max <= 0 || max >= userPageSize {
		return userPageSize
	}
	return max + 1
}
