package create

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	// A normal (non-aborted) create batch.
	parent := "123"
	parentFile := "index.md"
	results := []*createResult{
		{
			file: "child.md", ok: true, status: statusCreated,
			pageID: "456", title: "Child", space: "ENG", parent: &parent, url: "https://x/456",
			width:     &jsonout.PageWidth{Value: "max", Default: false},
			persisted: true,
		},
		{
			// A dry-run of an in-set child: no page id/url yet, parent unresolved
			// (null), but the relationship is reported via parent_file.
			file: "grandchild.md", ok: true, status: statusCreated, dryRun: true,
			title: "Grandchild", space: "ENG", parentFile: &parentFile,
			width:     &jsonout.PageWidth{Value: "max", Default: false},
			persisted: true,
		},
		(&createResult{file: "bad.md"}).fail(errString("boom"), jsonout.CodeConvert),
	}
	items := make([]any, len(results))
	for i, r := range results {
		items[i] = r.jsonResult()
	}
	env := jsonout.NewEnvelope("create", items, summarize(results))
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// The phase-1 abort envelope.
	abortItems := []any{
		abortedResult("bad.md", statusFailed, "no title given", jsonout.CodeValidation),
		abortedResult("ok.md", statusNotCreated, "", ""),
	}
	abortEnv := jsonout.NewEnvelope("create", abortItems,
		createSummary{Total: 2, Succeeded: 0, Failed: 1, Aborted: true})
	buf.Reset()
	if err := jsonout.Emit(&buf, abortEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

type errString string

func (e errString) Error() string { return string(e) }

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
  "dry_run": false,
  "file": "child.md",
  "page_id": "456",
  "title": "Child",
  "space": "ENG",
  "parent": "123",
  "parent_file": null,
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

func TestJSONResultDryRunInSetParent(t *testing.T) {
	parentFile := "index.md"
	r := &createResult{
		file: "child.md", ok: true, status: statusCreated, dryRun: true,
		title: "Child", space: "ENG", parentFile: &parentFile,
		width:     &jsonout.PageWidth{Value: "max", Default: false},
		persisted: true,
	}
	j := r.jsonResult()
	if !j.DryRun {
		t.Errorf("dry_run = false, want true")
	}
	// No page was created, so id/url are null; parent is unresolved but the
	// source file is reported so the relationship is not lost.
	if j.PageID != nil || j.URL != nil {
		t.Errorf("page_id=%v url=%v, want both nil in a dry-run", j.PageID, j.URL)
	}
	if j.Parent != nil {
		t.Errorf("parent = %v, want nil (in-set parent has no id yet)", j.Parent)
	}
	if j.ParentFile == nil || *j.ParentFile != "index.md" {
		t.Errorf("parent_file = %v, want index.md", j.ParentFile)
	}
	// persisted reflects intent even though nothing was written.
	if !j.Persisted {
		t.Errorf("persisted = false, want true (intent)")
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
