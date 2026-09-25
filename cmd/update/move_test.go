package update

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/schematest"
)

// node is a page or folder in the fake tree.
type node struct {
	kind, space, parent, parentType, title string
	version                                int
}

// tree is a stateful fake Confluence holding pages and folders, which a move
// actually moves and a body PUT actually versions -- so a test can run update
// twice and see the second run's view of the first.
type tree struct {
	t     *testing.T
	mu    sync.Mutex
	nodes map[string]*node
	// roots are the space's top-level pages as the v1 listing reports them,
	// with their positions.
	roots []rootRow
	// requests is every request, "METHOD path".
	requests []string
	// failMove makes the move route answer 403.
	failMove bool
}

type rootRow struct {
	id, status string
	position   int64
}

func newTree(t *testing.T) *tree {
	return &tree{t: t, nodes: map[string]*node{
		// Page 1 is the page being published, under page 100.
		"1":   {kind: "page", space: "S1", parent: "100", parentType: "page", title: "Doc", version: 3},
		"100": {kind: "page", space: "S1", title: "Old Parent"},
		"200": {kind: "page", space: "S1", title: "Runbooks"},
		"300": {kind: "folder", space: "S1", title: "A Folder"},
		"400": {kind: "page", space: "S2", title: "Elsewhere"},
		// 500 is under page 1, so moving 1 under 500 is a loop.
		"500": {kind: "page", space: "S1", parent: "1", parentType: "page", title: "Child"},
	}}
}

