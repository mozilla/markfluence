package convert_test

// The round-trip property behind L5 and L6: exporting a page, publishing that
// markdown back, and exporting again yields the same markdown. Once a page has
// been through markfluence, it stops moving.
//
// This is deliberately weaker than "publishing an export back changes nothing
// on the page", which is how docs/guarantees.md words L5 -- and which does not
// hold. Measured against a live page (2026-09-05): Confluence's editor writes
// <li><p>text</p></li> where the converter emits <li>text</li>, and a TOC macro
// carries ac:local-id/ac:macro-id/data-layout attributes that the converter's
// canonical form omits. Both render identically and neither loses content, but
// the stored bytes differ, so a byte-equality test would fail for reasons that
// have nothing to do with this feature. What a reader actually depends on is
// that the *export* is stable, which is what this asserts.
//
// The corpus is every storage2md case rather than a hand-kept list, because a
// hardcoded list is how #125 stayed invisible: an ac:adf-extension existed and
// simply was not in it (d86bec4).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

// notAFixedPoint lists cases whose markdown is not stable, with the reason.
// Empty is the goal; an entry here is a known gap, not a passing test.
var notAFixedPoint = map[string]string{}

func TestRoundTripMarkdownIsAFixedPoint(t *testing.T) {
	dirs, err := os.ReadDir(filepath.Join("testdata", "storage2md"))
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		t.Run(name, func(t *testing.T) {
			if reason, skip := notAFixedPoint[name]; skip {
				t.Skip(reason)
			}
			storage, err := os.ReadFile(filepath.Join("testdata", "storage2md", name, "input.storage"))
			if err != nil {
				t.Fatal(err)
			}

			first, err := convert.StorageToMarkdown(string(storage), convert.StorageOptions{})
			if err != nil {
				t.Fatalf("storage -> markdown: %v", err)
			}

			republished := publish(t, first)
			second, err := convert.StorageToMarkdown(republished, convert.StorageOptions{})
			if err != nil {
				t.Fatalf("markdown -> storage -> markdown: %v", err)
			}

			if first != second {
				t.Errorf("markdown is not a fixed point\n--- first ---\n%s\n--- second ---\n%s",
					first, second)
			}
			ran++
		})
	}
	if ran == 0 {
		t.Fatal("no cases ran; the corpus moved")
	}
}

// imageDestRE finds the image destinations a converted document references, so
// they can be created on disk -- an image that is not there converts to
// IMAGE BROKEN, which would fail the comparison for the wrong reason.
var imageDestRE = regexp.MustCompile(`!\[[^\]]*\]\(([^) ]+)`)

// publish converts markdown back to storage the way `update` would, with every
// image it references materialized under a throwaway root.
func publish(t *testing.T, md string) string {
	t.Helper()
	root := t.TempDir()
	for _, m := range imageDestRE.FindAllStringSubmatch(md, -1) {
		dest := m[1]
		if strings.Contains(dest, "://") {
			continue // remote, never read from disk
		}
		path := filepath.Join(root, filepath.FromSlash(convert.DecodeDestinationForTest(dest)))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("PNG"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mf, err := frontmatter.Parse(filepath.Join(root, "main.md"), md)
	if err != nil {
		t.Fatal(err)
	}
	r, err := project.FromPath(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.FS.Close() }()

	page, err := convert.MdToConfluence(mf, r, testIndex(t, r), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("markdown -> storage: %v", err)
	}
	return page.HTML
}
