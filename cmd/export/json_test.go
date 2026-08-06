package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
)

func testPage() *client.Page {
	p := &client.Page{ID: "123", Title: "markfluence test page", ParentID: "456"}
	p.Links.WebUI = "/spaces/ENG/pages/123/markfluence+test+page"
	return p
}

func emit(t *testing.T, res result) []byte {
	t.Helper()
	env := jsonout.NewEnvelope(command, []any{buildResult(res)},
		map[string]int{"total": 1, "succeeded": 1, "failed": 0})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	return buf.Bytes()
}

func TestSchemaConformance(t *testing.T) {
	res := result{
		page:       testPage(),
		destPath:   "out/markfluence-test-page.md",
		pageStatus: statusWrote,
		attachments: []attachment{
			{name: "assets%2Fx.png", destPath: "out/assets/x.png", status: attachfile.StatusDownloaded},
			{name: "notes.pdf", destPath: "out/notes.pdf", status: attachfile.StatusSkipped},
			{name: "old.png", status: statusSkippedUnreferenced},
			{
				name: "evil.png", status: attachfile.StatusFailed,
				err: errors.New("outside the destination directory"), code: jsonout.CodeValidation,
			},
		},
		warnings: []string{"gone.png is referenced but not attached"},
	}
	schematest.ValidateEnvelope(t, emit(t, res))
}

func TestSchemaConformancePageFailure(t *testing.T) {
	res := result{page: testPage(), err: errors.New("boom"), code: jsonout.CodeAPI}
	schematest.ValidateEnvelope(t, emit(t, res))

	failRes := map[string]any{"ok": false, "page_id": "9", "error": "page 9 not found", "code": jsonout.CodeNotFound}
	env := jsonout.NewEnvelope(command, []any{failRes},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatal(err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())
}

func TestSchemaConformanceSkipAttachments(t *testing.T) {
	res := result{page: testPage(), destPath: "out/p.md", pageStatus: statusWrote}
	schematest.ValidateEnvelope(t, emit(t, res))
}

// TestBuildResultEmptyCollectionsAreArrays keeps attachments and warnings as []
// rather than null, so a consumer can iterate without a nil check -- the same
// rule update and create follow.
func TestBuildResultEmptyCollectionsAreArrays(t *testing.T) {
	b, err := json.Marshal(buildResult(result{page: testPage(), pageStatus: statusWrote}))
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"attachments", "warnings"} {
		if _, ok := round[k].([]any); !ok {
			t.Errorf("%s = %v, want []", k, round[k])
		}
	}
}

func TestBuildResultUnreferencedHasNullDest(t *testing.T) {
	res := buildResult(result{
		page:        testPage(),
		attachments: []attachment{{name: "old.png", status: statusSkippedUnreferenced}},
	})
	if res.Attachments[0].DestPath != nil {
		t.Errorf("dest_path = %v, want null for an attachment that was not written",
			*res.Attachments[0].DestPath)
	}
}

func TestBuildResultCarriesSpaceAndParent(t *testing.T) {
	res := buildResult(result{page: testPage(), pageStatus: statusWrote})
	if res.Space != "ENG" {
		t.Errorf("space = %q, want ENG", res.Space)
	}
	if res.Parent == nil || *res.Parent != "456" {
		t.Errorf("parent = %v, want 456", res.Parent)
	}
}
