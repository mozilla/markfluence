package update

import (
	"fmt"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
)

// Per-file status verbs for update.
const (
	statusPublished = "published"
	statusSkipped   = "skipped"
	statusFailed    = "failed"
)

// updateResult captures the outcome of publishing one file. It carries both what
// the human renderer needs (to reproduce the previous inline output) and the
// structured fields for JSON.
type updateResult struct {
	file        string
	ok          bool
	status      string
	dryRun      bool
	pageID      string
	title       string
	space       string
	url         string
	versionPrev int
	versionNew  int
	width       *jsonout.PageWidth // set only when a width was asserted this run
	widthSet    bool               // a "page width:" line should show (human)
	attachments []jsonout.Attachment
	broken      []string
	warnings    []string
	errMsg      string
	code        jsonout.Code
}

// fail marks the result failed with an error and code, and returns it for a
// tidy `return r.fail(...)`.
func (r *updateResult) fail(err error, code jsonout.Code) *updateResult {
	r.ok = false
	r.status = statusFailed
	r.errMsg = err.Error()
	r.code = code
	return r
}

// renderHuman reproduces the command's original inline output for one file, in
// the original order (broken/warnings, attachments, the update line, an optional
// width line, then the success/skip/error line).
func (r *updateResult) renderHuman() {
	prefix := "[" + r.file + "]"
	if !r.ok {
		ui.Error(prefix + " " + r.errMsg)
		return
	}
	if r.status == statusSkipped {
		ui.Info(prefix + " Skipping -- no changes")
		return
	}
	for _, b := range r.broken {
		ui.Warn(prefix + " " + b)
	}
	for _, w := range r.warnings {
		ui.Warn(prefix + " " + w)
	}
	for _, a := range r.attachments {
		ui.Info(fmt.Sprintf("%s attachment %s: %s", prefix, a.Action, a.Filename))
	}
	ui.Info(fmt.Sprintf("%s Updating '%s' (v%d -> v%d)...", prefix, r.title, r.versionPrev, r.versionNew))
	if r.widthSet && r.width != nil {
		ui.Info(prefix + " page width: " + r.width.Value)
	}
	ui.Success(fmt.Sprintf("%s Published v%d: %s", prefix, r.versionNew, r.url))
}

// jsonUpdateResult is update's --json result shape.
type jsonUpdateResult struct {
	OK          bool                 `json:"ok"`
	Status      string               `json:"status"`
	DryRun      bool                 `json:"dry_run"`
	File        string               `json:"file"`
	PageID      *string              `json:"page_id"`
	Title       *string              `json:"title"`
	Space       *string              `json:"space"`
	URL         *string              `json:"url"`
	Version     *jsonUpdateVersion   `json:"version"`
	PageWidth   *jsonout.PageWidth   `json:"page_width"`
	Attachments []jsonout.Attachment `json:"attachments"`
	Warnings    []string             `json:"warnings"`
	Broken      []string             `json:"broken"`
	Error       *string              `json:"error"`
	Code        *jsonout.Code        `json:"code"`
}

type jsonUpdateVersion struct {
	Previous int `json:"previous"`
	New      int `json:"new"`
}

func (r *updateResult) jsonResult() jsonUpdateResult {
	res := jsonUpdateResult{
		OK:          r.ok,
		Status:      r.status,
		DryRun:      r.dryRun,
		File:        r.file,
		PageID:      strOrNil(r.pageID),
		Title:       strOrNil(r.title),
		Space:       strOrNil(r.space),
		URL:         strOrNil(r.url),
		PageWidth:   r.width,
		Attachments: nonNilAttachments(r.attachments),
		Warnings:    nonNilStrings(r.warnings),
		Broken:      nonNilStrings(r.broken),
	}
	// version is present once we know the live version (all non-early failures).
	if r.versionPrev != 0 || r.versionNew != 0 {
		res.Version = &jsonUpdateVersion{Previous: r.versionPrev, New: r.versionNew}
	}
	if !r.ok {
		res.Error = &r.errMsg
		c := r.code
		res.Code = &c
	}
	return res
}

// summarize builds update's batch summary.
func summarize(results []*updateResult) map[string]int {
	s := map[string]int{"total": len(results), "succeeded": 0, "failed": 0, "skipped": 0}
	for _, r := range results {
		switch {
		case !r.ok:
			s["failed"]++
		case r.status == statusSkipped:
			s["succeeded"]++
			s["skipped"]++
		default:
			s["succeeded"]++
		}
	}
	return s
}

func strOrNil(s string) *string {
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

func nonNilAttachments(a []jsonout.Attachment) []jsonout.Attachment {
	if a == nil {
		return []jsonout.Attachment{}
	}
	return a
}
