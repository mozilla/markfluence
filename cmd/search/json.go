package search

import "github.com/mozilla/markfluence/internal/client"

// jsonSearchResult is search's --json result shape: one object per hit, so
// `.results[] | .id` works directly and summary.total is the match count.
//
// The array is in the server's relevance order, best first, which a consumer
// cannot reproduce -- score comes back as 0.0 on every row -- so re-sorting it
// discards the only ranking there is.
//
// There is deliberately no status field, unlike find. The search index cannot see
// archived content, so every hit is current; carrying the field would suggest
// search can report an archived page, which is exactly the case it misses and the
// reason to reach for find instead.
//
// space, url and excerpt are all null-able: the first two are derived from the
// row's link, and a hit whose match was in its title can have an empty excerpt.
type jsonSearchResult struct {
	OK      bool    `json:"ok"`
	ID      string  `json:"id"`
	Type    string  `json:"type"`
	Title   string  `json:"title"`
	Space   *string `json:"space"`
	URL     *string `json:"url"`
	Excerpt *string `json:"excerpt"`
}

// jsonSearchSummary is search's summary.
//
// basicSummary cannot be reused: it is additionalProperties:false, and both
// fields below are load-bearing.
//
// truncated is a flag rather than a count of what was dropped, because there is
// no trustworthy count to report -- totalSize drifted 294/292/291 against 289
// rows actually collected, and read 1 against an empty results array.
//
// skipped counts index rows that had no addressable content object. Reachable
// only through --cql or --type all, and reported rather than dropped quietly:
// `type = space` matches hundreds of such rows, and without the count this
// envelope would say total 0 and exit 0 for a query that matched plenty.
type jsonSearchSummary struct {
	Total     int  `json:"total"`
	Succeeded int  `json:"succeeded"`
	Failed    int  `json:"failed"`
	Truncated bool `json:"truncated"`
	Skipped   int  `json:"skipped"`
}

func buildResult(m client.SearchMatch) jsonSearchResult {
	return jsonSearchResult{
		OK:      true,
		ID:      m.ID,
		Type:    m.Type,
		Title:   m.Title,
		Space:   nullable(m.Space),
		URL:     nullable(m.URL),
		Excerpt: nullable(m.Excerpt),
	}
}

// buildSummary is split out so the conformance test can build the summary this
// command really emits rather than a hand-copied literal.
//
// failed is always 0: there is no per-row failure variant, since a failed search
// is an error object with no envelope at all.
func buildSummary(res client.SearchResults) jsonSearchSummary {
	return jsonSearchSummary{
		Total:     len(res.Matches),
		Succeeded: len(res.Matches),
		Failed:    0,
		Truncated: res.More,
		Skipped:   res.Skipped,
	}
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
