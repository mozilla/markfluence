package pagetree

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

// node is a fixture row: what the fake server returns for one child.
type node struct {
	id, kind, title string
	position        int64
}

// treeServer serves v1 child/page and child/folder from a fixture map of
// parent id -> children, and counts requests so the depth accounting can be
// asserted by cost as well as by output.
func treeServer(t *testing.T, tree map[string][]node) (*client.ConfluenceClient, *int) {
	t.Helper()
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		// .../content/{id}/child/{kind}
		if len(parts) < 6 {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(500)
			return
		}
		parentID, want := parts[len(parts)-3], parts[len(parts)-1]

		rows := make([]map[string]any, 0)
		for _, n := range tree[parentID] {
			if n.kind != want {
				continue
			}
			slug := "pages"
			if n.kind == "folder" {
				slug = "folder"
			}
			rows = append(rows, map[string]any{
				"id": n.id, "type": n.kind, "title": n.title, "status": "current",
				"extensions": map[string]any{"position": n.position},
				"_links":     map[string]any{"webui": fmt.Sprintf("/spaces/ENG/%s/%s", slug, n.id)},
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": rows})
	}))
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"}), &calls
}

// fixture: root holds a folder and two pages, interleaved by position; the
// folder holds a page; that page holds a page three levels down.
func fixture() map[string][]node {
	return map[string][]node{
		"root": {
			{"f1", "folder", "Articles", 666},
			{"p1", "page", "Alpha", 200},
			{"p2", "page", "Beta", 900},
		},
		"f1": {{"p3", "page", "Inside Articles", 10}},
		"p3": {{"p4", "page", "Deeper", 10}},
	}
}

func titles(nodes []Node) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, fmt.Sprintf("%s:%s@%d", n.Type[:1], n.Title, n.Depth))
	}
	return out
}

// TestWalkMergesSiblingsByPosition is the ordering rule: pages and folders come
// from separate requests, so without the merge all folders would sort ahead of
// all pages and the output would not match what Confluence shows.
func TestWalkMergesSiblingsByPosition(t *testing.T) {
	c, _ := treeServer(t, fixture())
	got, err := Walk(c, "root", 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	want := []string{"p:Alpha@1", "f:Articles@1", "p:Beta@1"}
	if fmt.Sprint(titles(got)) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v (position 200, 666, 900)", titles(got), want)
	}
}

// TestWalkFolderCostsALevel is the depth semantics: a folder is a level like any
// other, so its contents need one more depth than the folder row itself.
func TestWalkFolderCostsALevel(t *testing.T) {
	c, calls := treeServer(t, fixture())

	got, err := Walk(c, "root", 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, n := range got {
		if n.Title == "Inside Articles" {
			t.Error("depth 1 must not reach inside a child folder")
		}
	}
	// One page request + one folder request for the root, and nothing deeper:
	// the depth check has to happen before the requests, not after.
	if *calls != 2 {
		t.Errorf("calls = %d, want 2 (no requests below the depth limit)", *calls)
	}

	c2, _ := treeServer(t, fixture())
	got2, err := Walk(c2, "root", 2)
	if err != nil {
		t.Fatalf("Walk depth 2: %v", err)
	}
	var found bool
	for _, n := range got2 {
		if n.Title == "Inside Articles" {
			found = true
			if n.Depth != 2 {
				t.Errorf("Inside Articles depth = %d, want 2", n.Depth)
			}
			if n.ParentID != "f1" {
				t.Errorf("ParentID = %q, want f1", n.ParentID)
			}
		}
		if n.Title == "Deeper" {
			t.Error("depth 2 must not reach the third level")
		}
	}
	if !found {
		t.Error("depth 2 must reach inside a child folder")
	}
}

// TestWalkDepthFirst pins that a subtree prints under its parent rather than
// after all of the parent's siblings, which is what makes indentation readable.
func TestWalkDepthFirst(t *testing.T) {
	c, _ := treeServer(t, fixture())
	got, err := Walk(c, "root", AllDepths)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	want := []string{"p:Alpha@1", "f:Articles@1", "p:Inside Articles@2", "p:Deeper@3", "p:Beta@1"}
	if fmt.Sprint(titles(got)) != fmt.Sprint(want) {
		t.Errorf("order = %v, want %v", titles(got), want)
	}
}

func TestWalkBuildsSpaceAndURLFromWebUI(t *testing.T) {
	c, _ := treeServer(t, fixture())
	got, err := Walk(c, "root", 1)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	for _, n := range got {
		if n.Space != "ENG" {
			t.Errorf("%s space = %q, want ENG", n.Title, n.Space)
		}
		if !strings.HasSuffix(n.URL, "/wiki/spaces/ENG/pages/"+n.ID) &&
			!strings.HasSuffix(n.URL, "/wiki/spaces/ENG/folder/"+n.ID) {
			t.Errorf("%s url = %q, want a site-rooted webui path", n.Title, n.URL)
		}
	}
}

func TestWalkEmptyIsNotAnError(t *testing.T) {
	c, _ := treeServer(t, map[string][]node{})
	got, err := Walk(c, "root", AllDepths)
	if err != nil {
		t.Fatalf("an empty subtree must not be an error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d nodes, want none", len(got))
	}
}

// TestWalkSurvivesACycle guards the unbounded case: without the visited set a
// self-referential tree would recurse until the stack gave out.
func TestWalkSurvivesACycle(t *testing.T) {
	c, _ := treeServer(t, map[string][]node{
		"root": {{"a", "page", "A", 1}},
		"a":    {{"root", "page", "Root again", 1}, {"a", "page", "Itself", 2}},
	})
	got, err := Walk(c, "root", AllDepths)
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	if len(got) != 1 || got[0].Title != "A" {
		t.Errorf("got %v, want just A", titles(got))
	}
}
