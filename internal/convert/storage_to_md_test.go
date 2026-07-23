package convert_test

import (
	"encoding/json"
	"os"
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
			md, err := convert.StorageToMarkdown(string(in))
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
			if _, err := convert.StorageToMarkdown(page.HTML); err != nil {
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
	got, err := convert.StorageToMarkdown(page.HTML)
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
	got, err := convert.StorageToMarkdown(in)
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
	for _, name := range []string{"layout", "unknown-macros"} {
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
			back, err := convert.StorageToMarkdown(page.HTML)
			if err != nil {
				t.Fatalf("StorageToMarkdown: %v", err)
			}
			if back != string(src) {
				t.Errorf("round-trip mismatch\n--- got ---\n%s\n--- want ---\n%s", back, src)
			}
		})
	}
}
