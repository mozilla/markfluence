package check

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
)

// Per-file status verbs for check.
const (
	statusClean    = "clean"
	statusWarnings = "warnings"
	statusBroken   = "broken"
	statusFailed   = "failed"
)

// checkResult captures the outcome of validating one file.
type checkResult struct {
	file     string
	status   string
	broken   []string
	warnings []string
	// hasDebug is true only when --show-html was passed and the file reached
	// the converter -- never on a failed file, which has no HTML to show.
	hasDebug         bool
	debugHTML        string
	debugAttachments []convert.Attachment
	errMsg           string
	code             jsonout.Code
}

// fail marks the result failed with an error and code, and returns it for a
// tidy `return r.fail(...)`.
func (r *checkResult) fail(err error, code jsonout.Code) *checkResult {
	r.status = statusFailed
	r.errMsg = err.Error()
	r.code = code
	return r
}

// ok reports whether the result counts as a success. Computed from status
// rather than stored alongside it, so there is exactly one thing four
// different call sites (fail and the three switch branches in processFile)
// have to get right, not two that could silently disagree -- a broken result
// is ok:false too, but status is what a caller actually branches on.
func (r *checkResult) ok() bool {
	return r.status != statusBroken && r.status != statusFailed
}

// renderHuman prints one file's diagnostics: a [file]-prefixed line per
// broken item (ui.Error) and warning (ui.Warn), a plain "clean" line when
// there's nothing else to report, and -- with --show-html -- the storage
// HTML (indented by nesting depth) and attachment list.
func (r *checkResult) renderHuman() {
	prefix := "[" + r.file + "]"
	if r.status == statusFailed {
		ui.Error(prefix + " " + r.errMsg)
		return
	}
	for _, b := range r.broken {
		ui.Error(prefix + " " + b)
	}
	for _, w := range r.warnings {
		ui.Warn(prefix + " " + w)
	}
	if r.status == statusClean {
		ui.Info(prefix + " clean")
	}
	if r.hasDebug {
		ui.Info(prefix + " --- storage HTML ---")
		fmt.Println(indentHTML(r.debugHTML))
		if len(r.debugAttachments) > 0 {
			ui.Info(prefix + " --- attachments ---")
			for _, a := range r.debugAttachments {
				fmt.Printf("%s -> %s\n", a.Filename, a.Source)
			}
		}
	}
}

// htmlTagRE matches one HTML/XML tag, used only to track nesting depth --
// never to reformat inside a line. Storage HTML is XHTML with every
// attribute value already entity-escaped (html.EscapeString escapes ">"),
// so an unescaped ">" inside a quoted attribute value never reaches here.
var htmlTagRE = regexp.MustCompile(`<[^>]+>`)

// indentHTML indents storage HTML by nesting depth for human-readable
// display. The renderer already emits one tag per line at every structural
// boundary (confirmed against the regression goldens); this only adds
// leading whitespace per line based on the net depth change its tags cause,
// never reformatting within a line -- so it cannot alter meaningful inline
// text mixed into a block the way a whitespace-normalizing pretty-printer
// could.
func indentHTML(html string) string {
	lines := strings.Split(strings.TrimRight(html, "\n"), "\n")
	var b strings.Builder
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		d := depth
		if strings.HasPrefix(trimmed, "</") && d > 0 {
			d--
		}
		b.WriteString(strings.Repeat("  ", d))
		b.WriteString(trimmed)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
		depth += tagDepthDelta(trimmed)
		if depth < 0 {
			depth = 0
		}
	}
	return b.String()
}

// tagDepthDelta returns how a line's tags change nesting depth: +1 per
// opening tag, -1 per closing tag, 0 for a self-closing tag (XHTML's own
// "<tag />") or an HTML comment, net over every tag found on the line.
func tagDepthDelta(line string) int {
	delta := 0
	for _, tag := range htmlTagRE.FindAllString(line, -1) {
		switch {
		case strings.HasPrefix(tag, "<!--"):
		case strings.HasPrefix(tag, "</"):
			delta--
		case strings.HasSuffix(tag, "/>"):
		default:
			delta++
		}
	}
	return delta
}

// jsonCheckResult is check's --json result shape.
type jsonCheckResult struct {
	OK       bool            `json:"ok"`
	Status   string          `json:"status"`
	File     string          `json:"file"`
	Broken   []string        `json:"broken"`
	Warnings []string        `json:"warnings"`
	Debug    *jsonCheckDebug `json:"debug"`
	Error    *string         `json:"error"`
	Code     *jsonout.Code   `json:"code"`
}

// jsonCheckDebug is checkResult's --show-html payload: the storage HTML
// exactly as produced (unindented -- indentation is a human-output display
// concern, not a data one), plus ConfluencePage.Attachments verbatim.
type jsonCheckDebug struct {
	HTML        string               `json:"html"`
	Attachments []convert.Attachment `json:"attachments"`
}

func (r *checkResult) jsonResult() jsonCheckResult {
	res := jsonCheckResult{
		OK:       r.ok(),
		Status:   r.status,
		File:     r.file,
		Broken:   nonNilStrings(r.broken),
		Warnings: nonNilStrings(r.warnings),
	}
	if r.hasDebug {
		res.Debug = &jsonCheckDebug{HTML: r.debugHTML, Attachments: nonNilAttachments(r.debugAttachments)}
	}
	// error/code are only ever set on a failed result: a broken result is
	// ok:false too, but its broken/warnings fields already say everything --
	// there is no separate operational error to report.
	if r.status == statusFailed {
		res.Error = &r.errMsg
		c := r.code
		res.Code = &c
	}
	return res
}

// summarize builds check's batch summary.
func summarize(results []*checkResult) map[string]int {
	s := map[string]int{"total": len(results), "succeeded": 0, "failed": 0, "clean": 0, "warnings": 0}
	for _, r := range results {
		switch {
		case !r.ok():
			s["failed"]++
		case r.status == statusWarnings:
			s["succeeded"]++
			s["warnings"]++
		default:
			s["succeeded"]++
			s["clean"]++
		}
	}
	return s
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func nonNilAttachments(a []convert.Attachment) []convert.Attachment {
	if a == nil {
		return []convert.Attachment{}
	}
	return a
}
