package read

import (
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagewidth"
)

// jsonReadResult is read's --json result: the page's frontmatter fields as
// structured keys plus the body as its own string, with the requested format
// echoed. parent is null for a top-level page; page_width is null when the
// (best-effort) width read fails.
type jsonReadResult struct {
	OK        bool               `json:"ok"`
	PageID    string             `json:"page_id"`
	Title     string             `json:"title"`
	Space     string             `json:"space"`
	Parent    *string            `json:"parent"`
	PageWidth *jsonout.PageWidth `json:"page_width"`
	Format    string             `json:"format"`
	Body      string             `json:"body"`
}

// buildResult assembles the JSON result. Unlike the human path, the metadata is
// carried in structured fields (not embedded YAML), so body is the bare content
// for both formats. A width read is attempted regardless of format so the schema
// is stable; failure leaves page_width null.
func buildResult(c *client.ConfluenceClient, page *client.Page, format, body string) jsonReadResult {
	res := jsonReadResult{
		OK:     true,
		PageID: page.ID,
		Title:  page.Title,
		Space:  client.SpaceKeyFromWebUI(page.Links.WebUI),
		Format: format,
		Body:   body,
	}
	if page.ParentID != "" {
		p := page.ParentID
		res.Parent = &p
	}
	if w, explicit, err := pagewidth.Read(c, page.ID); err == nil {
		res.PageWidth = &jsonout.PageWidth{Value: string(w), Default: !explicit}
	}
	return res
}
