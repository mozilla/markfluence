package attachmentupload

import "github.com/mozilla/markfluence/internal/client"

// jsonUploadResult is attachment-upload's --json result shape: one object per
// file. status uses the same created/updated/skipped verbs the attachments
// array on update and create already reports, so a script that understands one
// understands the other.
type jsonUploadResult struct {
	OK       bool    `json:"ok"`
	Status   string  `json:"status"`
	DryRun   bool    `json:"dry_run"`
	Filename string  `json:"filename"`
	Error    *string `json:"error"`
	Code     *string `json:"code"`
}

func buildResult(a client.SyncAction) jsonUploadResult {
	return jsonUploadResult{
		OK:       true,
		Status:   a.Action,
		DryRun:   dryRun,
		Filename: a.Filename,
	}
}
