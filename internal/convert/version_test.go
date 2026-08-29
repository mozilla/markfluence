package convert_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
)

func TestVersionTokenReplaced(t *testing.T) {
	md := frontmatter.Parse(
		filepath.Join(t.TempDir(), "main.md"),
		"# Title\n\n<!-- markfluence-version -->\n",
	)
	const stamp = "markfluence v1.2.3 2020-01-01T00:00:00Z"
	root := testRoot(t, filepath.Dir(md.Filename))
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", stamp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(page.HTML, "markfluence-version") {
		t.Errorf("token was not replaced:\n%s", page.HTML)
	}
	if !strings.Contains(page.HTML, stamp) {
		t.Errorf("stamp %q missing from output:\n%s", stamp, page.HTML)
	}
}
