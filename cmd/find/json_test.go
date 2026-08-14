package find

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

// envelopeFor builds the document run emits, so the schema is validated against
// the command's own builders rather than a hand-copied literal.
func envelopeFor(matches []client.TitleMatch) jsonout.Envelope {
	results := make([]any, 0, len(matches))
	for _, m := range matches {
		results = append(results, buildResult(m))
	}
	return jsonout.NewEnvelope(command, results,
		map[string]int{"total": len(matches), "succeeded": len(matches), "failed": 0})
}

func TestSchemaConformance(t *testing.T) {
	matches := []client.TitleMatch{
		{ID: "500", Type: "page", Title: "Runbook", Status: "current", Space: "ENG",
			URL: "https://wiki.example.net/wiki/spaces/ENG/pages/500/Runbook"},
		// An archived page: reported because it still reserves the title.
		{ID: "400", Type: "page", Title: "Runbook", Status: "archived", Space: "ENG",
			URL: "https://wiki.example.net/wiki/spaces/ENG/pages/400/Runbook"},
		{ID: "300", Type: "folder", Title: "Runbook", Status: "current", Space: "OPS",
			URL: "https://wiki.example.net/wiki/spaces/OPS/folder/300"},
		// A match with no derivable link: space and url are both null, which the
		// schema has to allow.
		{ID: "200", Type: "page", Title: "Runbook", Status: "current"},
	}
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(matches)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestEmptyEnvelopeConforms is the no-match case, which is a success with zero
// results rather than a failure -- so the schema has to accept an empty array.
func TestEmptyEnvelopeConforms(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(nil)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestErrorObjectConforms covers find's only failure path. Unlike the per-page
// commands it has no results[0] failure variant, because there is no page id to
// name -- so the stderr document is the whole contract.
func TestErrorObjectConforms(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		code jsonout.Code
	}{
		{"unknown space", `space "ENGG" not found`, jsonout.CodeValidation},
		{"empty title", "no title given: TITLE must not be empty", jsonout.CodeValidation},
		{"search failed", "GET .../search: 500", jsonout.CodeAPI},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := jsonout.EmitError(&buf, command, tc.msg, tc.code); err != nil {
				t.Fatalf("EmitError: %v", err)
			}
			schematest.ValidateError(t, buf.Bytes())
		})
	}
}

func TestBuildResultMarshal(t *testing.T) {
	got, err := json.MarshalIndent(buildResult(client.TitleMatch{
		ID: "300", Type: "folder", Title: "Runbook", Status: "current", Space: "OPS",
		URL: "https://wiki.example.net/wiki/spaces/OPS/folder/300",
	}), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "id": "300",
  "type": "folder",
  "title": "Runbook",
  "space": "OPS",
  "status": "current",
  "url": "https://wiki.example.net/wiki/spaces/OPS/folder/300"
}`
	if string(got) != want {
		t.Errorf("result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestBuildResultNullsWithoutALink(t *testing.T) {
	res := buildResult(client.TitleMatch{ID: "1", Type: "page", Title: "X", Status: "current"})
	if res.Space != nil {
		t.Errorf("space = %v, want null", *res.Space)
	}
	if res.URL != nil {
		t.Errorf("url = %v, want null", *res.URL)
	}
}
