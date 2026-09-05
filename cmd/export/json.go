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
	ParentType  *string            `json:"parent_type"`
	ParentFile  *string            `json:"parent_file"`
	DryRun      bool               `json:"dry_run"`
	Status      string             `json:"status"`
	DestPath    *string            `json:"dest_path"`
	Attachments []jsonExportAttach `json:"attachments"`
	Warnings    []string           `json:"warnings"`
	Error       *string            `json:"error"`
	Code        *string            `json:"code"`
}

// jsonExportSummary is export's summary. It is a typed struct rather than the
// map every other batch uses because project_file is not a count, and it
// carries skipped because skip-and-resume is how a retry works here: a run that
// exports nothing new is all skipped and has still succeeded.
type jsonExportSummary struct {
	Total       int     `json:"total"`
	Succeeded   int     `json:"succeeded"`
	Failed      int     `json:"failed"`
	Skipped     int     `json:"skipped"`
	ProjectFile *string `json:"project_file"`
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
		// The same condition report() counts as a success, or a consumer
		// filtering on ok gets a different number than the summary states.
		OK:          r.err == nil && !anyAttachmentFailed(r),
		ParentFile:  nullable(r.place.parentFile),
		DryRun:      dryRun,
		Status:      r.pageStatus,
		DestPath:    nullable(r.destPath),
		Attachments: []jsonExportAttach{},
		Warnings:    []string{},
	}
	switch {
	case r.page != nil:
		res.PageID = r.page.ID
		res.Title = r.page.Title
		res.Space = client.SpaceKeyFromWebUI(r.page.Links.WebUI)
		res.Parent = nullable(r.page.ParentID)
		res.ParentType = nullable(r.page.ParentType)
	case r.node != nil:
		// Never fetched -- its body failed, or an ancestor's did -- so what is
		// known about it is the walk's own row.
		res.PageID = r.node.ID
		res.Title = r.node.Title
		res.Space = r.node.Space
		res.Parent = nullable(r.node.ParentID)
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
