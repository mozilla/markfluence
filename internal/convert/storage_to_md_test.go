package convert_test

import (
	"encoding/json"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
)

const storage2mdDir = "testdata/storage2md"

// TestStorageToMarkdown runs every case under testdata/storage2md: an input.storage
// fragment converted to markdown and exact-matched against its output.md golden.
// Regenerate goldens with `go test ./internal/convert -run TestStorageToMarkdown -update`.
func TestStorageToMarkdown(t *testing.T) {
	entries, err := os.ReadDir(storage2mdDir)
	if err != nil {
		t.Fatalf("reading %s: %v", storage2mdDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join(storage2mdDir, name)
			in, err := os.ReadFile(filepath.Join(caseDir, "input.storage"))
			if err != nil {
				t.Fatalf("reading input: %v", err)
			}
			md, err := convert.StorageToMarkdown(string(in), convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			goldenPath := filepath.Join(caseDir, "output.md")
			if *update {
				if err := os.WriteFile(goldenPath, []byte(md), 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden (run with -update): %v", err)
			}
			if md != string(want) {
				t.Errorf("golden mismatch for %q\n--- got ---\n%s\n--- want ---\n%s", name, md, want)
			}
		})
	}
}

// TestStorageToMarkdownAcceptsForwardCorpus feeds every forward regression
// golden's storage HTML back through StorageToMarkdown, asserting the converter
// never errors on real emitted storage.
func TestStorageToMarkdownAcceptsForwardCorpus(t *testing.T) {
	entries, err := os.ReadDir(regressionDir)
	if err != nil {
		t.Fatalf("reading %s: %v", regressionDir, err)
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || strings.HasPrefix(e.Name(), "_") {
			continue
		}
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(regressionDir, name, "test.output"))
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			var page struct {
				HTML string `json:"html"`
			}
			if err := json.Unmarshal(data, &page); err != nil {
				t.Fatalf("parsing golden: %v", err)
			}
			if _, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{}); err != nil {
				t.Errorf("StorageToMarkdown(%q storage): %v", name, err)
			}
		})
	}
}

// TestRoundTripStableCallouts checks that markdown constructs whose forward
// mapping is lossless survive md -> storage -> md unchanged.
func TestRoundTripStableCallouts(t *testing.T) {
	src := strings.Join([]string{
		"# Title",
		"",
		"A paragraph with **bold**, *italic*, and `code`.",
		"",
		"> [!NOTE]",
		"> A note.",
		"",
		"> [!WARNING]",
		"> Be careful.",
		"",
		"```python",
		"print(\"hi\")",
		"```",
	}, "\n") + "\n"

	md, err := frontmatter.Parse("main.md", src)
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	got, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != src {
		t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestRoundTripTableAlignment checks which column alignments survive
// md -> storage -> md. Center and right make the trip. Left does not: Confluence
// has no explicit left, so a ":---" column publishes with no alignment markup and
// is indistinguishable from an unaligned one on the way back.
func TestRoundTripTableAlignment(t *testing.T) {
	src := strings.Join([]string{
		"| Left | Center | Right | Plain |",
		"| :--- | :---: | ---: | --- |",
		"| a | b | c | d |",
	}, "\n") + "\n"
	want := strings.Join([]string{
		"| Left | Center | Right | Plain |",
		"| --- | :---: | ---: | --- |",
		"| a | b | c | d |",
	}, "\n") + "\n"

	md, err := frontmatter.Parse("main.md", src)
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	// The align attribute is the one form Confluence discards, so it must not be
	// what carries the alignment.
	if strings.Contains(page.HTML, "align=\"") {
		t.Errorf("published storage still uses the align attribute:\n%s", page.HTML)
	}
	got, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestRoundTripTableCellBG checks that a cell background marker survives
// md -> storage -> md, including the swatch-name normalization (a #hex that
// matches a named swatch comes back as the name) and the gray/grey collision
// (both spellings resolve to the same hex, and grey wins on the way back).
func TestRoundTripTableCellBG(t *testing.T) {
	src := strings.Join([]string{
		"| status | note |",
		"| --- | --- |",
		"| <!-- bg:light-red --> down | <!-- bg:#ffebe6 --> also down |",
		"| <!-- bg:gray --> unknown |  |",
	}, "\n") + "\n"
	want := strings.Join([]string{
		"| status | note |",
		"| --- | --- |",
		"| <!-- bg:light-red --> down | <!-- bg:light-red --> also down |",
		"| <!-- bg:grey --> unknown |  |",
	}, "\n") + "\n"

	md, err := frontmatter.Parse("main.md", src)
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if !strings.Contains(page.HTML, `data-highlight-colour="#ffebe6"`) {
		t.Errorf("published storage missing data-highlight-colour:\n%s", page.HTML)
	}
	got, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

// TestStorageToMarkdownJoinsMultilineCells checks that a cell holding more
// than one line survives export. Confluence's editor writes one <p> per line
// -- Enter inside a cell starts a new <p>, it does not insert a <br> -- and
// rendering each independently the way ordinary block content is loses every
// line break, running the lines together with no separator. A real newline
// can't stand in for it (a GFM table row is one physical line), so the fix
// joins with a literal "<br>" instead; the same substitution also catches a
// bare mid-line <br> (Shift+Enter, no surrounding <p> split), which otherwise
// renders as the two-trailing-spaces hard break valid in ordinary block
// content but not inside a single table row.
func TestStorageToMarkdownJoinsMultilineCells(t *testing.T) {
	in := `<table><tbody><tr>` +
		`<td><p>line one</p><p>line two</p></td>` +
		`<td><p>mid-line<br/>break</p></td>` +
		`<td><p>plain, no wrapper issue</p></td>` +
		// An empty <p> is a deliberate blank line (Enter twice in the editor), not
		// the absence of one, and must survive rather than being silently dropped.
		`<td><p>line one</p><p></p><p>line three</p></td>` +
		`</tr></tbody></table>`
	want := "| line one<br>line two | mid-line<br>break | plain, no wrapper issue" +
		" | line one<br><br>line three |\n| --- | --- | --- | --- |\n"

	got, err := convert.StorageToMarkdown(in, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// The <br> form must itself be stable: publishing it back and exporting
	// again should reproduce the same markdown (L6, roundtrip-from-disk).
	md, err := frontmatter.Parse("main.md", got)
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	back, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if back != got {
		t.Errorf("round-trip unstable\n--- first ---\n%s--- second ---\n%s", got, back)
	}
}

// TestStorageToMarkdownPassesThroughListsInCells checks that a <ul>/<ol>
// found directly in a table cell round-trips as raw HTML rather than being
// rendered as inline content -- a GFM table row is exactly one physical line,
// so a real list (one line per item) can't be expressed as markdown list
// syntax inside a cell at all, and rendering it the way ordinary inline
// content is ran every item together with no separator: "<ul><li>one</li>
// <li>two</li></ul>" became "onetwo".
func TestStorageToMarkdownPassesThroughListsInCells(t *testing.T) {
	// The <li><p>...</p></li> form is what Confluence's own editor writes for
	// a list authored inside a cell in the browser; markfluence's own write
	// side (a <ul> typed directly into a markdown cell) never adds the <p>,
	// since goldmark's raw HTML passthrough carries it through unchanged.
	// The third cell's link href carries a literal "|": code review on the
	// original fix found that reusing cellTexts's blanket "|" -> "\|" escape
	// (needed so literal pipe *text* doesn't get read as a column boundary in
	// the single-line row a cell becomes) against serialize's raw-HTML output
	// would corrupt the href with a backslash that has no meaning inside a
	// quoted attribute -- a URL, unlike table text, has no escaping syntax at
	// all, so this must come back byte-identical.
	in := `<table><tbody><tr>` +
		`<td><ul><li>one</li><li>two</li></ul></td>` +
		`<td><ol><li><p>a</p></li><li><p>b</p></li></ol></td>` +
		`<td><ul><li><a href="https://example.com/a|b">c</a></li></ul></td>` +
		`</tr></tbody></table>`
	want := "| <ul><li>one</li><li>two</li></ul> | <ol><li><p>a</p></li><li><p>b</p></li></ol> |" +
		` <ul><li><a href="https://example.com/a|b">c</a></li></ul> |` + "\n" +
		"| --- | --- | --- |\n"

	got, err := convert.StorageToMarkdown(in, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	// The passthrough form must itself be stable end to end: publishing the
	// exported markdown must reproduce the exact storage read in above.
	md, err := frontmatter.Parse("main.md", got)
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	if !strings.Contains(page.HTML, `<ul><li>one</li><li>two</li></ul>`) {
		t.Errorf("published storage lost the list:\n%s", page.HTML)
	}
}

// TestStorageToMarkdownStripsGeneratedIDs checks that the server-generated
// ac:macro-id and ac:local-id attributes are dropped from passthrough output.
func TestStorageToMarkdownStripsGeneratedIDs(t *testing.T) {
	in := `<ac:structured-macro ac:macro-id="abc" ac:local-id="def" ac:name="status">` +
		`<ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro>`
	got, err := convert.StorageToMarkdown(in, convert.StorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "macro-id") || strings.Contains(got, "local-id") {
		t.Errorf("expected macro-id/local-id stripped, got:\n%s", got)
	}
	if !strings.Contains(got, `ac:name="status"`) {
		t.Errorf("expected the macro to pass through, got:\n%s", got)
	}
}

// TestStorageToMarkdownCoalescesSplitMarks checks the repair for a bold (or
// italic) span that Confluence's own editor splits around a link: saving a page
// through the editor stores marks per text run rather than as nested elements,
// so "<strong>text <a>link</a></strong>" -- which is all MdToConfluence ever
// writes -- can come back as two adjacent runs sharing the mark instead,
// "<strong>text </strong><a><strong>link</strong></a>". Rendered independently
// that produces "**text **[**link**](url)", whose closing ** is preceded by a
// space and so does not open emphasis at all under CommonMark's flanking rule --
// verified live on 2026-08-30 by PUTting a page's own unmodified
// atlas_doc_format back at it, which is what the editor does on every save.
func TestStorageToMarkdownCoalescesSplitMarks(t *testing.T) {
	tests := map[string]struct {
		in, want string
	}{
		"bold text then bold link": {
			in:   `<p><strong>some text </strong><a href="https://example.com"><strong>x</strong></a></p>`,
			want: "**some text [x](https://example.com)**\n",
		},
		"bold link then bold text": {
			in:   `<p><a href="https://example.com"><strong>x</strong></a><strong> more text</strong></p>`,
			want: "**[x](https://example.com) more text**\n",
		},
		"italic text then italic link": {
			in:   `<p><em>x </em><a href="https://example.com"><em>y</em></a></p>`,
			want: "*x [y](https://example.com)*\n",
		},
		"adjacent same-mark runs with no link still merge": {
			in:   `<p><strong>a</strong><strong>b</strong></p>`,
			want: "**ab**\n",
		},
		"link only partly bold does not merge": {
			in:   `<p><strong>a </strong><a href="https://example.com">b<strong>c</strong></a></p>`,
			want: "**a**[b**c**](https://example.com)\n",
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := convert.StorageToMarkdown(tc.in, convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRoundTripPassthrough verifies that the raw-storage passthrough cases
// (column layouts, unknown macros, ADF extensions) survive markdown -> storage
// -> markdown unchanged -- the whole point of emitting them in a form
// MdToConfluence re-publishes verbatim.
//
// The list is hardcoded rather than every storage2md case, because only these
// are passthrough: a case whose output is ordinary markdown is covered by its
// own golden.
func TestRoundTripPassthrough(t *testing.T) {
	for _, name := range []string{"layout", "unknown-macros", "excerpt", "aclink", "adf-panel"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(storage2mdDir, name, "output.md"))
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			md, err := frontmatter.Parse("main.md", string(src))
			if err != nil {
				t.Fatal(err)
			}
			root := testRoot(t, "")
			page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
			if err != nil {
				t.Fatalf("MdToConfluence: %v", err)
			}
			back, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			if back != string(src) {
				t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", back, src)
			}
		})
	}
}

// TestRoundTripImageSourcesViaRecordedPath closes the loop between publishing an
// image and reading it back, on real emitted storage rather than a hand-written
// fragment: every destination `read`/`export` writes must decode to exactly the
// path `update` published from. If it did not, exporting a page would rename its
// own image files, and re-publishing the export would upload them again.
//
// It used to test the two halves of a codec -- the name encoded the path, and
// this asserted the decode inverted the encode. There is no codec now: the name
// is the base name and the path lives only in the attachment's comment. So the
// property is the same sentence with a different mechanism behind it, and it
// matters more than it did, because a single recorded field is now the only
// thing standing between an export and a flattened tree.
func TestRoundTripImageSourcesViaRecordedPath(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(regressionDir, "images-encoded-src", "test.output"))
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	var page struct {
		HTML        string `json:"html"`
		Attachments []struct {
			Filename string `json:"filename"`
			Source   string `json:"source"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		t.Fatalf("parsing golden: %v", err)
	}
	if len(page.Attachments) == 0 {
		t.Fatal("golden has no attachments; the case no longer covers this")
	}

	sources := map[string]string{}
	for _, a := range page.Attachments {
		sources[a.Filename] = a.Source
	}
	md, err := convert.StorageToMarkdown(page.HTML, convert.StorageOptions{Sources: sources})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}

	// Stated as the property rather than a positional match: the case has more
	// image-looking lines than attachments on purpose (two spellings dedupe to
	// one attachment, and the bare-space line is literal text, not an image).
	dests := imageDests(md)
	for _, a := range page.Attachments {
		found := false
		for _, dest := range dests {
			if strings.ContainsAny(dest, " \t") {
				continue // not a destination at all -- would not parse as an image
			}
			if decoded, err := url.PathUnescape(dest); err == nil && decoded == a.Source {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no whitespace-free destination decodes to %q\n%s", a.Source, md)
		}
	}
}

// imageDests pulls the destinations out of the markdown image lines, in order.
func imageDests(md string) []string {
	var out []string
	for _, line := range strings.Split(md, "\n") {
		if !strings.HasPrefix(line, "![") {
			continue
		}
		if i := strings.LastIndex(line, "("); i >= 0 && strings.HasSuffix(line, ")") {
			out = append(out, line[i+1:len(line)-1])
		}
	}
	return out
}

// TestStorageToMarkdownPrefersRecordedSource checks the two ways an image path
// is recovered. The path recorded on the attachment wins because it is the only
// record there is; with none, the name is used as written and never
// interpreted -- a name containing "%2F" is a filename with a "%2F" in it, not
// a path in disguise.
func TestStorageToMarkdownPrefersRecordedSource(t *testing.T) {
	const in = `<p><ac:image ac:alt="d"><ri:attachment ri:filename="assets%2Fx.png" /></ac:image></p>`

	got, err := convert.StorageToMarkdown(in, convert.StorageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// The "%" is percent-encoded on the way out because a destination is a URL,
	// not because the name is being decoded: the file really is called
	// "assets%2Fx.png".
	if want := "![d](assets%252Fx.png)\n"; got != want {
		t.Errorf("name used verbatim: got %q, want %q", got, want)
	}

	// A recorded source overrides the name -- this is what makes an attachment
	// whose name cannot be decoded faithfully (one a human uploaded, or one from
	// an older markfluence) still resolve to the right path. The space in the
	// path is encoded on the way out: a destination is a URL, and a bare space
	// would end it, leaving markdown that is not an image at all.
	sources := map[string]string{"assets%2Fx.png": "images/original name.png"}
	got, err = convert.StorageToMarkdown(in, convert.StorageOptions{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	if want := "![d](images/original%20name.png)\n"; got != want {
		t.Errorf("from recorded source: got %q, want %q", got, want)
	}
}

// TestStorageToMarkdownRefusesAbsoluteSource covers a tampered or foreign
// attachment: neither a decoded name nor a recorded path may point a reader --
// or a later export writing files -- at an absolute location.
func TestStorageToMarkdownRefusesAbsoluteSource(t *testing.T) {
	const in = `<p><ac:image ac:alt="d"><ri:attachment ri:filename="%2Fetc%2Fpasswd.png" /></ac:image></p>`
	got, err := convert.StorageToMarkdown(in,
		convert.StorageOptions{Sources: map[string]string{"%2Fetc%2Fpasswd.png": "/etc/passwd.png"}})
	if err != nil {
		t.Fatal(err)
	}
	// Falls back to the raw attachment name rather than an absolute path, with
	// its "%" escaped. The escaping is the point: an unescaped "%2Fetc%2F..."
	// destination would decode straight back to the absolute path this refuses,
	// so encoding on the way out is what keeps the refusal from being undone by
	// the next read. It round-trips to a file literally named "%2Fetc%2Fpasswd.png".
	if want := "![d](%252Fetc%252Fpasswd.png)\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	dest := strings.TrimSuffix(strings.TrimPrefix(got, "![d]("), ")\n")
	decoded, err := url.PathUnescape(dest)
	if err != nil {
		t.Fatalf("destination is not decodable: %v", err)
	}
	if path.IsAbs(decoded) {
		t.Errorf("destination decodes to an absolute path: %q", decoded)
	}
}

// TestStorageToMarkdownParsesNamespacedLink is the regression for the crash that
// made export unusable on any page carrying an editor-authored internal link
// (#88). The decoder matches an auto-close name against Name.Local and ignores
// the prefix, so the HTML void element "link" swallowed <ac:link>, and the real
// </ac:link> then hit an empty stack: "unexpected end element </link>".
//
// The assertion is on the error, not the markdown, because the markdown is what
// the mapping tests cover and only the parse is at stake here.
func TestStorageToMarkdownParsesNamespacedLink(t *testing.T) {
	// The real shape, from page 2820571155.
	const storage = `<p>see <ac:link><ri:page ri:content-title="How to: Create and manage ` +
		`Yardstick accounts and access" ri:version-at-save="12" /><ac:link-body>Requesting ` +
		`Yardstick access</ac:link-body></ac:link> or Terraform.</p>`

	if _, err := convert.StorageToMarkdown(storage, convert.StorageOptions{}); err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
}

// TestStorageToMarkdownStillAutoClosesVoidElements guards the other half of the
// auto-close fix: dropping "link" must not drop the entries that make a bare
// <br> or <hr> parse, which is why the list is filtered rather than emptied.
func TestStorageToMarkdownStillAutoClosesVoidElements(t *testing.T) {
	md, err := convert.StorageToMarkdown(`<p>one<br>two</p><hr><p>three</p>`, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	for _, want := range []string{"one", "two", "three"} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
}

// TestStorageToMarkdownRendersADFExtensionOnce is #125: an <ac:adf-extension>
// carries its content twice -- once as the authoritative ac:adf-node, once as a
// pre-rendered ac:adf-fallback -- and the transparent-wrapper default rendered
// both, so every editor-authored purple Note panel exported doubled.
//
// Counting occurrences rather than matching a golden, because a golden
// regenerated against the bug looks perfectly plausible.
func TestStorageToMarkdownRendersADFExtensionOnce(t *testing.T) {
	const storage = `<ac:adf-extension><ac:adf-node type="panel">` +
		`<ac:adf-attribute key="panel-type">note</ac:adf-attribute>` +
		`<ac:adf-content><p>UNIQUEBODY</p></ac:adf-content></ac:adf-node>` +
		`<ac:adf-fallback><div class="panel"><div class="panelContent">` +
		`<p>UNIQUEBODY</p></div></div></ac:adf-fallback></ac:adf-extension>`

	md, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got := strings.Count(md, "UNIQUEBODY"); got != 1 {
		t.Errorf("body appears %d times, want 1:\n%s", got, md)
	}
	if strings.Contains(md, "adf-fallback") {
		t.Errorf("fallback survived:\n%s", md)
	}
	if strings.Contains(md, "panelContent") {
		t.Errorf("fallback rendering survived:\n%s", md)
	}
}

// TestStorageToMarkdownADFPassthroughDoesNotMutate: adfPassthrough copies rather
// than editing the parsed tree, which matters because headingSlugs has already
// walked it and a document may hold more than one extension. Two identical
// extensions must render identically.
//
// Uses a non-panel extension deliberately: a purple panel converts to an alert,
// so it would not exercise the passthrough path at all.
func TestStorageToMarkdownADFPassthroughDoesNotMutate(t *testing.T) {
	const one = `<ac:adf-extension><ac:adf-node type="expand">` +
		`<ac:adf-attribute key="title">Details</ac:adf-attribute>` +
		`<ac:adf-attribute key="local-id">abc123</ac:adf-attribute>` +
		`<ac:adf-content><p>same</p></ac:adf-content></ac:adf-node>` +
		`<ac:adf-fallback><div><p>same</p></div></ac:adf-fallback></ac:adf-extension>`

	md, err := convert.StorageToMarkdown(one+one, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got := strings.Count(md, `<ac:adf-node type="expand">`); got != 2 {
		t.Errorf("rendered %d nodes, want 2:\n%s", got, md)
	}
	if strings.Contains(md, "local-id") {
		t.Errorf("server-generated local-id survived:\n%s", md)
	}
	if got := strings.Count(md, `key="title"`); got != 2 {
		t.Errorf("adf-attribute kept %d times, want 2:\n%s", got, md)
	}
}

// TestStorageToMarkdownKeepsAFallbackThatIsTheOnlyCopy: the fallback is dropped
// because the node holds the same content. With no node there is nothing else,
// so dropping it would silently delete the content on export -- and then, on the
// next update, from the page.
func TestStorageToMarkdownKeepsAFallbackThatIsTheOnlyCopy(t *testing.T) {
	const storage = `<ac:adf-extension><ac:adf-fallback>` +
		`<div class="panel"><p>ONLYCOPY</p></div></ac:adf-fallback></ac:adf-extension>`

	md, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if !strings.Contains(md, "ONLYCOPY") {
		t.Errorf("content deleted:\n%s", md)
	}
}

// TestStorageToMarkdownRendersAnInlineADFExtensionOnce covers the other switch:
// inlineString's default is renderInlineChildren, which duplicated an inline
// extension exactly the way the block default did.
func TestStorageToMarkdownRendersAnInlineADFExtensionOnce(t *testing.T) {
	const storage = `<p>before <ac:adf-extension><ac:adf-node type="thing">` +
		`<ac:adf-content>INLINEBODY</ac:adf-content></ac:adf-node>` +
		`<ac:adf-fallback>INLINEBODY</ac:adf-fallback></ac:adf-extension> after</p>`

	md, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got := strings.Count(md, "INLINEBODY"); got != 1 {
		t.Errorf("body appears %d times, want 1:\n%s", got, md)
	}
	if strings.Count(md, "\n") > 1 {
		t.Errorf("inline extension broke out of its paragraph:\n%s", md)
	}
}

// publishAlert converts a one-alert document and returns the storage it
// publishes.
func publishAlert(t *testing.T, alert string) string {
	t.Helper()
	md, err := frontmatter.Parse("main.md", "> [!"+alert+"]\n> The body.\n")
	if err != nil {
		t.Fatal(err)
	}
	root := testRoot(t, "")
	page, err := convert.MdToConfluence(md, root, testIndex(t, root), "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	return page.HTML
}

// TestCalloutsRoundTrip is the property the colour-faithful map buys: every
// GitHub alert publishes to a distinct Confluence construct and reads back as
// the same alert. The map used to be many-to-one -- WARNING and CAUTION both
// became the "warning" macro -- so CAUTION could not survive this at all.
//
// It also catches the failure mode a two-sided change invites: edit
// calloutTargets without editing calloutMacroInverse, or the reverse, and every
// golden still regenerates cleanly while alerts silently change colour.
func TestCalloutsRoundTrip(t *testing.T) {
	for _, alert := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		t.Run(alert, func(t *testing.T) {
			storage := publishAlert(t, alert)
			back, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			want := "> [!" + alert + "]\n> The body.\n"
			if back != want {
				t.Errorf("round trip\n got: %q\nwant: %q\nvia storage: %s", back, want, storage)
			}
		})
	}
}

// TestCalloutTargetsAreDistinct pins the bijection itself. Two alerts sharing a
// target is exactly what made CAUTION unrecoverable, and it is an easy thing to
// reintroduce while editing the table.
func TestCalloutTargetsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, alert := range []string{"NOTE", "TIP", "IMPORTANT", "WARNING", "CAUTION"} {
		storage := publishAlert(t, alert)
		if prev, dup := seen[storage]; dup {
			t.Errorf("%s and %s publish identically: %s", prev, alert, storage)
		}
		seen[storage] = alert
	}
}

// TestCalloutVocabulariesDisagree guards the trap recorded in
// docs/confluence/storage-format.md: a macro name and an ADF panel type are
// different vocabularies that share strings. "note" as a macro is yellow
// (WARNING); "note" as a panel type is purple (IMPORTANT). Anything that merges
// the two lookups repaints panels.
func TestCalloutVocabulariesDisagree(t *testing.T) {
	read := func(storage string) string {
		t.Helper()
		md, err := convert.StorageToMarkdown(storage, convert.StorageOptions{})
		if err != nil {
			t.Fatalf("StorageToMarkdown: %v", err)
		}
		return md
	}
	macroNote := read(`<ac:structured-macro ac:name="note" ac:schema-version="1">` +
		`<ac:rich-text-body><p>body</p></ac:rich-text-body></ac:structured-macro>`)
	panelNote := read(`<ac:adf-extension><ac:adf-node type="panel">` +
		`<ac:adf-attribute key="panel-type">note</ac:adf-attribute>` +
		`<ac:adf-content><p>body</p></ac:adf-content></ac:adf-node></ac:adf-extension>`)

	if !strings.Contains(macroNote, "[!WARNING]") {
		t.Errorf(`the "note" macro is yellow and must read back as WARNING, got: %s`, macroNote)
	}
	if !strings.Contains(panelNote, "[!IMPORTANT]") {
		t.Errorf(`the "note" panel type is purple and must read back as IMPORTANT, got: %s`, panelNote)
	}
	if macroNote == panelNote {
		t.Errorf("the two vocabularies' \"note\" must not converge: %s", macroNote)
	}
}
