package create

import (
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
)

func TestJSONResultCreated(t *testing.T) {
	parent := "123"
	r := &createResult{
		file: "child.md", ok: true, status: statusCreated,
		pageID: "456", title: "Child", space: "ENG", parent: &parent,
		url:       "https://wiki.example.net/wiki/spaces/ENG/pages/456/Child",
		width:     &jsonout.PageWidth{Value: "max", Default: false},
		persisted: true,
	}
	got, err := json.MarshalIndent(r.jsonResult(), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "status": "created",
  "file": "child.md",
  "page_id": "456",
  "title": "Child",
  "space": "ENG",
  "parent": "123",
  "url": "https://wiki.example.net/wiki/spaces/ENG/pages/456/Child",
  "page_width": {
    "value": "max",
    "default": false
  },
  "persisted": true,
  "attachments": [],
  "warnings": [],
  "broken": [],
  "error": null,
  "code": null
}`
	if string(got) != want {
		t.Errorf("created result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestAbortedResultShapes(t *testing.T) {
	// A validation-failed file.
	failed := abortedResult("bad.md", statusFailed, "no title given", jsonout.CodeValidation)
	if failed.OK || failed.Status != "failed" || failed.Error == nil ||
		failed.Code == nil || *failed.Code != jsonout.CodeValidation {
		t.Errorf("failed abort result unexpected: %+v", failed)
	}
	// A file that simply wasn't created (batch aborted).
	nc := abortedResult("ok.md", statusNotCreated, "", "")
	if nc.OK || nc.Status != "not_created" || nc.Error != nil || nc.Code != nil {
		t.Errorf("not_created abort result unexpected: %+v", nc)
	}
	// Arrays must be [] not null.
	if nc.Attachments == nil || nc.Warnings == nil || nc.Broken == nil {
		t.Errorf("abort arrays must be [], got %+v", nc)
	}
}

func TestSummarize(t *testing.T) {
	s := summarize([]*createResult{
		{ok: true, status: statusCreated},
		{ok: false, status: statusFailed},
	})
	if s.Total != 2 || s.Succeeded != 1 || s.Failed != 1 || s.Aborted {
		t.Errorf("summary = %+v", s)
	}
}

func TestCreateSummaryAbortedJSON(t *testing.T) {
	b, err := json.Marshal(createSummary{Total: 3, Succeeded: 0, Failed: 1, Aborted: true})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"total":3,"succeeded":0,"failed":1,"aborted":true}`
	if string(b) != want {
		t.Errorf("summary JSON = %s, want %s", b, want)
	}
}
