package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/project"
)

// clearConfluenceEnv unsets the CONFLUENCE_* vars for a test so .env / flags are
// the only sources.
func clearConfluenceEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{urlEnv, usernameEnv, tokenEnv, cloudIDEnv} {
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
	c, err := Resolve(Options{EnvFile: path})
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
	c, err := Resolve(Options{URL: "https://from-flag", EnvFile: path})
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
	if _, err := Resolve(Options{EnvFile: missing}); err == nil {
		t.Fatal("Resolve: want error for a missing --env-file path")
	}
}

func TestResolveDefaultEnvFileMissingIsFine(t *testing.T) {
	clearConfluenceEnv(t)
	// No ./.env in this temp cwd, and no explicit env file: the missing default
	// is tolerated, so we fail only on missing config values (not a read error).
	t.Chdir(t.TempDir())
	_, err := Resolve(Options{})
	if err == nil {
		t.Fatal("want a missing-config error")
	}
	// It should be the missing-values error, not a file-read error.
	if !strings.Contains(err.Error(), "missing Confluence") {
		t.Errorf("error = %q, want a missing-Confluence-config error", err)
	}
}

func TestResolveDefaultEnvFileFoundInCwdWithNoProjectFile(t *testing.T) {
	clearConfluenceEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".env"),
		[]byte("CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// No markfluence.yaml anywhere above dir, so discovery falls back to dir
	// itself -- today's behavior, preserved.
	t.Chdir(dir)

	c, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://wiki" {
		t.Errorf("baseURL = %q, want https://wiki", c.BaseURL())
	}
}

func TestResolveDefaultEnvFileFoundAtDiscoveredProjectRoot(t *testing.T) {
	clearConfluenceEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markfluence.yaml"), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"),
		[]byte("CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "docs", "team")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// No .env in the working directory itself -- only at the project root
	// discovery finds by walking up.
	t.Chdir(sub)

	c, err := Resolve(Options{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://wiki" {
		t.Errorf("baseURL = %q, want https://wiki from the project root's .env", c.BaseURL())
	}
}

// TestResolveRootsOverridesEnvDiscovery covers Options.Roots: when the caller
// passes its own --root-backed project.Cache, .env is read from that root, not
// from a plain upward walk from the working directory -- so a --root pointed
// at a different project also redirects which .env create/update/
// attachment-upload read, matching the flag's stated meaning of overriding
// discovery for the whole invocation.
func TestResolveRootsOverridesEnvDiscovery(t *testing.T) {
	clearConfluenceEnv(t)
	cwd := t.TempDir() // no .env here
	override := t.TempDir()
	if err := os.WriteFile(filepath.Join(override, ".env"),
		[]byte("CONFLUENCE_URL=https://from-root\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(cwd)

	roots := project.NewCache(override)
	defer roots.Close()
	c, err := Resolve(Options{Roots: roots})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://from-root" {
		t.Errorf("baseURL = %q, want https://from-root from --root's .env", c.BaseURL())
	}
}

func TestResolveCloudIDPrecedence(t *testing.T) {
	clearConfluenceEnv(t)
	path := writeEnvFile(t,
		"CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"+
			"CONFLUENCE_CLOUD_ID=from-file\n")

	// From .env: requests move to the gateway, the site is untouched.
	c, err := Resolve(Options{EnvFile: path})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := gatewayPrefix + "from-file"; c.BaseURL() != want {
		t.Errorf("BaseURL = %q, want %q", c.BaseURL(), want)
	}
	if c.SiteURL() != "https://wiki" {
		t.Errorf("SiteURL = %q, want https://wiki", c.SiteURL())
	}

	// Env beats .env; flag beats env.
	t.Setenv(cloudIDEnv, "from-env")
	if c, _ := Resolve(Options{EnvFile: path}); c.BaseURL() != gatewayPrefix+"from-env" {
		t.Errorf("env should beat .env, got %q", c.BaseURL())
	}
	if c, _ := Resolve(Options{CloudID: "from-flag", EnvFile: path}); c.BaseURL() != gatewayPrefix+"from-flag" {
		t.Errorf("flag should win, got %q", c.BaseURL())
	}
}

func TestResolveRejectsURLishCloudID(t *testing.T) {
	clearConfluenceEnv(t)
	path := writeEnvFile(t, "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n")

	// Pasting a whole gateway URL (or any path fragment) is the likely mistake;
	// it must fail with a usable message rather than a 404 at request time.
	for _, bad := range []string{
		"https://api.atlassian.com/ex/confluence/abc",
		"ex/confluence/abc",
		"abc/wiki",
	} {
		_, err := Resolve(Options{CloudID: bad, EnvFile: path})
		if err == nil {
			t.Errorf("Resolve(cloud ID %q): want an error", bad)
			continue
		}
		if !strings.Contains(err.Error(), "invalid Confluence cloud ID") {
			t.Errorf("Resolve(cloud ID %q) error = %q, want the invalid-cloud-ID message", bad, err)
		}
	}
}

func TestResolveWithoutCloudIDKeepsSiteURL(t *testing.T) {
	clearConfluenceEnv(t)
	path := writeEnvFile(t, "CONFLUENCE_URL=https://wiki/\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n")
	c, err := Resolve(Options{EnvFile: path})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// No cloud ID: both bases are the site, exactly as before the gateway existed.
	if c.BaseURL() != "https://wiki" || c.SiteURL() != "https://wiki" {
		t.Errorf("BaseURL/SiteURL = %q/%q, want https://wiki for both", c.BaseURL(), c.SiteURL())
	}
}
