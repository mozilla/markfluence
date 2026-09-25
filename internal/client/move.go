package client

import (
	"fmt"
	"net/http"
)

// Where MovePage puts a page relative to its target.
const (
	// MoveAppend makes the page the last child of the target, a page or a
	// folder.
	MoveAppend = "append"
	// MoveAfter makes the page the next sibling of the target. After a page at
	// the top of a space, that is the top of the space: the only way there,
	// since a page has no parent to append to.
	MoveAfter = "after"
)

// MovePage moves a page and its whole subtree, through the v1 move route:
// PUT /wiki/rest/api/content/{id}/move/{position}/{targetId}.
//
// v1 rather than a v2 page update with a new parentId, because a v1 move
// leaves the page version alone and a v2 one bumps it, and because v2 cannot
// move a page to the top of a space at all (a null parentId is accepted and
// ignored). Keeping the version still is what keeps a move out of update's
// moved-page check and its unchanged-body skip (docs/confluence/api.md).
//
// Only the two positions markfluence uses are accepted. A repeated move to the
// same place changes nothing, so the PUT is safe to retry, which send does.
func (c *ConfluenceClient) MovePage(pageID, position, targetID string) error {
	if position != MoveAppend && position != MoveAfter {
		return fmt.Errorf("MovePage: unsupported position %q", position)
	}
	path := c.baseURL + "/wiki/rest/api/content/" + pageID + "/move/" + position + "/" + targetID
	return c.doJSON(http.MethodPut, path, nil, nil, nil, timeoutWrite)
}