func (tr *tree) client() *client.ConfluenceClient {
	return clienttest.New(tr.t, func(w http.ResponseWriter, r *http.Request) {
		tr.mu.Lock()
		defer tr.mu.Unlock()
		tr.requests = append(tr.requests, r.Method+" "+r.URL.Path)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
			tr.writeNode(w, parts[4], "page")
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/wiki/api/v2/folders/"):
			tr.writeNode(w, parts[4], "folder")
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/content/page"):
			var rows []string
			for _, rr := range tr.roots {
				rows = append(rows, fmt.Sprintf(`{"id":%q,"type":"page","status":%q,"extensions":{"position":%d}}`,
					rr.id, rr.status, rr.position))
			}
			_, _ = fmt.Fprintf(w, `{"results":[%s]}`, strings.Join(rows, ","))
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/move/"):
			if tr.failMove {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"statusCode":403,"message":"no"}`))
				return
			}
			// /wiki/rest/api/content/{id}/move/{position}/{target}
			id, position, target := parts[4], parts[6], parts[7]
			n := tr.nodes[id]
			if position == client.MoveAfter {
				n.parent, n.parentType = "", ""
			} else {
				n.parent, n.parentType = target, tr.nodes[target].kind
			}
			_, _ = fmt.Fprintf(w, `{"pageId":%q}`, id)
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/wiki/api/v2/pages/"):
			var body struct {
				Version struct {
					Number int `json:"number"`
				} `json:"version"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			tr.nodes[parts[4]].version = body.Version.Number
			tr.writeNode(w, parts[4], "page")
		default:
			tr.t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (tr *tree) writeNode(w http.ResponseWriter, id, kind string) {
	n, ok := tr.nodes[id]
	if !ok || n.kind != kind {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, `{"errors":[{"status":404,"title":"Cannot find content with id [%s]"}]}`, id)
		return
	}
	var parent string
	if n.parent != "" {
		parent = fmt.Sprintf(`,"parentId":%q,"parentType":%q`, n.parent, n.parentType)
	}
	_, _ = fmt.Fprintf(w, `{"id":%q,"title":%q,"spaceId":%q%s,"version":{"number":%d,"createdAt":"2020-01-01T00:00:00Z"},`+
		`"_links":{"webui":"/spaces/%s/pages/%s/x"}}`, id, n.title, n.space, parent, n.version, spaceKey(n.space), id)
}

// spaceKey maps the fake's space ids to keys: S1 is ENG, S2 is OPS.
func spaceKey(id string) string {
	if id == "S2" {
		return "OPS"
	}
	return "ENG"
}

// writes are the requests that change something.
func (tr *tree) writes() []string {
	var out []string
	for _, r := range tr.requests {
		if !strings.HasPrefix(r, http.MethodGet) {
			out = append(out, r)
		}
	}
	return out
}

func (tr *tree) moves() []string {
	var out []string
	for _, r := range tr.requests {
		if strings.Contains(r, "/move/") {
			out = append(out, strings.TrimPrefix(r, "PUT /wiki/rest/api/content/"))
		}
	}
	return out
}

// projectWith writes markfluence.yaml and files under one root, returning the
// root directory. files maps a root-relative path to its content.
func projectWith(t *testing.T, projectFile string, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte(projectFile), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func (tr *tree) run(t *testing.T, path string) *updateResult {
	t.Helper()
	logs := actionlog.NewCache()
	r := processFile(path, tr.client(), project.NewCache(""), linkindex.NewCache(), pagedoc.NewUserCache(), logs)
	recordAction(logs, r)
	return r
}

func publishFile(t *testing.T, tr *tree, frontmatter string) *updateResult {
	t.Helper()
	dir := projectWith(t, "# marker\n", map[string]string{"f.md": "---\n" + frontmatter + "---\nHello.\n"})
	return tr.run(t, filepath.Join(dir, "f.md"))
}

func TestAnAbsentParentMakesNoParentRequest(t *testing.T) {
	tr := newTree(t)
	r := publishFile(t, tr, "page_id: 1\n")
	if !r.ok || r.move != nil {
		t.Fatalf("result = %+v, want ok and no move", r)
	}
	for _, req := range tr.requests {
		if strings.Contains(req, "/folders/") || strings.Contains(req, "/pages/100") || strings.Contains(req, "/move/") {
			t.Errorf("request %q for a file that declares no parent", req)
		}
	}
}

func TestAMatchingParentDoesNotMove(t *testing.T) {
	tr := newTree(t)
	r := publishFile(t, tr, "page_id: 1\nparent: 100\n")
	if !r.ok || r.move != nil || len(tr.moves()) != 0 {
		t.Fatalf("result = %+v moves = %v, want no move", r, tr.moves())
	}
}

func TestADeclaredParentMovesThePage(t *testing.T) {
	for _, tc := range []struct{ name, parent, want string }{
		{"a page", "200", "1/move/append/200"},
		{"a folder", "300", "1/move/append/300"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t)
			r := publishFile(t, tr, "page_id: 1\nparent: "+tc.parent+"\n")
			if !r.ok || r.status != statusPublished {
				t.Fatalf("result = %+v, want published", r)
			}
			if got := tr.moves(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("moves = %v, want [%s]", got, tc.want)
			}
			if r.move == nil || r.move.from != "100" || r.move.to != tc.parent {
				t.Errorf("move = %+v, want 100 -> %s", r.move, tc.parent)
			}
		})
	}
}

// The move is the first write: nothing else is on the page yet if it fails.
func TestTheMoveComesBeforeTheBody(t *testing.T) {
	tr := newTree(t)
	publishFile(t, tr, "page_id: 1\nparent: 200\n")
	w := tr.writes()
	if len(w) < 2 || !strings.Contains(w[0], "/move/") {
		t.Errorf("writes = %v, want the move first", w)
	}
}

func TestAParentFileResolvesToItsPageID(t *testing.T) {
	tr := newTree(t)
	dir := projectWith(t, "# marker\n", map[string]string{
		"runbooks.md":    "---\npage_id: 200\n---\n# Runbooks\n",
		"sub/f.md":       "---\npage_id: 1\nparent: ../runbooks.md\n---\nHello.\n",
		"unpublished.md": "---\ntitle: Nope\n---\n",
	})
	r := tr.run(t, filepath.Join(dir, "sub", "f.md"))
	if !r.ok {
		t.Fatalf("result = %+v", r)
	}
	if got := tr.moves(); len(got) != 1 || got[0] != "1/move/append/200" {
		t.Errorf("moves = %v, want the parent file's page", got)
	}
}

// parent: null, and each other blank spelling, is the top of the space: after
// the last current page there, skipping an archived one.
func TestANullParentMovesToTheTop(t *testing.T) {
	for _, spelling := range []string{"null", "~", ""} {
		t.Run(spelling, func(t *testing.T) {
			tr := newTree(t)
			tr.roots = []rootRow{
				{id: "10", status: "current", position: 5},
				{id: "11", status: "current", position: 90},
				{id: "12", status: "archived", position: 200},
				{id: "13", status: "current", position: 40},
			}
			r := publishFile(t, tr, "page_id: 1\nparent: "+spelling+"\n")
			if !r.ok {
				t.Fatalf("result = %+v", r)
			}
			if got := tr.moves(); len(got) != 1 || got[0] != "1/move/after/11" {
				t.Errorf("moves = %v, want after the last current root page, 11", got)
			}
			if r.move == nil || r.move.from != "100" || r.move.to != "" {
				t.Errorf("move = %+v, want 100 -> top", r.move)
			}
		})
	}
}

func TestANullParentOnATopPageDoesNotMove(t *testing.T) {
	tr := newTree(t)
	tr.nodes["1"].parent, tr.nodes["1"].parentType = "", ""
	r := publishFile(t, tr, "page_id: 1\nparent: null\n")
	if !r.ok || r.move != nil || len(tr.moves()) != 0 {
		t.Fatalf("result = %+v moves = %v, want no move", r, tr.moves())
	}
}

// Every refusal happens before any write.
func TestAParentThatCannotBeUsedFailsBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct{ name, parent, want string }{
		{"in another space", "400", "not in the target space"},
		{"missing", "999", "not found"},
		{"below the page", "500", "would make a loop"},
		{"the page itself", "1", "would make a loop"},
		{"unpublished file", "unpublished.md", "not yet published"},
		{"missing file", "gone.md", "parent file not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t)
			dir := projectWith(t, "# marker\n", map[string]string{
				"f.md":           "---\npage_id: 1\nparent: " + tc.parent + "\n---\nHello.\n",
				"unpublished.md": "---\ntitle: Nope\n---\n",
			})
			r := tr.run(t, filepath.Join(dir, "f.md"))
			if r.ok || !strings.Contains(r.errMsg, tc.want) {
				t.Fatalf("result = %+v, want a failure containing %q", r, tc.want)
			}
			if r.code != jsonout.CodeValidation {
				t.Errorf("code = %q, want VALIDATION", r.code)
			}
			if w := tr.writes(); len(w) != 0 {
				t.Errorf("writes = %v, want none", w)
			}
		})
	}
}

