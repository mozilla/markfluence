package attachmentlist

import "github.com/mozilla/markfluence/internal/client"

// jsonListResult is attachment-list's --json result shape: one object per
// attachment, so `.results[] | .filename` works directly and summary.total is
// the attachment count.
//
// comment is the raw stored comment, and managed/sha256/source are what
// markfluence parses out of it -- both are emitted so a script never has to
// re-parse the comment itself, and nothing markfluence knows is hidden.
//
// source is null for a hand-uploaded attachment and also for one published
// before source paths were recorded; managed distinguishes them, which is why
// it is a field of its own rather than inferred from source being null.
//
// There is deliberately no download_url: built on the site URL it fails under a
// scoped token, and built on the request base it would leak the gateway host
// into reader-facing output. attachment-download is how bytes are fetched.
type jsonListResult struct {
	OK        bool    `json:"ok"`
	ID        string  `json:"id"`
	Filename  string  `json:"filename"`
	Size      int64   `json:"size"`
	MediaType string  `json:"media_type"`
	Version   int     `json:"version"`
	Comment   string  `json:"comment"`
	Managed   bool    `json:"managed"`
	SHA256    *string `json:"sha256"`
	Source    *string `json:"source"`
}

func buildResult(a client.Attachment) jsonListResult {
	m := a.Meta()
	return jsonListResult{
		OK:        true,
		ID:        a.ID,
		Filename:  a.Title,
		Size:      a.Extensions.FileSize,
		MediaType: a.Extensions.MediaType,
		Version:   a.Version.Number,
		Comment:   a.Metadata.Comment,
		Managed:   m.Managed,
		SHA256:    nullable(m.SHA256),
		Source:    nullable(m.Source),
	}
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
