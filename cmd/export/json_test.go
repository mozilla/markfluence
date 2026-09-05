package export

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/mozilla/markfluence/internal/pagetree"
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

// emit builds the document through the command's own builder, so a renamed key
// or a changed summary has to reach the schema through this test rather than
// past a copy of the shape.
func emit(t *testing.T, res ...result) []byte {
	t.Helper()
	env := envelope(res, markerSkipped, "/out", len(res), 0, 0)
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

	// Built by the command, not restated here: a renamed key or a changed summary
	// in failEnvelope has to reach the schema through this test.
	env := failEnvelope("9", errors.New("page 9 not found"), jsonout.CodeNotFound)
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

// TestSchemaConformanceTree validates the shapes only a tree produces: a page
// carrying a parent: path, a page that was never fetched because its own parent
// failed, and a summary reporting a skip and a planted project file.
func TestSchemaConformanceTree(t *testing.T) {
	root := result{
		page: testPage(), destPath: "out/home.md", pageStatus: statusWrote,
		place: placement{file: "home.md"},
	}
	child := result{
		page: testPage(), destPath: "out/home/child.md", pageStatus: attachfile.StatusSkipped,
		place: placement{dir: "home", file: "home/child.md", parentFile: "../home.md"},
	}
	orphan := result{
		node:  &pagetree.Node{ID: "789", Title: "Escalation", ParentID: "456", Space: "ENG"},
		place: placement{dir: "home/child", file: "home/child/escalation.md"},
		err:   errors.New("parent page was not exported; skipping"),
		code:  jsonout.CodeValidation,
	}

	env := envelope([]result{root, child, orphan}, markerWrote, "/out", 2, 1, 1)
	var buf bytes.Buffer
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatal(err)
	}
	schematest.ValidateEnvelope(t, buf.Bytes())

	var doc struct {
		Roots   []string `json:"roots"`
		Results []struct {
			PageID     string  `json:"page_id"`
			ParentFile *string `json:"parent_file"`
		} `json:"results"`
		Summary struct {
			Skipped     int     `json:"skipped"`
			ProjectFile *string `json:"project_file"`
		} `json:"summary"`
	}
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Results[0].ParentFile != nil {
		t.Errorf("root parent_file = %v, want null", *doc.Results[0].ParentFile)
	}
	if doc.Results[1].ParentFile == nil || *doc.Results[1].ParentFile != "../home.md" {
		t.Errorf("child parent_file = %v, want ../home.md", doc.Results[1].ParentFile)
	}
	if doc.Results[2].PageID != "789" {
		t.Errorf("a page that was never fetched must still be named: %+v", doc.Results[2])
	}
	if doc.Summary.ProjectFile == nil || *doc.Summary.ProjectFile != markerWrote {
		t.Errorf("project_file = %v, want %q", doc.Summary.ProjectFile, markerWrote)
	}
	if doc.Summary.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", doc.Summary.Skipped)
	}
	if len(doc.Roots) != 1 {
		t.Errorf("roots = %v, want the destination", doc.Roots)
	}
}
