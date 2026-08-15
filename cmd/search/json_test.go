package search

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

// envelopeFor builds the document report emits, so the schema is validated
// against the command's own builders rather than a hand-copied literal.
func envelopeFor(res client.SearchResults) jsonout.Envelope {
	results := make([]any, 0, len(res.Matches))
	for _, m := range res.Matches {
		results = append(results, buildResult(m))
	}
	return jsonout.NewEnvelope(command, results, buildSummary(res))
}

func TestSchemaConformance(t *testing.T) {
	res := client.SearchResults{
		Matches: []client.SearchMatch{
			{ID: "500", Type: "page", Title: "Deployment runbook", Space: "ENG",
				URL:     "https://wiki.example.net/wiki/spaces/ENG/pages/500/Deployment+runbook",
				Excerpt: "Deploying to prod requires an approved change request."},
			{ID: "300", Type: "blogpost", Title: "Why we deploy on Fridays", Space: "OPS",
				URL: "https://wiki.example.net/wiki/spaces/OPS/blog/300", Excerpt: "It turns out we should not."},
			// --type all can return a content type outside any fixed set, which is
			// why the schema's type is an open string.
			{ID: "200", Type: "whiteboard", Title: "Deploy sketch", Space: "ENG",
				URL: "https://wiki.example.net/wiki/spaces/ENG/whiteboard/200", Excerpt: ""},
			// A hit with no derivable link: space and url are both null.
			{ID: "100", Type: "page", Title: "Deploys", Excerpt: "text"},
		},
		More:    true,
		Skipped: 2,
	}
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(res)); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestEmptyEnvelopeConforms is the no-match case, which is a success with zero
// results rather than a failure -- so the schema has to accept an empty array.
func TestEmptyEnvelopeConforms(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(client.SearchResults{})); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// TestSkippedOnlyEnvelopeConforms is the case the skipped field exists for: every
// row the query matched was unaddressable, so results is empty while the search
// itself succeeded. Without skipped this document would be indistinguishable from
// a genuine miss.
func TestSkippedOnlyEnvelopeConforms(t *testing.T) {
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, envelopeFor(client.SearchResults{Skipped: 388})); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	var doc struct {
		Summary jsonSearchSummary `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if doc.Summary.Total != 0 || doc.Summary.Skipped != 388 {
		t.Errorf("summary = %+v, want total 0 and skipped 388", doc.Summary)
	}
}

// TestErrorObjectConforms covers search's only failure path. Like find and unlike
// the per-page commands it has no results[0] failure variant, because there is no
// page id to name -- so the stderr document is the whole contract.
func TestErrorObjectConforms(t *testing.T) {
	for _, tc := range []struct {
		name string
		msg  string
		code jsonout.Code
	}{
		{"unknown space", `space "ENGG" not found`, jsonout.CodeValidation},
		{"empty query", "no query given: QUERY must not be empty", jsonout.CodeValidation},
		{"bad type", `invalid --type "folder": full text cannot match a folder`, jsonout.CodeValidation},
		{"bad limit", `invalid --limit "0": want a positive number or "all"`, jsonout.CodeValidation},
		{"conflicting flags", "--cql cannot be combined with --space", jsonout.CodeValidation},
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
	got, err := json.MarshalIndent(buildResult(client.SearchMatch{
		ID: "500", Type: "page", Title: "Deployment runbook", Space: "ENG",
		URL:     "https://wiki.example.net/wiki/spaces/ENG/pages/500/Deployment+runbook",
		Excerpt: "Deploying to prod.",
	}), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "ok": true,
  "id": "500",
  "type": "page",
  "title": "Deployment runbook",
  "space": "ENG",
  "url": "https://wiki.example.net/wiki/spaces/ENG/pages/500/Deployment+runbook",
  "excerpt": "Deploying to prod."
}`
	if string(got) != want {
		t.Errorf("result mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

// TestBuildResultHasNoStatus: the search index cannot see archived content, so
// every hit is current. A status field would imply search can report an archived
// page, which is precisely the case it misses.
func TestBuildResultHasNoStatus(t *testing.T) {
	b, err := json.Marshal(buildResult(client.SearchMatch{ID: "1", Type: "page", Title: "X"}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(b, []byte(`"status"`)) {
		t.Errorf("result = %s, want no status field", b)
	}
}

func TestBuildResultNullsWhatIsMissing(t *testing.T) {
	res := buildResult(client.SearchMatch{ID: "1", Type: "page", Title: "X"})
	if res.Space != nil {
		t.Errorf("space = %q, want null", *res.Space)
	}
	if res.URL != nil {
		t.Errorf("url = %q, want null", *res.URL)
	}
	// A hit matched on its title alone has no excerpt.
	if res.Excerpt != nil {
		t.Errorf("excerpt = %q, want null", *res.Excerpt)
	}
}

func TestBuildSummaryMarshal(t *testing.T) {
	got, err := json.MarshalIndent(buildSummary(client.SearchResults{
		Matches: make([]client.SearchMatch, 25), More: true, Skipped: 3,
	}), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{
  "total": 25,
  "succeeded": 25,
  "failed": 0,
  "truncated": true,
  "skipped": 3
}`
	if string(got) != want {
		t.Errorf("summary mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

// TestBuildSummaryAlwaysEmitsEveryField: nothing here uses omitempty, so a
// truncated:false / skipped:0 document still carries both keys. A consumer
// branching on their presence would otherwise see them vanish on the common path.
func TestBuildSummaryAlwaysEmitsEveryField(t *testing.T) {
	b, err := json.Marshal(buildSummary(client.SearchResults{}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"total":0,"succeeded":0,"failed":0,"truncated":false,"skipped":0}`
	if string(b) != want {
		t.Errorf("summary = %s, want %s", b, want)
	}
}
