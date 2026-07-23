package update

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	results := []*updateResult{
		{
			file: "docs/foo.md", ok: true, status: statusPublished,
			pageID: "123", title: "Foo", space: "ENG", url: "https://x/123",
			versionPrev: 3, versionNew: 4,
			width:       &jsonout.PageWidth{Value: "max", Default: false},
			attachments: []jsonout.Attachment{{Action: "updated", Filename: "d.png"}},
		},
		(&updateResult{file: "bad.md"}).fail(errTest("boom"), jsonout.CodeValidation),
	}
	items := make([]any, len(results))
	for i, r := range results {
		items[i] = r.jsonResult()
	}
	env := jsonout.NewEnvelope("update", items, summarize(results))
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestJSONResultPublished(t *testing.T) {
	r := &updateResult{
		file: "docs/foo.md", ok: true, status: statusPublished,
		pageID: "123", title: "Foo", space: "ENG",
		url:         "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
		versionPrev: 3, versionNew: 4,
		width:       &jsonout.PageWidth{Value: "max", Default: false},
		attachments: []jsonout.Attachment{{Action: "updated", Filename: "d.png"}},
	}
	got, err := json.MarshalIndent(r.jsonResult(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "status": "published",
  "file": "docs/foo.md",
  "page_id": "123",
  "title": "Foo",
  "space": "ENG",
  "url": "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
  "version": {
    "previous": 3,
    "new": 4
  },
  "page_width": {
    "value": "max",
    "default": false
  },
  "attachments": [
    {
      "action": "updated",
      "filename": "d.png"
    }
  ],
  "warnings": [],
  "broken": [],
  "error": null,
  "code": null
}`
	if string(got) != want {
		t.Errorf("published result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestJSONResultSkipped(t *testing.T) {
	r := &updateResult{
		file: "f.md", ok: true, status: statusSkipped,
		pageID: "1", title: "T", space: "ENG", url: "u",
		versionPrev: 5, versionNew: 5,
	}
	res := r.jsonResult()
	if res.Status != "skipped" || res.Version == nil ||
		res.Version.Previous != 5 || res.Version.New != 5 {
		t.Errorf("skipped result unexpected: %+v", res)
	}
}

func TestJSONResultFailedEarly(t *testing.T) {
	r := (&updateResult{file: "f.md"}).fail(errTest("no page id"), jsonout.CodeValidation)
	res := r.jsonResult()
	if res.OK || res.Status != "failed" {
		t.Errorf("want failed result, got %+v", res)
	}
	if res.Version != nil {
		t.Errorf("Version = %+v, want nil for an early failure", res.Version)
	}
	if res.PageID != nil {
		t.Errorf("PageID = %v, want nil (never resolved)", *res.PageID)
	}
	if res.Error == nil || *res.Error != "no page id" || res.Code == nil || *res.Code != jsonout.CodeValidation {
		t.Errorf("error/code not set: %+v", res)
	}
	// Empty slices, not null.
	if res.Attachments == nil || res.Warnings == nil || res.Broken == nil {
		t.Errorf("array fields must marshal as [], got %+v", res)
	}
}

func TestSummarize(t *testing.T) {
	results := []*updateResult{
		{ok: true, status: statusPublished},
		{ok: true, status: statusSkipped},
		{ok: false, status: statusFailed},
	}
	s := summarize(results)
	if s["total"] != 3 || s["succeeded"] != 2 || s["failed"] != 1 || s["skipped"] != 1 {
		t.Errorf("summary = %+v", s)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }
