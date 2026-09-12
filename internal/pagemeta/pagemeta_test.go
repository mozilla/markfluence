package pagemeta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/project"
)

// rootWith builds a project root whose markfluence.yaml holds body.
func rootWith(t *testing.T, body string) *project.Root {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, project.Filename), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	root, err := project.Discover(dir)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	t.Cleanup(func() { _ = root.FS.Close() })
	return root
}

// parse builds a MarkdownFile from a frontmatter block (or "" for none).
func parse(t *testing.T, block string) *frontmatter.MarkdownFile {
	t.Helper()
	content := "body\n"
	if block != "" {
		content = "---\n" + block + "---\nbody\n"
	}
	mf, err := frontmatter.Parse("a.md", content)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return mf
}

func TestResolveFrontmatterOnly(t *testing.T) {
	root := rootWith(t, "space: ENG\n")
	r, err := Resolve("a.md", parse(t, "title: A\npage_id: 1\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != FromFrontmatter || r.MetadataSource() != FromFrontmatter {
		t.Errorf("source = %q, want frontmatter", r.Source)
	}
	if !r.Managed() {
		t.Error("not managed; a page_id in frontmatter is a claim")
	}
	if r.Fields["title"] != "A" || r.Fields["page_id"] != "1" {
		t.Errorf("fields = %#v", r.Fields)
	}
}

func TestResolveManifestOnly(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    title: A\n    page_id: 1\n    labels: [x]\n")
	r, err := Resolve("a.md", parse(t, ""), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != FromManifest || r.MetadataSource() != FromManifest {
		t.Errorf("source = %q, want manifest", r.Source)
	}
	if !r.Managed() {
		t.Error("not managed; an entry is a claim")
	}
	if r.Fields["title"] != "A" || r.Fields["page_id"] != "1" {
		t.Errorf("fields = %#v", r.Fields)
	}
	if l := r.Lists["labels"]; len(l) != 1 || l[0] != "x" {
		t.Errorf("labels = %#v", l)
	}
}

// Agreement is silent, which is what makes migration incremental: copy values
// into the manifest, verify, delete them from the files later.
func TestResolveAgreementIsSilent(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    title: A\n    page_id: 1\n")
	r, err := Resolve("a.md", parse(t, "title: A\npage_id: 1\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none", r.Warnings)
	}
	if r.Source != FromBoth {
		t.Errorf("source = %q, want both", r.Source)
	}
	// FromBoth collapses for reporting: frontmatter is the location that won
	// every field it could win.
	if r.MetadataSource() != FromFrontmatter {
		t.Errorf("metadata_source = %q, want frontmatter", r.MetadataSource())
	}
}

// A coordinate disagreement is the clobber case: a page_id pasted from an old
// file publishes over a live page.
func TestResolveCoordinateDisagreementIsAnError(t *testing.T) {
	for _, field := range []string{"page_id", "space", "parent"} {
		t.Run(field, func(t *testing.T) {
			root := rootWith(t, "pages:\n  a.md:\n    "+field+": manifestvalue\n")
			_, err := Resolve("a.md", parse(t, field+": filevalue\n"), root)
			if err == nil {
				t.Fatalf("Resolve accepted a %s disagreement, want an error", field)
			}
			for _, want := range []string{field, "filevalue", "manifestvalue", "will not guess"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %q, want it to contain %q", err, want)
				}
			}
		})
	}
}

// Soft fields are visible and recoverable, so they warn and frontmatter wins.
func TestResolveSoftDisagreementWarnsAndFrontmatterWins(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    title: Manifest\n    page_width: narrow\n")
	r, err := Resolve("a.md", parse(t, "title: File\npage_width: wide\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Fields["title"] != "File" || r.Fields["page_width"] != "wide" {
		t.Errorf("fields = %#v, want frontmatter's values", r.Fields)
	}
	if len(r.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want two", r.Warnings)
	}
	// In field order, so output is stable across runs.
	if !strings.HasPrefix(r.Warnings[0], "page_width:") || !strings.HasPrefix(r.Warnings[1], "title:") {
		t.Errorf("warnings = %#v, want them sorted by field", r.Warnings)
	}
}

// A blank value says no more than an absent key -- every null spelling already
// reads as "" -- so it must not be a conflicting answer.
func TestResolveBlankIsNotADisagreement(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    page_id: 1\n")
	for _, block := range []string{"page_id:\n", "page_id: ~\n", "page_id: null\n", "page_id: \"  \"\n"} {
		t.Run(block, func(t *testing.T) {
			r, err := Resolve("a.md", parse(t, block), root)
			if err != nil {
				t.Fatalf("Resolve(%q): %v", block, err)
			}
			if r.Fields["page_id"] != "1" {
				t.Errorf("page_id = %q, want the manifest's 1", r.Fields["page_id"])
			}
		})
	}
}

// labels is compared as a set: Confluence has no label order, so a reordering
// cannot reach the page and warning about it would be noise.
func TestResolveListOrderIsNotADisagreement(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    labels: [a, b]\n")
	r, err := Resolve("a.md", parse(t, "labels: [b, a]\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(r.Warnings) != 0 {
		t.Errorf("warnings = %#v, want none for a reordering", r.Warnings)
	}
}

func TestResolveListDisagreementWarns(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md:\n    labels: [a]\n")
	r, err := Resolve("a.md", parse(t, "labels: [b]\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "labels") {
		t.Errorf("warnings = %#v, want one about labels", r.Warnings)
	}
	if l := r.Lists["labels"]; len(l) != 1 || l[0] != "b" {
		t.Errorf("labels = %#v, want frontmatter's [b]", l)
	}
}

// The point of D7: a pristine file nothing claims is unmanaged, so a
// glob-driven CI run skips it instead of going red.
func TestResolveUnmanaged(t *testing.T) {
	root := rootWith(t, "pages:\n  other.md:\n    page_id: 1\n")
	r, err := Resolve("a.md", parse(t, ""), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != Unmanaged || r.Managed() {
		t.Errorf("source = %q managed = %v, want unmanaged", r.Source, r.Managed())
	}
	if r.MetadataSource() != Unmanaged {
		t.Errorf("metadata_source = %q, want empty", r.MetadataSource())
	}
}

// A docs tree carrying Jekyll or Hugo frontmatter has said nothing about
// Confluence. Counting any key at all would report every such file as claimed
// and fail a whole tree.
func TestResolveForeignFrontmatterIsUnmanaged(t *testing.T) {
	root := rootWith(t, "space: ENG\n")
	r, err := Resolve("a.md", parse(t, "layout: post\ndate: 2026-01-01\ndraft: false\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != Unmanaged || r.Managed() {
		t.Errorf("source = %q managed = %v, want unmanaged", r.Source, r.Managed())
	}
}

// A file carrying title: and nothing else has not said which page it is, so it
// is not claimed -- #139 is precise that a *registered* file is one with an
// entry.
func TestResolveTitleAloneDoesNotClaimAFile(t *testing.T) {
	root := rootWith(t, "space: ENG\n")
	r, err := Resolve("a.md", parse(t, "title: A\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Managed() {
		t.Error("managed; a title alone does not identify a page")
	}
	// It did contribute metadata, though, which is a different question.
	if r.Source != FromFrontmatter {
		t.Errorf("source = %q, want frontmatter", r.Source)
	}
}

// An empty entry is somebody claiming the path, which must error for want of a
// page_id rather than be skipped as unmanaged.
func TestResolveEmptyEntryStillClaimsTheFile(t *testing.T) {
	root := rootWith(t, "pages:\n  a.md: {}\n")
	r, err := Resolve("a.md", parse(t, ""), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !r.Managed() {
		t.Error("not managed; an entry is an explicit claim even when it declares nothing")
	}
	if r.Fields["page_id"] != "" {
		t.Errorf("page_id = %q, want empty so the caller reports it", r.Fields["page_id"])
	}
}

// No pages: key at all must behave exactly as markfluence did before #139.
func TestResolveWithNoManifest(t *testing.T) {
	root := rootWith(t, "space: ENG\n")
	r, err := Resolve("a.md", parse(t, "title: A\npage_id: 1\n"), root)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != FromFrontmatter || len(r.Warnings) != 0 {
		t.Errorf("source = %q warnings = %#v", r.Source, r.Warnings)
	}
	if HasManifest(root) {
		t.Error("HasManifest true with no pages: key")
	}
}

// D9 turns on nil-vs-empty: an empty pages: block is a project that has chosen
// the manifest, so new metadata belongs there rather than in frontmatter.
func TestHasManifest(t *testing.T) {
	if !HasManifest(rootWith(t, "pages: {}\n")) {
		t.Error("HasManifest false for an empty pages: block")
	}
	if HasManifest(nil) {
		t.Error("HasManifest true for a nil root")
	}
}

// A nil root is what a caller has when discovery found nothing; it must not
// panic and must read as frontmatter-only.
func TestResolveNilRoot(t *testing.T) {
	r, err := Resolve("a.md", parse(t, "page_id: 1\n"), nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if r.Source != FromFrontmatter || !r.Managed() {
		t.Errorf("source = %q managed = %v", r.Source, r.Managed())
	}
}

func TestKeyFor(t *testing.T) {
	root := rootWith(t, "pages: {}\n")
	got, ok := KeyFor(root, filepath.Join(root.Dir, "docs", "a.md"))
	if !ok || got != "docs/a.md" {
		t.Errorf("= %q/%v, want docs/a.md", got, ok)
	}
	if got, ok := KeyFor(root, filepath.Join(root.Dir, "a.md")); !ok || got != "a.md" {
		t.Errorf("= %q/%v, want a.md", got, ok)
	}
}

// A batch may span more than one project, so a file outside the root being
// consulted has no key there -- and that is not an error, just no entry.
func TestKeyForOutsideTheRoot(t *testing.T) {
	root := rootWith(t, "pages: {}\n")
	if got, ok := KeyFor(root, filepath.Join(filepath.Dir(root.Dir), "elsewhere.md")); ok {
		t.Errorf("= %q/%v, want no key for a file outside the root", got, ok)
	}
	if _, ok := KeyFor(nil, "/tmp/a.md"); ok {
		t.Error("want no key for a nil root")
	}
}
