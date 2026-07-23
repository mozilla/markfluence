package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// clearConfluenceEnv unsets the CONFLUENCE_* vars for a test so .env / flags are
// the only sources.
func clearConfluenceEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{urlEnv, usernameEnv, tokenEnv} {
		t.Setenv(k, "")
	}
}

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveUsesExplicitEnvFile(t *testing.T) {
	clearConfluenceEnv(t)
	path := writeEnvFile(t, "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n")
	c, err := Resolve("", "", path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://wiki" {
		t.Errorf("baseURL = %q, want https://wiki", c.BaseURL())
	}
}

func TestResolveFlagOverridesEnvFile(t *testing.T) {
	clearConfluenceEnv(t)
	path := writeEnvFile(t, "CONFLUENCE_URL=https://from-file\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n")
	c, err := Resolve("https://from-flag", "", path)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://from-flag" {
		t.Errorf("baseURL = %q, want the flag value", c.BaseURL())
	}
}

func TestResolveMissingExplicitEnvFileErrors(t *testing.T) {
	clearConfluenceEnv(t)
	missing := filepath.Join(t.TempDir(), "nope.env")
	if _, err := Resolve("", "", missing); err == nil {
		t.Fatal("Resolve: want error for a missing --env-file path")
	}
}

func TestResolveDefaultEnvFileMissingIsFine(t *testing.T) {
	clearConfluenceEnv(t)
	// No ./.env in this temp cwd, and no explicit env file: the missing default
	// is tolerated, so we fail only on missing config values (not a read error).
	t.Chdir(t.TempDir())
	_, err := Resolve("", "", "")
	if err == nil {
		t.Fatal("want a missing-config error")
	}
	// It should be the missing-values error, not a file-read error.
	if !strings.Contains(err.Error(), "missing Confluence") {
		t.Errorf("error = %q, want a missing-Confluence-config error", err)
	}
}
