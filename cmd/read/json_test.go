package read

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func TestSchemaConformance(t *testing.T) {
	parent := "456"
	parentType := "page"
	res := jsonReadResult{
		OK: true, PageID: "123", Title: "X", Space: "ENG", Parent: &parent,
		ParentType: &parentType,
		PageWidth:  &jsonout.PageWidth{Value: "max", Default: true},
		Format:     "markdown", Body: "# X\n\nhello",
	}
	env := jsonout.NewEnvelope("read", []any{res}, map[string]int{"total": 1, "succeeded": 1, "failed": 0})
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

func TestJSONReadResultMarshal(t *testing.T) {
	parent := "456"
	parentType := "page"
	res := jsonReadResult{
		OK:         true,
		PageID:     "123",
		Title:      "X",
		Space:      "ENG",
		Parent:     &parent,
		ParentType: &parentType,
		PageWidth:  &jsonout.PageWidth{Value: "max", Default: true},
		Format:     "markdown",
		Body:       "# X\n\nhello",
	}
	b, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "page_id": "123",
  "title": "X",
  "space": "ENG",
  "parent": "456",
  "parent_type": "page",
  "page_width": {
    "value": "max",
    "default": true
  },
  "format": "markdown",
  "body": "# X\n\nhello"
}`
	if string(b) != want {
		t.Errorf("read result mismatch:\n got:\n%s\n want:\n%s", b, want)
	}
}

func TestJSONReadResultTopLevelNullWidth(t *testing.T) {
	res := jsonReadResult{OK: true, PageID: "1", Format: "storage", Body: "<p/>"}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if round["parent"] != nil {
		t.Errorf("parent = %v, want null", round["parent"])
	}
	if round["page_width"] != nil {
		t.Errorf("page_width = %v, want null", round["page_width"])
	}
}
