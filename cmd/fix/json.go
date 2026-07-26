package fix

import (
	"errors"
	"fmt"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
)

// Per-file status verbs for fix.
const (
	statusChanged    = "changed"
	statusConsistent = "consistent"
	statusFailed     = "failed"
)

// noneDisplay is the sentinel plannedChanges uses for a field with no prior
// value; it maps to a null "old" in JSON.
const noneDisplay = "(none)"

// fixResult captures the outcome of reconciling one file.
type fixResult struct {
	file     string
	ok       bool
	status   string
	pageID   string
	dryRun   bool
	changes  []change
	warnings []string
	errMsg   string
	code     jsonout.Code
}

func (r *fixResult) fail(err error, code jsonout.Code) *fixResult {
	r.ok = false
	r.status = statusFailed
	r.errMsg = err.Error()
	r.code = code
	return r
}

// renderHuman reproduces the command's original inline output for one file.
func (r *fixResult) renderHuman() {
	prefix := "[" + r.file + "]"
	if !r.ok {
		ui.Error(prefix + " " + r.errMsg)
		return
	}
	for _, w := range r.warnings {
		ui.Warn(prefix + " " + w)
	}
	if r.status == statusConsistent {
		ui.Info(prefix + " already consistent")
		return
	}
	// The per-field lines are identical in a dry-run; the leading DRY RUN banner
	// (and dry_run in --json) is the only signal nothing was written.
	for _, ch := range r.changes {
		ui.Info(fmt.Sprintf("%s set %s: %s -> %s", prefix, ch.field, ch.oldDisplay, ch.newValue))
	}
}

// jsonFixResult is fix's --json result shape.
type jsonFixResult struct {
	OK       bool          `json:"ok"`
	Status   string        `json:"status"`
	File     string        `json:"file"`
	PageID   *string       `json:"page_id"`
	DryRun   bool          `json:"dry_run"`
	Changes  []jsonChange  `json:"changes"`
	Warnings []string      `json:"warnings"`
	Error    *string       `json:"error"`
	Code     *jsonout.Code `json:"code"`
}

// jsonChange is one reconciled field. old is null when there was no prior value.
type jsonChange struct {
	Field string  `json:"field"`
	Old   *string `json:"old"`
	New   string  `json:"new"`
}

func (r *fixResult) jsonResult() jsonFixResult {
	res := jsonFixResult{
		OK:       r.ok,
		Status:   r.status,
		File:     r.file,
		PageID:   nullableStr(r.pageID),
		DryRun:   r.dryRun,
		Changes:  toJSONChanges(r.changes),
		Warnings: nonNilStrings(r.warnings),
	}
	if !r.ok {
		res.Error = &r.errMsg
		c := r.code
		res.Code = &c
	}
	return res
}

func toJSONChanges(changes []change) []jsonChange {
	out := make([]jsonChange, 0, len(changes))
	for _, ch := range changes {
		jc := jsonChange{Field: ch.field, New: ch.newValue}
		if ch.oldDisplay != noneDisplay {
			old := ch.oldDisplay
			jc.Old = &old
		}
		out = append(out, jc)
	}
	return out
}

// summarize builds fix's batch summary.
func summarize(results []*fixResult) map[string]int {
	s := map[string]int{"total": len(results), "succeeded": 0, "failed": 0, "changed": 0, "consistent": 0}
	for _, r := range results {
		switch {
		case !r.ok:
			s["failed"]++
		case r.status == statusConsistent:
			s["succeeded"]++
			s["consistent"]++
		default:
			s["succeeded"]++
			s["changed"]++
		}
	}
	return s
}

// locateCode classifies a page-location failure: an HTTP status maps via CodeFor,
// anything else is a frontmatter/target problem (VALIDATION).
func locateCode(err error) jsonout.Code {
	var he *client.HTTPError
	if errors.As(err, &he) {
		return jsonout.CodeFor(err)
	}
	return jsonout.CodeValidation
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
