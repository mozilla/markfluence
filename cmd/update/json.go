package update

import (
	"fmt"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/labels"
	"github.com/mozilla/markfluence/internal/project"
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
	// labels is nil when the file declares no labels key, which is the
	// precedent page_width already sets for "not asserted this run" -- and it
	// is load-bearing rather than cosmetic here, since an empty set is a
	// meaningful declaration that means "remove them all".
	labels []jsonout.Label
	// pageStatus is nil when the file declares no page_status key, or when
	// asserting it failed -- the labels/page_width convention for "not
	// asserted this run". Named apart from status above, which is this
	// result's own verb (published/skipped/failed); the collision is the
	// reason info's Confluence lifecycle status is reported as
	// content_status.
	pageStatus  *jsonout.PageStatus
	attachments []jsonout.Attachment
	broken      []string
	warnings    []string
	errMsg      string
	code        jsonout.Code
	// metadataSource is which location supplied this file's page metadata
	// ("frontmatter", "manifest", or empty when nothing claims the file).
	// Debugging "why did it publish to *that* page" in a CI log otherwise means
	// reproducing the resolution by hand.
	metadataSource string
	// unmanaged distinguishes the two reasons a file is skipped: nothing
	// claims it, or it is unchanged since the page's last version. Human
	// output says which; --json has status plus metadata_source.
	unmanaged bool

	// The three below feed the action log (#149) and are deliberately absent
	// from --json: they are markfluence's own bookkeeping about the run, not
	// a report about the page.
	//
	// root is the project root this file resolved under, which decides which
	// log the line goes to -- a batch may span several. logKey is the file's
	// root-relative manifest key, empty when it has none. publishSHA is a
	// hash of what the body PUT sent, empty when the render never happened.
	root       *project.Root
	logKey     string
	publishSHA string
	// logVersion is the version the *page* was left at, which is versionNew
	// except when a page status was written: that is the one metadata pass
	// that bumps the page version, and recording the body PUT's version would
	// leave the base one behind and make the next run report a divergence.
	// Zero means "use versionNew".
	logVersion int

	// base is the merge base found for this file, nil when none was usable.
	// Reported as --json's "base" -- a fact about the log rather than about
	// the check, so it is set even under --force, where the checks do not run.
	base *actionlog.Entry
	// bodyChanged is what the idempotence check concluded, nil when it could
	// not run: no base, no sha in the base, or --force. A tri-state rather
	// than a bool because "the check did not run" is the thing a CI consumer
	// has to be able to see, the same reason metadata_source is nullable.
	bodyChanged *bool
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
	// Broken links and warnings print *above* the skip branch, not below it.
	// That ordering was safe while the skip was the mtime check, which
	// returned before the converter ever ran -- so a skipped result carried
	// nothing to report. The unchanged-body skip runs after the render, so by
	// now the result may be carrying a LINK BROKEN message, a mention naming
	// nobody, a metadata disagreement, a label-case repair, or a failure to
	// record the run. Reporting those only when something was published would
	// hide a dead link forever on exactly the file that has stopped changing,
	// and would leave human output disagreeing with --json, which reports them
	// either way.
	for _, b := range r.broken {
		ui.Warn(prefix + " " + b)
	}
	for _, w := range r.warnings {
		ui.Warn(prefix + " " + w)
	}
	if r.status == statusSkipped {
		if r.unmanaged {
			ui.Info(prefix + " Skipping -- not published by markfluence")
			return
		}
		ui.Info(prefix + " Skipping -- no changes")
		return
	}
	for _, a := range r.attachments {
		ui.Info(fmt.Sprintf("%s attachment %s: %s", prefix, a.Action, a.Filename))
	}
	// The version pair only when the body actually moved. An attachment-only
	// run -- a redrawn diagram, published with no prose change -- must not
	// claim a bump that did not happen, since an attachment upload leaves the
	// page version alone; and it must not print nothing either, which would
	// read as a no-op when the diagram really was replaced.
	bodyMoved := r.versionNew != r.versionPrev
	if bodyMoved {
		ui.Info(fmt.Sprintf("%s Updating '%s' (v%d -> v%d)...", prefix, r.title, r.versionPrev, r.versionNew))
	}
	if r.widthSet && r.width != nil {
		ui.Info(prefix + " page width: " + r.width.Value)
	}
	for _, l := range r.labels {
		if l.Action != labels.ActionUnchanged {
			ui.Info(fmt.Sprintf("%s label %s: %s", prefix, l.Action, l.Name))
		}
	}
	if r.pageStatus != nil && r.pageStatus.Action != "unchanged" {
		ui.Info(prefix + " page status: " + r.pageStatus.Name)
	}
	if !bodyMoved {
		ui.Success(fmt.Sprintf("%s Body unchanged at v%d: %s", prefix, r.versionNew, r.url))
		return
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
	Labels      *[]jsonout.Label     `json:"labels"`
	PageStatus  *jsonout.PageStatus  `json:"page_status"`
	Attachments []jsonout.Attachment `json:"attachments"`
	Warnings    []string             `json:"warnings"`
	Broken      []string             `json:"broken"`
	// MetadataSource is null for a file nothing claims, which is why it is a
	// pointer rather than an empty string: "" would read as a source named "".
	MetadataSource *string `json:"metadata_source"`
	// Base is the merge base this run compared against, null when none was
	// usable -- no log, no line for this file, or a line naming another page.
	// That null is the "the check could not run" signal a CI consumer needs
	// and a human does not, the same split metadata_source makes.
	Base *jsonUpdateBase `json:"base"`
	// BodyChanged is what the idempotence check concluded, null when it did
	// not run (no base, a base with no sha, or --force).
	BodyChanged *bool         `json:"body_changed"`
	Error       *string       `json:"error"`
	Code        *jsonout.Code `json:"code"`
}

// jsonUpdateBase is the recorded base, reported so a consumer can see what the
// decision rested on.
type jsonUpdateBase struct {
	PageVersion   int    `json:"page_version"`
	PublishSHA256 string `json:"publish_sha256"`
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
		Labels:      labelsOrNil(r.labels),
		PageStatus:  r.pageStatus,
		Attachments: nonNilAttachments(r.attachments),
		Warnings:    nonNilStrings(r.warnings),
		Broken:      nonNilStrings(r.broken),

		MetadataSource: strOrNil(r.metadataSource),
		BodyChanged:    r.bodyChanged,
	}
	if r.base != nil {
		res.Base = &jsonUpdateBase{
			PageVersion:   r.base.PageVersion,
			PublishSHA256: r.base.PublishSHA256,
		}
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

// labelsOrNil renders the labels field: an array when the file declared the
// key, null when it did not. A pointer to a slice rather than a slice, because
// an empty declared set and an absent key are different answers -- "[]" means
// the page's labels were removed, null means they were never touched -- and a
// nil slice cannot say which.
func labelsOrNil(l []jsonout.Label) *[]jsonout.Label {
	if l == nil {
		return nil
	}
	return &l
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
