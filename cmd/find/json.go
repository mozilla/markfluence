package find

import "github.com/mozilla/markfluence/internal/client"

// jsonFindResult is find's --json result shape: one object per match, so
// `.results[] | select(.type == "page") | .id` works directly and summary.total
// is the match count.
//
// status is carried rather than assumed. Unlike children, which lists only live
// nodes, a match here may be an archived page -- one that does not appear in
// the page tree but still reserves its title -- and a consumer that cannot tell
// the two apart would report an unusable id as a live page.
//
// space and url are both derived from the match's link, so a row missing one is
// missing both.
type jsonFindResult struct {
	OK     bool    `json:"ok"`
	ID     string  `json:"id"`
	Type   string  `json:"type"`
	Title  string  `json:"title"`
	Space  *string `json:"space"`
	Status string  `json:"status"`
	URL    *string `json:"url"`
}

func buildResult(m client.TitleMatch) jsonFindResult {
	return jsonFindResult{
		OK:     true,
		ID:     m.ID,
		Type:   m.Type,
		Title:  m.Title,
		Space:  nullable(m.Space),
		Status: m.Status,
		URL:    nullable(m.URL),
	}
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
