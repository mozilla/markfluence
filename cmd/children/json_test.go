package children

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagetree"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	nodes := []pagetree.Node{
		{ID: "11", Type: "page", Title: "Alpha", Status: "current",
			ParentID: "1", Depth: 1, Space: "ENG", URL: "https://wiki.example.net/wiki/spaces/ENG/pages/11/Alpha"},
		{ID: "22", Type: "folder", Title: "Articles", Status: "current",
			ParentID: "1", Depth: 1, Space: "ENG", URL: "https://wiki.example.net/wiki/spaces/ENG/folder/22"},
		// A row with no webui: space and url are both null, which the schema has
		// to allow.
		{ID: "33", Type: "page", Title: "Linkless", Status: "current", ParentID: "22", Depth: 2},
	}
	results := make([]any, 0, len(nodes))
	for _, n := range nodes {
		results = append(results, buildResult(n))
	}
	env := jsonout.NewEnvelope(command, results,
		map[string]int{"total": len(nodes), "succeeded": len(nodes), "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// Built by the command, not restated here: a renamed key or a changed summary
	// in failEnvelope has to reach the schema through this test.
	failEnv := failEnvelope("9", errors.New("page 9 not found"), jsonout.CodeNotFound)
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestEmptyEnvelopeConforms is the no-children case, which is a success with zero
// results rather than a failure -- so the schema has to accept an empty array.
func TestEmptyEnvelopeConforms(t *testing.T) {
	env := jsonout.NewEnvelope(command, []any{},
		map[string]int{"total": 0, "succeeded": 0, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestBuildResultMarshal(t *testing.T) {
	got, err := json.MarshalIndent(buildResult(pagetree.Node{
		ID: "11", Type: "folder", Title: "Articles", Status: "current",
		ParentID: "1", Depth: 2, Space: "ENG",
		URL: "https://wiki.example.net/wiki/spaces/ENG/folder/11",
	}), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "id": "11",
  "type": "folder",
  "title": "Articles",
  "status": "current",
  "parent_id": "1",
  "depth": 2,
  "space": "ENG",
  "url": "https://wiki.example.net/wiki/spaces/ENG/folder/11"
}`
	if string(got) != want {
		t.Errorf("result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestBuildResultNullsWithoutWebUI(t *testing.T) {
	res := buildResult(pagetree.Node{ID: "11", Type: "page", Title: "X", ParentID: "1", Depth: 1})
	if res.Space != nil {
		t.Errorf("space = %v, want null", *res.Space)
	}
	if res.URL != nil {
		t.Errorf("url = %v, want null", *res.URL)
	}
}
