package convert_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
)

// update, when set (`go test ./internal/convert -run TestRegression -update`,
// wrapped by `make regen-regressions`), rewrites each case's test.output golden.
var update = flag.Bool("update", false, "regenerate regression goldens")

const regressionDir = "testdata/regression"

// TestRegression runs every case directory under testdata/regression through
// MdToConfluence and exact-matches the normalized result against its test.output
// golden.
func TestRegression(t *testing.T) {
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
			caseDir := filepath.Join(regressionDir, name)
			got := runCase(t, caseDir)
			goldenPath := filepath.Join(caseDir, "test.output")

			if *update {
				if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
					t.Fatalf("writing golden: %v", err)
				}
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden (run `make regen-regressions`): %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden mismatch for %q; review and run `make regen-regressions`\n--- got ---\n%s", name, got)
			}
		})
	}
}

// caseConfig is the resolved test.input for a case.
type caseConfig struct {
	filename string
	baseURL  string
	spaceKey string
	files    []string
}

// runCase resolves a case's config, runs the converter, and returns the golden bytes.
func runCase(t *testing.T, caseDir string) []byte {
	cfg := loadConfig(t, caseDir)
	md, err := frontmatter.ParseFile(filepath.Join(caseDir, cfg.filename))
	if err != nil {
		t.Fatalf("parsing primary file: %v", err)
	}
	// A fixed version stamp keeps goldens deterministic; no case uses the token.
	page, err := convert.MdToConfluence(md, cfg.baseURL, cfg.spaceKey, "markfluence vtest")
	if err != nil {
		t.Fatalf("MdToConfluence: %v", err)
	}
	redactAttachmentPaths(t, page, caseDir)
	return marshalGolden(t, page)
}

// loadConfig reads test.input (if present), applies defaults, and enforces the
// fixture-file manifest. Defaults: main.md / https://wiki.example.net / ENG.
func loadConfig(t *testing.T, caseDir string) caseConfig {
	cfg := caseConfig{filename: "main.md", baseURL: "https://wiki.example.net", spaceKey: "ENG"}

	if data, err := os.ReadFile(filepath.Join(caseDir, "test.input")); err == nil {
		raw := map[string]json.RawMessage{}
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("parsing test.input: %v", err)
		}
		mustUnmarshal(t, raw, "filename", &cfg.filename)
		mustUnmarshal(t, raw, "base_url", &cfg.baseURL)
		if v, ok := raw["space_key"]; ok {
			// Present but possibly JSON null (no resolvable space key).
			var s *string
			if err := json.Unmarshal(v, &s); err != nil {
				t.Fatalf("parsing space_key: %v", err)
			}
			if s == nil {
				cfg.spaceKey = ""
			} else {
				cfg.spaceKey = *s
			}
		}
		mustUnmarshal(t, raw, "files", &cfg.files)
	}
	if cfg.files == nil {
		cfg.files = []string{cfg.filename}
	}
	enforceManifest(t, caseDir, cfg)
	return cfg
}

func mustUnmarshal(t *testing.T, raw map[string]json.RawMessage, key string, dst any) {
	if v, ok := raw[key]; ok {
		if err := json.Unmarshal(v, dst); err != nil {
			t.Fatalf("parsing %s: %v", key, err)
		}
	}
}

// enforceManifest asserts the case directory's fixture files exactly match the
// declared files, and that the primary filename is among them.
func enforceManifest(t *testing.T, caseDir string, cfg caseConfig) {
	actual := map[string]bool{}
	err := filepath.WalkDir(caseDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != caseDir && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "__")) {
				return fs.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(caseDir, path)
		rel = filepath.ToSlash(rel)
		if rel != "test.input" && rel != "test.output" {
			actual[rel] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scanning fixtures: %v", err)
	}

	declared := map[string]bool{}
	for _, f := range cfg.files {
		declared[f] = true
		if !actual[f] {
			t.Fatalf("case %q: declared file %q is missing on disk", caseDir, f)
		}
	}
	for f := range actual {
		if !declared[f] {
			t.Fatalf("case %q: stray file %q not listed in test.input files", caseDir, f)
		}
	}
	if !declared[cfg.filename] {
		t.Fatalf("case %q: primary filename %q not listed in test.input files", caseDir, cfg.filename)
	}
}

// redactAttachmentPaths replaces the case-directory prefix of each attachment
// path with <ROOT> so goldens don't depend on where the repository lives.
func redactAttachmentPaths(t *testing.T, page *convert.ConfluencePage, caseDir string) {
	root, err := filepath.Abs(caseDir)
	if err != nil {
		t.Fatalf("resolving case dir: %v", err)
	}
	for i := range page.Attachments {
		abs, err := filepath.Abs(page.Attachments[i].Path)
		if err != nil {
			continue
		}
		if abs == root || strings.HasPrefix(abs, root+string(os.PathSeparator)) {
			page.Attachments[i].Path = "<ROOT>" + filepath.ToSlash(abs[len(root):])
		}
	}
}

// marshalGolden serializes a page to the golden's on-disk form: indented JSON
// with HTML characters left literal and a trailing newline.
func marshalGolden(t *testing.T, page *convert.ConfluencePage) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(page); err != nil {
		t.Fatalf("marshaling golden: %v", err)
	}
	return buf.Bytes()
}
