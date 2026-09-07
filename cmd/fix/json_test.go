package fix

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
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
  "reordered": false,
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

// TestProcessFileClassifiesALocateFailureByOrigin is the consistency claim of
// #133 asserted from fix's side: the same rejected credential that create's
// preflight reports AUTH must report AUTH here too. It runs through processFile
// rather than the classifier, so what is pinned is the wiring -- a genuine 404
// still reports NOT_FOUND, and a file with nothing to locate by still reports
// VALIDATION.
func TestProcessFileClassifiesALocateFailureByOrigin(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		status   int
		respBody string
		want     jsonout.Code
	}{
		{
			"a rejected credential is AUTH, not NOT_FOUND",
			"---\ntitle: A\npage_id: 123\n---\nbody\n",
			http.StatusNotFound, `{"statusCode":404,"title":"Not Found"}`,
			jsonout.CodeAuth,
		},
		{
			// A genuine 404 never reaches the classifier: GetPageOrNil reports
			// the page as absent, and locatePage turns that into its own
			// message about the id in the file.
			"a genuine 404 is a local failure about the id",
			"---\ntitle: A\npage_id: 123\n---\nbody\n",
			http.StatusNotFound, `{"statusCode":404,"title":"Cannot find a page with id 123"}`,
			jsonout.CodeValidation,
		},
		{
			"a 500 is API",
			"---\ntitle: A\npage_id: 123\n---\nbody\n",
			http.StatusInternalServerError, `boom`,
			jsonout.CodeAPI,
		},
		{
			// The row that distinguishes CodeOr from the type check it
			// replaced. An undecodable 200 is a request that produced no
			// usable answer and carries no status to classify by, so the old
			// rule took the VALIDATION fallback and blamed the file. Chosen
			// over a dead server because a request failure on a GET spends the
			// full retry budget in real time outside internal/client.
			"a response that will not decode is NETWORK",
			"---\ntitle: A\npage_id: 123\n---\nbody\n",
			http.StatusOK, `not json at all`,
			jsonout.CodeNetwork,
		},
		{
			"nothing to locate by is VALIDATION",
			"---\nspace: ENG\n---\nbody\n",
			http.StatusOK, `{"results":[]}`,
			jsonout.CodeValidation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := clienttest.New(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.respBody))
			})
			path := filepath.Join(t.TempDir(), "a.md")
			if err := os.WriteFile(path, []byte(tt.body), 0o644); err != nil {
				t.Fatal(err)
			}
			r := processFile(path, c)
			if r.ok {
				t.Fatal("processFile should have failed")
			}
			if r.code != tt.want {
				t.Errorf("code = %q, want %q (error: %s)", r.code, tt.want, r.errMsg)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }
