package attachmentdownload

import "github.com/mozilla/markfluence/internal/attachfile"

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

func buildResult(r attachfile.Outcome) jsonDownloadResult {
	res := jsonDownloadResult{
		OK:       r.Status != attachfile.StatusFailed,
		Status:   r.Status,
		DryRun:   dryRun,
		Filename: r.Name,
		DestPath: nullable(r.DestPath),
	}
	if r.Err != nil {
		msg := r.Err.Error()
		res.Error = &msg
	}
	if r.Code != "" {
		code := string(r.Code)
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
