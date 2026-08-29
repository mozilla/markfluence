package attachmentupload

import (
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
)

// buildResult builds attachment-upload's --json result for one file. status uses
// the same created/updated/skipped verbs the attachments array on update and
// create already reports, so a script that understands one understands the
// other. dest_path is always null: upload has no local destination to report.
func buildResult(a client.SyncAction) jsonout.AttachmentActionResult {
	return jsonout.AttachmentActionResult{
		OK:       true,
		Status:   a.Action,
		DryRun:   dryRun,
		Filename: a.Filename,
	}
}
