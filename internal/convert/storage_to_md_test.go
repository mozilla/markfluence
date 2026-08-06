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
			md, err := convert.StorageToMarkdown(string(in), nil)
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
			if _, err := convert.StorageToMarkdown(page.HTML, nil); err != nil {
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

	md := frontmatter.Parse("main.md", src)
	page, err := convert.MdToConfluence(md, "https://wiki.example.net", "ENG", "vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	got, err := convert.StorageToMarkdown(page.HTML, nil)
	if err != nil {
		t.Fatalf("StorageToMarkdown: %v", err)
	}
	if got != src {
		t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", got, src)
	}
}

// TestStorageToMarkdownStripsGeneratedIDs checks that the server-generated
// ac:macro-id and ac:local-id attributes are dropped from passthrough output.
func TestStorageToMarkdownStripsGeneratedIDs(t *testing.T) {
	in := `<ac:structured-macro ac:macro-id="abc" ac:local-id="def" ac:name="status">` +
		`<ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro>`
	got, err := convert.StorageToMarkdown(in, nil)
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

// TestRoundTripPassthrough verifies that the raw-storage passthrough cases
// (column layouts and unknown macros) survive markdown -> storage -> markdown
// unchanged -- the whole point of emitting them in a form MdToConfluence
// re-publishes verbatim.
func TestRoundTripPassthrough(t *testing.T) {
	for _, name := range []string{"layout", "unknown-macros", "excerpt"} {
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(filepath.Join(storage2mdDir, name, "output.md"))
			if err != nil {
				t.Fatalf("reading golden: %v", err)
			}
			md := frontmatter.Parse("main.md", string(src))
			page, err := convert.MdToConfluence(md, "https://wiki.example.net", "ENG", "vtest")
			if err != nil {
				t.Fatalf("MdToConfluence: %v", err)
			}
			back, err := convert.StorageToMarkdown(page.HTML, nil)
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			if back != string(src) {
				t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", back, src)
			}
		})
	}
}

// TestRoundTripEncodedImageSources closes the loop between the two halves of the
// image-source codec, on real emitted storage rather than a hand-written
// fragment: every destination `read`/`export` writes must decode back to exactly
// the path `update` published from. If it did not, exporting a page would rename
// its own image files, and re-publishing the export would upload them again under
// new attachment names.
func TestRoundTripEncodedImageSources(t *testing.T) {
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
	md, err := convert.StorageToMarkdown(page.HTML, sources)
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

// TestStorageToMarkdownPrefersRecordedSource checks the two ways an image path is
// recovered. The path recorded on the attachment wins because it is exact; with
// no record, the attachment name is decoded, which is equally exact for a name
// markfluence produced.
func TestStorageToMarkdownPrefersRecordedSource(t *testing.T) {
	const in = `<p><ac:image ac:alt="d"><ri:attachment ri:filename="assets%2Fx.png" /></ac:image></p>`

	got, err := convert.StorageToMarkdown(in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "![d](assets/x.png)\n"; got != want {
		t.Errorf("decoded from name: got %q, want %q", got, want)
	}

	// A recorded source overrides the name -- this is what makes an attachment
	// whose name cannot be decoded faithfully (one a human uploaded, or one from
	// an older markfluence) still resolve to the right path. The space in the
	// path is encoded on the way out: a destination is a URL, and a bare space
	// would end it, leaving markdown that is not an image at all.
	sources := map[string]string{"assets%2Fx.png": "images/original name.png"}
	got, err = convert.StorageToMarkdown(in, sources)
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
	got, err := convert.StorageToMarkdown(in, map[string]string{"%2Fetc%2Fpasswd.png": "/etc/passwd.png"})
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
