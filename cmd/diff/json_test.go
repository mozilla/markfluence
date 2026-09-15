package diff

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/schematest"
	"github.com/mozilla/markfluence/internal/ui"
)

// sampleResult is a result with every field populated, including both a
// comparable and an uncomparable frontmatter row, so the document the schema
// sees exercises each branch of jsonResult.
func sampleResult() result {
	return result{
		file: "docs/runbook.md",
		page: &client.Page{ID: "1234567890", Title: "Deploy Runbook"},
		url:  "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title",
		meta: mustResolved(pagemeta.FromManifest),
		fields: []difference{
			{
				Field: "title", Confluence: "Deploy Runbook", Local: "Deploy Runbook (2026)",
				Source: "markfluence.yaml", Comparable: true,
			},
			{
				Field: "labels", Local: "[oncall, runbook]", Source: "frontmatter",
				Comparable: false, Note: "the page's labels could not be read",
			},
		},
		body: bodyDiff{
			Text: "--- confluence/docs/runbook.md\n+++ local/docs/runbook.md\n" +
				"@@ -6,3 +6,3 @@\n-old\n+new\n",
			Added: 1, Removed: 1,
		},
		warnings: []string{"labels could not be compared: 500"},
	}
}

// mustResolved builds a Resolved carrying just the source, which is all
// jsonResult reads from it.
func mustResolved(s pagemeta.Source) pagemeta.Resolved {
	return pagemeta.Resolved{
		Fields: map[string]string{}, Lists: map[string][]string{},
		Origin: map[string]pagemeta.Source{}, Source: s,
	}
}

func TestSchemaConformance(t *testing.T) {
	env := jsonout.NewEnvelope("diff", []any{jsonResult(sampleResult())},
		map[string]int{"total": 1, "succeeded": 1, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	// Built by the command, not restated here: a renamed key or a changed
	// summary in failEnvelope has to reach the schema through this test.
	failEnv := failEnvelope(nil, "9", errors.New("page_id 9 not found (deleted or wrong); "+
		"re-export the page, or correct the page_id"), jsonout.CodeNotFound)
	buf.Reset()
	if err := jsonout.Emit(&buf, failEnv); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

// Every source label the report can produce has to be in the schema's enum, or
// a real run emits a document the schema rejects. Checked against the labels
// rather than against a copy of the enum, so adding a spelling fails here.
func TestEverySourceLabelValidates(t *testing.T) {
	for _, s := range []pagemeta.Source{
		pagemeta.FromFrontmatter, pagemeta.FromManifest, pagemeta.FromBoth,
	} {
		label := sourceLabel(s)
		r := sampleResult()
		r.fields = []difference{{
			Field: "title", Confluence: "A", Local: "B", Source: label, Comparable: true,
		}}
		env := jsonout.NewEnvelope("diff", []any{jsonResult(r)},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		var buf bytes.Buffer
		if err := jsonout.Emit(&buf, env); err != nil {
			t.Fatalf("Emit: %v", err)
		}
		schematest.ValidateEnvelope(t, buf.Bytes())
	}

	// The project-file default's label, which no pagemeta.Source spells.
	r := sampleResult()
	r.fields = []difference{{
		Field: "page_width", Confluence: "max", Local: "wide",
		Source: "markfluence.yaml (project default)", Comparable: true,
	}}
	env := jsonout.NewEnvelope("diff", []any{jsonResult(r)},
		map[string]int{"total": 1, "succeeded": 1, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestJSONResultMarshal(t *testing.T) {
	b, err := json.MarshalIndent(jsonResult(sampleResult()), "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)

	// An uncomparable row reports a null confluence value, which is the one
	// thing a consumer has to branch on: it is not "the page has no labels".
	if !strings.Contains(got, `"confluence": null`) {
		t.Errorf("an uncomparable row did not marshal a null confluence:\n%s", got)
	}
	// differs is true here because the title row is comparable.
	if !strings.Contains(got, `"differs": true`) {
		t.Errorf("differs is not true:\n%s", got)
	}
}

// frontmatter and warnings must marshal as [] rather than null: the schema says
// array, and a nil slice would emit null on every in-sync run.
func TestEmptyListsMarshalAsArrays(t *testing.T) {
	r := sampleResult()
	r.fields = nil
	r.warnings = nil
	r.body = bodyDiff{}

	b, err := json.Marshal(jsonResult(r))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	for _, key := range []string{"frontmatter", "warnings"} {
		if _, ok := round[key].([]any); !ok {
			t.Errorf("%s = %#v, want an array", key, round[key])
		}
	}
	if round["differs"] != false || round["body_differs"] != false {
		t.Errorf("an in-sync result reported differs = %v / %v",
			round["differs"], round["body_differs"])
	}
}

// An uncomparable field alone is not a difference: nobody asked the page, so
// nothing may claim it disagrees, and the exit code must stay 0.
func TestUncomparableAloneIsNotADifference(t *testing.T) {
	r := sampleResult()
	r.body = bodyDiff{}
	r.fields = []difference{{
		Field: "labels", Local: "[runbook]", Source: "frontmatter", Comparable: false,
	}}
	if r.differs() {
		t.Error("an uncomparable field alone reported a difference")
	}
	if err := exitFor(r); err != nil {
		t.Errorf("exitFor = %v, want nil (exit 0)", err)
	}
}

// End to end under --json: the document a real run emits validates, and
// stderr stays empty -- under --json there is no second stream to write a
// human report to, so everything is in the payload.
func TestJSONRunValidates(t *testing.T) {
	dir := projectDir(t,
		"pages:\n  runbook.md:\n    title: Stale Title\n    page_id: 1234567890\n",
		map[string]string{"runbook.md": "Restart it.\n"})

	ui.SetJSON(true)
	defer ui.SetJSON(false)
	o := runDiff(t, pageStub{body: "<p>Restart them.</p>"}, dir, "runbook.md")

	if o.exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout:\n%s", o.exit, o.stdout)
	}
	schematest.ValidateEnvelope(t, []byte(o.stdout))
	if o.stderr != "" {
		t.Errorf("--json wrote to stderr:\n%s", o.stderr)
	}

	var env struct {
		Results []diffResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(o.stdout), &env); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(env.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(env.Results))
	}
	got := env.Results[0]
	if !got.Differs || !got.BodyDiffers {
		t.Errorf("differs/body_differs = %v/%v, want both true", got.Differs, got.BodyDiffers)
	}
	if got.Added != 1 || got.Removed != 1 {
		t.Errorf("added/removed = %d/%d, want 1/1", got.Added, got.Removed)
	}
	if len(got.Frontmatter) != 1 || got.Frontmatter[0].Field != "title" {
		t.Errorf("frontmatter = %#v, want one title row", got.Frontmatter)
	}
	if got.Frontmatter[0].Source != "markfluence.yaml" {
		t.Errorf("source = %q, want markfluence.yaml", got.Frontmatter[0].Source)
	}
	// The diff string is the uncoloured bytes, whatever the terminal is doing.
	if strings.Contains(got.Diff, "\x1b[") {
		t.Errorf("the diff string carries escape codes:\n%q", got.Diff)
	}
}
