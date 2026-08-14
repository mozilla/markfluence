package client

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
)

// ErrSpaceNotFound reports a space key that resolves to nothing. It is a
// sentinel because the caller has to tell it apart from an API failure: an
// unknown key is a usage error, while the search itself failing is not.
var ErrSpaceNotFound = errors.New("space not found")

// TitleMatch is one thing found by title: a page or a folder.
//
// Space and URL are both derived from the row's link, so a row without one has
// neither. Status is "current" or "archived" -- an archived page is reported
// because it still reserves its title, which is what makes it worth knowing
// about (docs/confluence/search.md).
type TitleMatch struct {
	ID     string
	Type   string // "page" or "folder"
	Title  string
	Status string
	Space  string
	URL    string
}

// FindByTitle returns every page and folder whose title matches exactly,
// optionally restricted to a space key (pass "" for site-wide).
//
// It takes two requests because no single API answers the question. The v2
// pages route sees current and archived pages but 404s a folder id; CQL sees
// folders but not archived pages, and has no status field to ask with. Dropping
// either half silently drops a category of answer.
//
// Both requests must succeed. A partial answer is worse than an error here: the
// caller's next move on an empty result is usually to create the page, so
// omitting the half that failed turns a transport error into a duplicate.
//
// The match is exact and case-insensitive on both sides, so the two halves
// cannot disagree about what counts as a match.
func (c *ConfluenceClient) FindByTitle(title, spaceKey string) ([]TitleMatch, error) {
	var spaceID string
	if spaceKey != "" {
		var err error
		spaceID, err = c.ResolveSpaceID(spaceKey)
		if err != nil {
			return nil, err
		}
		// CQL would answer an unknown space key with zero results rather than an
		// error, which reads exactly like "no such page". Refuse it instead.
		if spaceID == "" {
			return nil, fmt.Errorf("%w: %q", ErrSpaceNotFound, spaceKey)
		}
	}

	pages, err := c.SearchPagesByTitle(title, spaceID, StatusCurrent, StatusArchived)
	if err != nil {
		return nil, err
	}
	folders, err := c.findFoldersByTitle(title, spaceKey)
	if err != nil {
		return nil, err
	}

	matches := make([]TitleMatch, 0, len(pages)+len(folders))
	for _, p := range pages {
		matches = append(matches, TitleMatch{
			ID:     p.ID,
			Type:   "page",
			Title:  p.Title,
			Status: p.Status,
			Space:  SpaceKeyFromWebUI(p.Links.WebUI),
			URL:    c.contextURL(p.Links.WebUI),
		})
	}
	matches = append(matches, folders...)
	sortMatches(matches)
	return matches, nil
}

// findFoldersByTitle is the CQL half. The type is pinned to folder rather than
// left open: the index also holds attachments, comments, spaces and users, all
// of which have titles, and none of which is something a caller can pass to
// --parent.
func (c *ConfluenceClient) findFoldersByTitle(title, spaceKey string) ([]TitleMatch, error) {
	cql := fmt.Sprintf(`type = folder and title = "%s"`, escapeCQL(title))
	if spaceKey != "" {
		cql += fmt.Sprintf(` and space = "%s"`, escapeCQL(spaceKey))
	}
	hits, err := c.SearchCQL(cql)
	if err != nil {
		return nil, err
	}
	out := make([]TitleMatch, 0, len(hits))
	for _, h := range hits {
		if h.Content.ID == "" || h.Content.Type != "folder" {
			continue
		}
		out = append(out, TitleMatch{
			ID:     h.Content.ID,
			Type:   h.Content.Type,
			Title:  h.Content.Title,
			Status: h.Content.Status,
			Space:  SpaceKeyFromWebUI(h.URL),
			URL:    c.contextURL(h.URL),
		})
	}
	return out, nil
}

// contextURL turns a context-relative link -- what both a v2 webui and a CQL
// row's url are -- into an absolute one a reader can open. The site URL is used
// rather than any base the response carries, since that must never be the
// gateway host.
func (c *ConfluenceClient) contextURL(path string) string {
	if path == "" {
		return ""
	}
	return c.siteURL + "/wiki" + path
}

// sortMatches orders by space key, then type, then id numerically.
//
// Space leads because it is what tells identically-titled hits apart. The id
// comparison parses rather than comparing strings, or "10" would sort before
// "9". Matches with no derivable space key sort last instead of first, which is
// where an empty string would otherwise put them.
func sortMatches(m []TitleMatch) {
	sort.SliceStable(m, func(i, j int) bool {
		a, b := m[i], m[j]
		if (a.Space == "") != (b.Space == "") {
			return b.Space == ""
		}
		if a.Space != b.Space {
			return a.Space < b.Space
		}
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		ai, aerr := strconv.ParseInt(a.ID, 10, 64)
		bi, berr := strconv.ParseInt(b.ID, 10, 64)
		if aerr == nil && berr == nil {
			return ai < bi
		}
		return a.ID < b.ID
	})
}
