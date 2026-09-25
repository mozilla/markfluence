package parentref

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
	"github.com/mozilla/markfluence/internal/project"
)

// rootWith builds a project root holding files, keyed by root-relative path.
func rootWith(t *testing.T, projectFile string, files map[string]string) *project.Root {
	t.Helper()
	dir := t.TempDir()
	files[project.Filename] = projectFile
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, err := project.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

func TestResolve(t *testing.T) {
	root := rootWith(t, "pages:\n  entry.md:\n    page_id: 30\n", map[string]string{
		"parent.md":      "---\npage_id: 10\n---\n",
		"entry.md":       "# In the manifest\n",
		"unpublished.md": "---\ntitle: Not yet\n---\n",
		"sub/child.md":   "",
	})
	sub := filepath.Join(root.Dir, "sub")
	for _, tc := range []struct{ ref, want, err string }{
		{"12345", "12345", ""},
		{"../parent.md", "10", ""},
		// Its page_id is in its pages: entry, not its frontmatter (#139).
		{"../entry.md", "30", ""},
		{"../unpublished.md", "", "not yet published"},
		{"../missing.md", "", "parent file not found"},
		{"../../outside.md", "", "outside the documentation root"},
	} {
		got, err := Resolve(root, sub, tc.ref)
		switch {
		case tc.err == "" && (err != nil || got != tc.want):
			t.Errorf("Resolve(%q) = %q, %v; want %q", tc.ref, got, err, tc.want)
		case tc.err != "" && (err == nil || !strings.Contains(err.Error(), tc.err)):
			t.Errorf("Resolve(%q) error = %v; want one containing %q", tc.ref, err, tc.err)
		}
	}
}

func TestLocateRefusesSymlinks(t *testing.T) {
	root := rootWith(t, "", map[string]string{"real.md": "---\npage_id: 1\n---\n"})
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "p.md"), []byte("---\npage_id: 2\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.md", filepath.Join(root.Dir, "link.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root.Dir, "out")); err != nil {
		t.Fatal(err)
	}
	for ref, want := range map[string]string{
		"link.md":  "is a symlink",
		"out/p.md": "outside the documentation root",
	} {
		if _, err := Locate(root, root.Dir, ref); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Locate(%q) error = %v; want one containing %q", ref, err, want)
		}
	}
}

func TestLocateWithNoRoot(t *testing.T) {
	if _, err := Locate(nil, "/tmp", "p.md"); err == nil {
		t.Error("want an error with no root")
	}
}

// fakeNode is a page or folder in serve's fake tree.
type fakeNode struct{ kind, space, parent, parentType string }

// serve answers the v2 page and folder routes from nodes, recording each path.
func serve(nodes map[string]fakeNode, requests *[]string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.URL.Path)
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		kind := strings.TrimSuffix(parts[3], "s")
		n, ok := nodes[parts[4]]
		if !ok || n.kind != kind {
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"errors":[{"status":404,"title":"Cannot find content with id [%s]"}]}`, parts[4])
			return
		}
		parent := ""
		if n.parent != "" {
			parent = fmt.Sprintf(`,"parentId":%q,"parentType":%q`, n.parent, n.parentType)
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"title":"T%s","spaceId":%q%s}`, parts[4], parts[4], n.space, parent)
	}
}

func TestLookup(t *testing.T) {
	var reqs []string
	c := clienttest.New(t, serve(map[string]fakeNode{
		"1": {kind: "page", space: "S"},
		"2": {kind: "folder", space: "S", parent: "1", parentType: "page"},
		"3": {kind: "page", space: "OTHER"},
		"4": {kind: "folder", space: "OTHER"},
	}, &reqs))
	for _, tc := range []struct{ id, kind, err string }{
		{"1", "page", ""},
		{"2", "folder", ""},
		{"3", "", "parent page 3 is not in the target space"},
		{"4", "", "parent folder 4 is not in the target space"},
		{"9", "", "parent 9 not found: no page or folder has that id"},
	} {
		got, err := Lookup(c, tc.id, "S")
		switch {
		case tc.err == "" && (err != nil || got.Kind != tc.kind):
			t.Errorf("Lookup(%s) = %+v, %v; want a %s", tc.id, got, err, tc.kind)
		case tc.err != "" && (err == nil || err.Error() != tc.err):
			t.Errorf("Lookup(%s) error = %v; want %q", tc.id, err, tc.err)
		}
	}
}

// Within walks parents through pages and folders alike, and stops at the top.
func TestWithin(t *testing.T) {
	var reqs []string
	c := clienttest.New(t, serve(map[string]fakeNode{
		"1":  {kind: "page", space: "S", parent: "0", parentType: "page"},
		"0":  {kind: "page", space: "S"},
		"20": {kind: "folder", space: "S", parent: "1", parentType: "page"},
		"30": {kind: "page", space: "S", parent: "20", parentType: "folder"},
		"40": {kind: "page", space: "S", parent: "0", parentType: "page"},
	}, &reqs))
	for _, tc := range []struct {
		target Target
		want   bool
	}{
		{Target{ID: "1", Kind: "page", ParentID: "0", ParentType: "page"}, true},
		{Target{ID: "30", Kind: "page", ParentID: "20", ParentType: "folder"}, true},
		{Target{ID: "40", Kind: "page", ParentID: "0", ParentType: "page"}, false},
		{Target{ID: "0", Kind: "page"}, false},
	} {
		got, err := Within(c, tc.target, "1")
		if err != nil || got != tc.want {
			t.Errorf("Within(%s, 1) = %v, %v; want %v", tc.target.ID, got, err, tc.want)
		}
	}
}
