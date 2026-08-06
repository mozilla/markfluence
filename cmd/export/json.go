package export

import "github.com/mozilla/markfluence/internal/client"

// jsonExportResult is export's --json result shape. The target is the page, as
// with info and read, and the files written to disk hang off it -- matching how
// update and create nest an attachments array on their per-page result.
type jsonExportResult struct {
	OK          bool               `json:"ok"`
	PageID      string             `json:"page_id"`
	Title       string             `json:"title"`
	Space       string             `json:"space"`
	Parent      *string            `json:"parent"`
	DryRun      bool               `json:"dry_run"`
	Status      string             `json:"status"`
	DestPath    *string            `json:"dest_path"`
	Attachments []jsonExportAttach `json:"attachments"`
	Warnings    []string           `json:"warnings"`
	Error       *string            `json:"error"`
	Code        *string            `json:"code"`
}

// jsonExportAttach is one attachment's outcome. dest_path is null for an
// attachment that was not written -- unreferenced, or one whose path could not
// be resolved.
type jsonExportAttach struct {
	Status   string  `json:"status"`
	Filename string  `json:"filename"`
	DestPath *string `json:"dest_path"`
	Error    *string `json:"error"`
	Code     *string `json:"code"`
}

func buildResult(r result) jsonExportResult {
	res := jsonExportResult{
		OK:          r.err == nil,
		DryRun:      dryRun,
		Status:      r.pageStatus,
		DestPath:    nullable(r.destPath),
		Attachments: []jsonExportAttach{},
		Warnings:    []string{},
	}
	if r.page != nil {
		res.PageID = r.page.ID
		res.Title = r.page.Title
		res.Space = client.SpaceKeyFromWebUI(r.page.Links.WebUI)
		res.Parent = nullable(r.page.ParentID)
	}
	for _, a := range r.attachments {
		entry := jsonExportAttach{
			Status:   a.status,
			Filename: a.name,
			DestPath: nullable(a.destPath),
		}
		if a.err != nil {
			msg := a.err.Error()
			entry.Error = &msg
		}
		if a.code != "" {
			code := string(a.code)
			entry.Code = &code
		}
		res.Attachments = append(res.Attachments, entry)
	}
	res.Warnings = append(res.Warnings, r.warnings...)
	if r.err != nil {
		msg := r.err.Error()
		res.Error = &msg
	}
	if r.code != "" {
		code := string(r.code)
		res.Code = &code
	}
	return res
}

// nullable maps an empty string to a JSON null, else a pointer to the value.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
