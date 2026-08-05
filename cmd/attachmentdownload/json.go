package attachmentdownload

// jsonDownloadResult is attachment-download's --json result shape: one object
// per attachment. dest_path is the local path written, which is the piece a
// caller cannot derive itself -- it depends on the recorded source path, --flat,
// and --dest. It is null only when resolving the path is what failed.
type jsonDownloadResult struct {
	OK       bool    `json:"ok"`
	Status   string  `json:"status"`
	DryRun   bool    `json:"dry_run"`
	Filename string  `json:"filename"`
	DestPath *string `json:"dest_path"`
	Error    *string `json:"error"`
	Code     *string `json:"code"`
}

func buildResult(r outcome) jsonDownloadResult {
	res := jsonDownloadResult{
		OK:       r.status != statusFailed,
		Status:   r.status,
		DryRun:   dryRun,
		Filename: r.name,
		DestPath: nullable(r.destPath),
	}
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
