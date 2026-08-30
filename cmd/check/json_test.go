package check

import (
	"bytes"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	results := []*checkResult{
		{file: "clean.md", ok: true, status: statusClean},
		{file: "warn.md", ok: true, status: statusWarnings, warnings: []string{"link not resolved: draft.md"}},
		{
			file: "broken.md", ok: false, status: statusBroken,
			broken: []string{"line 3: IMAGE BROKEN: x.png (not found)"},
		},
		{
			file: "debug.md", ok: true, status: statusClean, hasDebug: true,
			debugHTML:        "<p>hi</p>\n",
			debugAttachments: []convert.Attachment{{Filename: "a.png", Path: "/tmp/a.png", Source: "a.png"}},
		},
		(&checkResult{file: "bad.md"}).fail(errString("invalid page_width"), jsonout.CodeValidation),
	}
	items := make([]any, len(results))
	for i, r := range results {
		items[i] = r.jsonResult()
	}
	env := jsonout.NewEnvelope("check", items, summarize(results))
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestJSONResultBrokenHasNoErrorOrCode is the regression test for the bug
// this caught during development: a broken result is ok:false too, but
// unlike failed, it has no separate operational error to report -- error and
// code must stay null.
func TestJSONResultBrokenHasNoErrorOrCode(t *testing.T) {
	r := &checkResult{file: "b.md", ok: false, status: statusBroken, broken: []string{"IMAGE BROKEN: x.png (not found)"}}
	res := r.jsonResult()
	if res.Error != nil || res.Code != nil {
		t.Errorf("broken result error/code = %v/%v, want nil/nil", res.Error, res.Code)
	}
}

func TestJSONResultFailedHasErrorAndCode(t *testing.T) {
	r := (&checkResult{file: "b.md"}).fail(errString("invalid page_width"), jsonout.CodeValidation)
	res := r.jsonResult()
	if res.Error == nil || *res.Error != "invalid page_width" {
		t.Errorf("error = %v, want \"invalid page_width\"", res.Error)
	}
	if res.Code == nil || *res.Code != jsonout.CodeValidation {
		t.Errorf("code = %v, want VALIDATION", res.Code)
	}
}

func TestJSONResultDebugNullWithoutShowHTML(t *testing.T) {
	r := &checkResult{file: "c.md", ok: true, status: statusClean}
	if got := r.jsonResult().Debug; got != nil {
		t.Errorf("debug = %+v, want nil when --show-html wasn't requested", got)
	}
}

func TestSummarize(t *testing.T) {
	s := summarize([]*checkResult{
		{ok: true, status: statusClean},
		{ok: true, status: statusWarnings},
		{ok: false, status: statusBroken},
		{ok: false, status: statusFailed},
	})
	if s["total"] != 4 || s["succeeded"] != 2 || s["failed"] != 2 || s["clean"] != 1 || s["warnings"] != 1 {
		t.Errorf("summary = %+v", s)
	}
}

func TestIndentHTMLNestedTable(t *testing.T) {
	html := "<table>\n<thead>\n<tr>\n<th>A</th>\n</tr>\n</thead>\n</table>\n"
	want := "<table>\n  <thead>\n    <tr>\n      <th>A</th>\n    </tr>\n  </thead>\n</table>"
	if got := indentHTML(html); got != want {
		t.Errorf("indentHTML =\n%q\nwant\n%q", got, want)
	}
}

func TestIndentHTMLSelfContainedLineStaysFlat(t *testing.T) {
	// A line that opens and closes everything itself (a leaf <p>, or an
	// <ac:image>/<ri:attachment> pair) must not affect the depth of anything
	// after it.
	html := "<p>one</p>\n<p><ac:image><ri:attachment ri:filename=\"x\" /></ac:image></p>\n<p>two</p>\n"
	want := "<p>one</p>\n<p><ac:image><ri:attachment ri:filename=\"x\" /></ac:image></p>\n<p>two</p>"
	if got := indentHTML(html); got != want {
		t.Errorf("indentHTML =\n%q\nwant\n%q", got, want)
	}
}

func TestIndentHTMLCommentDoesNotAffectDepth(t *testing.T) {
	html := "<td>trailing <!-- bg:green --></td>\n<p>after</p>\n"
	want := "<td>trailing <!-- bg:green --></td>\n<p>after</p>"
	if got := indentHTML(html); got != want {
		t.Errorf("indentHTML =\n%q\nwant\n%q", got, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