func TestAFailedMoveFailsTheFileWithNothingElseWritten(t *testing.T) {
	tr := newTree(t)
	tr.failMove = true
	r := publishFile(t, tr, "page_id: 1\nparent: 200\n")
	if r.ok {
		t.Fatalf("result = %+v, want a failure", r)
	}
	if w := tr.writes(); len(w) != 1 || !strings.Contains(w[0], "/move/") {
		t.Errorf("writes = %v, want only the refused move", w)
	}
}

func TestADryRunReportsTheMoveWithoutMakingIt(t *testing.T) {
	dryRun = true
	t.Cleanup(func() { dryRun = false })
	tr := newTree(t)
	r := publishFile(t, tr, "page_id: 1\nparent: 200\n")
	if !r.ok || r.move == nil || r.move.to != "200" {
		t.Fatalf("result = %+v, want the move previewed", r)
	}
	if w := tr.writes(); len(w) != 0 {
		t.Errorf("writes = %v, want none", w)
	}
	if got := r.move.describe(true); !strings.HasPrefix(got, `would move under "Runbooks"`) {
		t.Errorf("describe = %q", got)
	}
}

// A move leaves the version alone, so it neither trips the moved-page check
// nor defeats the unchanged-body skip: a second run that only moves makes no
// body PUT, and the one after that is a no-op.
func TestAMoveOnlyChangeSkipsTheBody(t *testing.T) {
	tr := newTree(t)
	dir := projectWith(t, "# marker\n", map[string]string{"f.md": "---\npage_id: 1\n---\nHello.\n"})
	path := filepath.Join(dir, "f.md")
	if r := tr.run(t, path); !r.ok {
		t.Fatalf("first run: %+v", r)
	}

	if err := os.WriteFile(path, []byte("---\npage_id: 1\nparent: 200\n---\nHello.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr.requests = nil
	r := tr.run(t, path)
	if !r.ok || r.status != statusPublished || r.move == nil {
		t.Fatalf("second run = %+v, want a published move", r)
	}
	if r.bodyChanged == nil || *r.bodyChanged {
		t.Errorf("body_changed = %v, want false", r.bodyChanged)
	}
	if w := tr.writes(); len(w) != 1 || !strings.Contains(w[0], "/move/") {
		t.Errorf("writes = %v, want the move alone", w)
	}

	tr.requests = nil
	r = tr.run(t, path)
	if !r.ok || r.status != statusSkipped || r.move != nil {
		t.Errorf("third run = %+v, want a skip", r)
	}
}

func TestSpaceMismatchFailsBeforeAnyWrite(t *testing.T) {
	for _, tc := range []struct{ name, projectFile, frontmatter, want string }{
		{"declared in the file", "# marker\n", "space: OPS\n", "this file declares OPS"},
		{"the project default", "space: OPS\n", "", project.Filename + "'s space: default is OPS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr := newTree(t)
			dir := projectWith(t, tc.projectFile, map[string]string{
				"f.md": "---\npage_id: 1\n" + tc.frontmatter + "---\nHello.\n",
			})
			r := tr.run(t, filepath.Join(dir, "f.md"))
			if r.ok || !strings.Contains(r.errMsg, tc.want) || !strings.Contains(r.errMsg, "space ENG") {
				t.Fatalf("result = %+v, want a refusal naming ENG and %q", r, tc.want)
			}
			if w := tr.writes(); len(w) != 0 {
				t.Errorf("writes = %v, want none", w)
			}
		})
	}
}

// The file's own space wins over the project default, as it does for create.
func TestTheFilesSpaceBeatsTheProjectDefault(t *testing.T) {
	tr := newTree(t)
	dir := projectWith(t, "space: OPS\n", map[string]string{"f.md": "---\npage_id: 1\nspace: ENG\n---\nHello.\n"})
	if r := tr.run(t, filepath.Join(dir, "f.md")); !r.ok {
		t.Fatalf("result = %+v, want ok", r)
	}
}

func TestMovedInJSON(t *testing.T) {
	for _, r := range []*updateResult{
		{ok: true, status: statusPublished, file: "f.md", move: &move{from: "100", to: "200"}},
		{ok: true, status: statusPublished, file: "f.md", move: &move{from: "100"}},
		{ok: true, status: statusPublished, file: "f.md"},
	} {
		b, err := json.Marshal(jsonout.NewEnvelope("update", []any{r.jsonResult()}, summarize([]*updateResult{r})))
		if err != nil {
			t.Fatal(err)
		}
		schematest.ValidateEnvelope(t, b)
	}
	got := (&updateResult{move: &move{from: "100"}}).jsonResult().Moved
	if got == nil || got.From == nil || *got.From != "100" || got.To != nil {
		t.Errorf("moved = %+v, want from 100 to null", got)
	}
}
