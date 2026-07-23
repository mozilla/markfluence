package fix

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	results := []*fixResult{
		{
			file: "docs/foo.md", ok: true, status: statusChanged, pageID: "123",
			changes: []change{
				{field: "space", oldDisplay: "OLD", newValue: "ENG"},
				{field: "page_id", oldDisplay: noneDisplay, newValue: "123"},
			},
			warnings: []string{"could not read page width: boom"},
		},
		{file: "clean.md", ok: true, status: statusConsistent, pageID: "9"},
		(&fixResult{file: "bad.md"}).fail(errString("no page_id or title"), jsonout.CodeValidation),
	}
	items := make([]any, len(results))
	for i, r := range results {
		items[i] = r.jsonResult()
	}
	env := jsonout.NewEnvelope("fix", items, summarize(results))
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestJSONResultChanged(t *testing.T) {
	r := &fixResult{
		file: "docs/foo.md", ok: true, status: statusChanged, pageID: "123",
		dryRun: false,
		changes: []change{
			{field: "space", oldDisplay: "OLD", newValue: "ENG"},
			{field: "page_id", oldDisplay: noneDisplay, newValue: "123"},
		},
	}
	got, err := json.MarshalIndent(r.jsonResult(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "status": "changed",
  "file": "docs/foo.md",
  "page_id": "123",
  "dry_run": false,
  "changes": [
    {
      "field": "space",
      "old": "OLD",
      "new": "ENG"
    },
    {
      "field": "page_id",
      "old": null,
      "new": "123"
    }
  ],
  "warnings": [],
  "error": null,
  "code": null
}`
	if string(got) != want {
		t.Errorf("changed result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestJSONResultConsistent(t *testing.T) {
	r := &fixResult{file: "f.md", ok: true, status: statusConsistent, pageID: "1"}
	res := r.jsonResult()
	if res.Status != "consistent" || res.Changes == nil || len(res.Changes) != 0 {
		t.Errorf("consistent result unexpected: %+v", res)
	}
}

func TestJSONResultFailed(t *testing.T) {
	r := (&fixResult{file: "f.md"}).fail(errString("no page_id or title"), jsonout.CodeValidation)
	res := r.jsonResult()
	if res.OK || res.Status != "failed" || res.PageID != nil {
		t.Errorf("failed result unexpected: %+v", res)
	}
	if res.Error == nil || *res.Error != "no page_id or title" ||
		res.Code == nil || *res.Code != jsonout.CodeValidation {
		t.Errorf("error/code not set: %+v", res)
	}
}

func TestSummarize(t *testing.T) {
	s := summarize([]*fixResult{
		{ok: true, status: statusChanged},
		{ok: true, status: statusConsistent},
		{ok: false, status: statusFailed},
	})
	if s["total"] != 3 || s["succeeded"] != 2 || s["failed"] != 1 ||
		s["changed"] != 1 || s["consistent"] != 1 {
		t.Errorf("summary = %+v", s)
	}
}

func TestLocateCode(t *testing.T) {
	if got := locateCode(&client.HTTPError{StatusCode: 404}); got != jsonout.CodeNotFound {
		t.Errorf("locateCode(404) = %q, want NOT_FOUND", got)
	}
	if got := locateCode(errString("no page_id or title")); got != jsonout.CodeValidation {
		t.Errorf("locateCode(logic) = %q, want VALIDATION", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
