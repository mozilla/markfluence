package userfind

import (
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
)

// jsonUserResult is user-find's --json result shape: one object per match, so
// `.results[0].mention` works directly and summary.total is the match count.
//
// mention is carried rather than left for the consumer to assemble, for the
// same reason it is the point of the human output: assembling it means knowing
// that the host is Atlassian Home and that the "@" is what makes it a mention.
// It also comes from the converter's own builder, so what a script pastes is
// byte-identical to what read/export would write.
//
// There is deliberately no type field, though the row carries one. Every
// account answers "known" -- measured including an automation account and a
// page-template account -- so the field discriminates nothing, and a field
// named type whose only value is known invites a consumer to filter people out
// with it and get nothing for the effort.
//
// No separate url either. The profile URL is inside mention already, and a
// second copy differing only by the "@" is a thing to get wrong.
type jsonUserResult struct {
	OK          bool   `json:"ok"`
	AccountID   string `json:"account_id"`
	DisplayName string `json:"display_name"`
	Mention     string `json:"mention"`
}

// jsonUserSummary is user-find's summary.
//
// basicSummary cannot be reused: it is additionalProperties:false, and
// truncated is load-bearing. It is a flag rather than a count for a sharper
// reason than search's drift -- this route's totalSize reports the rows on the
// current page, so it cannot describe the result set at all.
type jsonUserSummary struct {
	Total     int  `json:"total"`
	Succeeded int  `json:"succeeded"`
	Failed    int  `json:"failed"`
	Truncated bool `json:"truncated"`
}

func buildResult(m client.UserMatch) jsonUserResult {
	return jsonUserResult{
		OK:          true,
		AccountID:   m.AccountID,
		DisplayName: m.DisplayName,
		Mention:     convert.MentionMarkdown(m.DisplayName, m.AccountID),
	}
}

// buildSummary is split out so the conformance test builds the summary this
// command really emits rather than a hand-copied literal.
//
// failed is always 0: there is no per-row failure variant, since a failed
// lookup is an error object with no envelope at all.
func buildSummary(matches []client.UserMatch, more bool) jsonUserSummary {
	return jsonUserSummary{
		Total:     len(matches),
		Succeeded: len(matches),
		Failed:    0,
		Truncated: more,
	}
}
