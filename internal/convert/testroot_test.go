package convert_test

import (
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/project"
)

// testRoot builds a *project.Root for tests that call MdToConfluence but don't
// exercise image/root behavior themselves -- their markdown has no local image
// references, so any real, existing directory is a valid root. dir defaults to
// the current directory when "".
func testRoot(t *testing.T, dir string) *project.Root {
	t.Helper()
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	root, err := project.FromPath(abs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

// testIndex builds the link index for root, for tests that call
// MdToConfluence but don't exercise link resolution themselves.
func testIndex(t *testing.T, root *project.Root) *linkindex.Index {
	t.Helper()
	idx, err := linkindex.Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}
