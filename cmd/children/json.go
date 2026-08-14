package children

import "github.com/mozilla/markfluence/internal/pagetree"

// jsonChildResult is children's --json result shape: one object per node, so
// `.results[] | select(.type == "page") | .id` works directly and summary.total
// is the node count.
//
// The array is flat rather than nested, with parent_id and depth carrying the
// shape. A nested children array would need a recursive schema definition, and
// since nothing here uses omitempty every leaf would have to emit an empty one.
// Flat also means a consumer that does not care about hierarchy can ignore two
// fields instead of recursing.
//
// Order is the walk order -- depth-first, siblings by the position Confluence
// displays them in -- so reading the array top to bottom reproduces the tree.
type jsonChildResult struct {
	OK       bool    `json:"ok"`
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Title    string  `json:"title"`
	Status   string  `json:"status"`
	ParentID string  `json:"parent_id"`
	Depth    int     `json:"depth"`
	Space    *string `json:"space"`
	URL      *string `json:"url"`
}

func buildResult(n pagetree.Node) jsonChildResult {
	return jsonChildResult{
		OK:       true,
		ID:       n.ID,
		Type:     n.Type,
		Title:    n.Title,
		Status:   n.Status,
		ParentID: n.ParentID,
		Depth:    n.Depth,
		Space:    nullable(n.Space),
		URL:      nullable(n.URL),
	}
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
// space and url are both derived from webui, so a row without one has neither.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
