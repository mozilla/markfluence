package info

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func fullReport() report {
	return report{
		id:         "123",
		title:      "Foo",
		status:     "current",
		space:      "ENG",
		parentID:   "456",
		versionNum: 7,
		widthKnown: true,
		width:      jsonout.PageWidth{Value: "max", Default: true},
		createdAt:  "2026-07-01T00:00:00Z",
		creator:    "Ada",
		creatorID:  "acc-1",
		updatedAt:  "2026-07-20T12:00:00Z",
		editor:     "Bo",
		editorID:   "acc-2",
		message:    "Updated via markfluence",
		url:        "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
	}
}

func marshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestJSONResultFull(t *testing.T) {
	got := marshal(t, fullReport().jsonResult())
	want := `{
  "ok": true,
  "page_id": "123",
  "title": "Foo",
  "page_status": "current",
  "space": "ENG",
  "parent": "456",
  "version": {
    "number": 7
  },
  "page_width": {
    "value": "max",
    "default": true
  },
  "created": {
    "at": "2026-07-01T00:00:00Z",
    "by": {
      "account_id": "acc-1",
      "name": "Ada"
    }
  },
  "updated": {
    "at": "2026-07-20T12:00:00Z",
    "by": {
      "account_id": "acc-2",
      "name": "Bo"
    }
  },
  "message": "Updated via markfluence",
  "url": "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
  "properties": null
}`
	if got != want {
		t.Errorf("jsonResult mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestJSONResultTopLevelAndUnknownWidth(t *testing.T) {
	r := fullReport()
	r.parentID = ""      // top-level
	r.widthKnown = false // width read failed
	res := r.jsonResult()
	if res.Parent != nil {
		t.Errorf("Parent = %v, want nil for top-level", *res.Parent)
	}
	if res.PageWidth != nil {
		t.Errorf("PageWidth = %v, want nil when width unknown", *res.PageWidth)
	}
}

func TestJSONResultPropertiesGating(t *testing.T) {
	// Not requested: null.
	if r := fullReport().jsonResult(); r.Properties != nil {
		t.Errorf("Properties = %v, want nil when --properties absent", r.Properties)
	}
	// Requested and empty: [] (non-nil).
	r := fullReport()
	r.withProps = true
	r.properties = nil
	if got := r.jsonResult().Properties; got == nil {
		t.Errorf("Properties = nil, want [] when --properties given")
	}
	// Requested with values: sorted.
	r.properties = []client.Property{{Key: "z", Value: "1"}, {Key: "a", Value: "2"}}
	props := r.jsonResult().Properties
	if len(props) != 2 || props[0].Key != "a" || props[1].Key != "z" {
		t.Errorf("Properties not sorted by key: %+v", props)
	}
}

func TestJSONResultNoAuthorWhenIDMissing(t *testing.T) {
	r := fullReport()
	r.creatorID = "" // timestamp present, author unknown
	res := r.jsonResult()
	if res.Created == nil || res.Created.By != nil {
		t.Errorf("Created = %+v, want stamp with nil By", res.Created)
	}
}

// TestSchemaConformance validates real info envelopes (success and the
// operational-failure single-target shape) against the published JSON Schema.
func TestSchemaConformance(t *testing.T) {
	success := jsonout.NewEnvelope("info", []any{fullReport().jsonResult()},
		map[string]int{"total": 1, "succeeded": 1, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, success); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	failRes := map[string]any{"ok": false, "page_id": "999", "error": "page 999 not found", "code": jsonout.CodeNotFound}
	failEnv := jsonout.NewEnvelope("info", []any{failRes}, map[string]int{"total": 1, "succeeded": 0, "failed": 1})
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}
