package create

import (
	"fmt"
	"os"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
)

// Per-file status verbs for create. not_created marks a file that passed nothing
// because the batch aborted (or its parent was never created).
const (
	statusCreated    = "created"
	statusNotCreated = "not_created"
	statusFailed     = "failed"
)

// createResult captures the outcome of creating one page.
type createResult struct {
	file        string
	ok          bool
	status      string
	dryRun      bool
	pageID      string
	title       string
	space       string
	parent      *string
	parentFile  *string
	url         string
	width       *jsonout.PageWidth
	widthSet    bool
	persisted   bool
	attachments []jsonout.Attachment
	broken      []string
	warnings    []string
	errMsg      string
	code        jsonout.Code
}

// newResult seeds a result with the fields known before creation is attempted.
func newResult(r record) *createResult {
	return &createResult{
		file: r.filename, title: r.title, space: r.spaceKey,
		dryRun: dryRunOpt, parentFile: nullableStr(r.parent.display),
	}
}

func (r *createResult) fail(err error, code jsonout.Code) *createResult {
	r.ok = false
	r.status = statusFailed
	r.errMsg = err.Error()
	r.code = code
	return r
}

// renderHuman reproduces the original phase-2 inline output for one file.
func (r *createResult) renderHuman() {
	prefix := "[" + r.file + "]"
	if !r.ok {
		ui.Error(prefix + " " + r.errMsg)
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
	if r.widthSet && r.width != nil {
		ui.Info(prefix + " page width: " + r.width.Value)
	}
	// A dry-run has created no page, so there is no id or URL to print; name the
	// title and space instead. Every other line above is identical to a real run.
	if r.dryRun {
		ui.Success(fmt.Sprintf("%s Would create page '%s' in %s", prefix, r.title, r.space))
		return
	}
	ui.Success(fmt.Sprintf("%s Created page %s: %s", prefix, r.pageID, r.url))
}

// jsonCreateResult is create's --json result shape.
type jsonCreateResult struct {
	OK          bool                 `json:"ok"`
	Status      string               `json:"status"`
	DryRun      bool                 `json:"dry_run"`
	File        string               `json:"file"`
	PageID      *string              `json:"page_id"`
	Title       *string              `json:"title"`
	Space       *string              `json:"space"`
	Parent      *string              `json:"parent"`
	ParentFile  *string              `json:"parent_file"`
	URL         *string              `json:"url"`
	PageWidth   *jsonout.PageWidth   `json:"page_width"`
	Persisted   bool                 `json:"persisted"`
	Attachments []jsonout.Attachment `json:"attachments"`
	Warnings    []string             `json:"warnings"`
	Broken      []string             `json:"broken"`
	Error       *string              `json:"error"`
	Code        *jsonout.Code        `json:"code"`
}

func (r *createResult) jsonResult() jsonCreateResult {
	res := jsonCreateResult{
		OK:          r.ok,
		Status:      r.status,
		DryRun:      r.dryRun,
		File:        r.file,
		PageID:      nullableStr(r.pageID),
		Title:       nullableStr(r.title),
		Space:       nullableStr(r.space),
		Parent:      r.parent,
		ParentFile:  r.parentFile,
		URL:         nullableStr(r.url),
		PageWidth:   r.width,
		Persisted:   r.persisted,
		Attachments: nonNilAttachments(r.attachments),
		Warnings:    nonNilStrings(r.warnings),
		Broken:      nonNilStrings(r.broken),
	}
	if !r.ok {
		res.Error = &r.errMsg
		c := r.code
		res.Code = &c
	}
	return res
}

// createSummary is create's batch summary; aborted is true when phase-1
// validation failed and nothing was created.
type createSummary struct {
	Total     int  `json:"total"`
	Succeeded int  `json:"succeeded"`
	Failed    int  `json:"failed"`
	Aborted   bool `json:"aborted"`
}

func summarize(results []*createResult) createSummary {
	s := createSummary{Total: len(results)}
	for _, r := range results {
		if r.ok {
			s.Succeeded++
		} else {
			s.Failed++
		}
	}
	return s
}

// abort reports a phase-1 validation abort. In human mode it prints each error
// and the abort line; in JSON mode it emits an envelope with every input file
// present (validation-failed ones "failed", the rest "not_created") and an
// aborted summary. Either way it exits 1.
func abort(args []string, errs []failure) error {
	if !ui.IsJSON() {
		for _, e := range errs {
			ui.Error(fmt.Sprintf("[%s] %s", e.filename, e.message))
		}
		ui.Error(fmt.Sprintf("Aborting: %d file(s) failed validation; nothing was created.", len(errs)))
		return ui.ErrSilent
	}

	argSet := map[string]bool{}
	for _, a := range args {
		argSet[a] = true
	}
	errMap := map[string]failure{}
	var extra []failure // failures not tied to an input file, e.g. "(hierarchy)"
	for _, e := range errs {
		if argSet[e.filename] {
			errMap[e.filename] = e
		} else {
			extra = append(extra, e)
		}
	}

	var items []any
	failed := 0
	for _, a := range args {
		if f, bad := errMap[a]; bad {
			items = append(items, abortedResult(a, statusFailed, f, jsonout.CodeValidation))
			failed++
		} else {
			items = append(items, abortedResult(a, statusNotCreated, failure{}, ""))
		}
	}
	for _, e := range extra {
		items = append(items, abortedResult(e.filename, statusFailed, e, jsonout.CodeValidation))
		failed++
	}

	env := jsonout.NewEnvelope("create", items,
		createSummary{Total: len(args), Succeeded: 0, Failed: failed, Aborted: true})
	if err := jsonout.Emit(os.Stdout, env); err != nil {
		return err
	}
	return ui.SilentExit(1)
}

// abortedResult builds a minimal result for a file when the batch aborted before
// creation. Fields that require a live/created page stay null; arrays are [].
//
// f is this file's failure, or the zero value for a file that was never reached
// (status not_created). A page_id failure fills page_id -- the id in the file, the
// thing to go fix -- and, when a page is really at that id, url.
func abortedResult(file, status string, f failure, code jsonout.Code) jsonCreateResult {
	res := jsonCreateResult{
		OK:          false,
		Status:      status,
		DryRun:      dryRunOpt,
		File:        file,
		PageID:      nullableStr(f.pageID),
		URL:         nullableStr(f.url),
		Attachments: []jsonout.Attachment{},
		Warnings:    []string{},
		Broken:      []string{},
	}
	if f.message != "" {
		msg := f.message
		res.Error = &msg
		c := code
		res.Code = &c
	}
	return res
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

func nonNilAttachments(a []jsonout.Attachment) []jsonout.Attachment {
	if a == nil {
		return []jsonout.Attachment{}
	}
	return a
}

// fatalFail reports a config/usage/pre-flight failure, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "create", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}
