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
	c, err := Resolve(ResolveOptions{EnvFile: path})
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
	c, err := Resolve(ResolveOptions{URL: "https://from-flag", EnvFile: path})
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
	if _, err := Resolve(ResolveOptions{EnvFile: missing}); err == nil {
		t.Fatal("Resolve: want error for a missing --env-file path")
	}
}

func TestResolveDefaultEnvFileMissingIsFine(t *testing.T) {
	clearConfluenceEnv(t)
	// No ./.env in this temp cwd, and no explicit env file: the missing default
	// is tolerated, so we fail only on missing config values (not a read error).
	t.Chdir(t.TempDir())
	_, err := Resolve(ResolveOptions{})
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

	c, err := Resolve(ResolveOptions{})
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

	c, err := Resolve(ResolveOptions{})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://wiki" {
		t.Errorf("baseURL = %q, want https://wiki from the project root's .env", c.BaseURL())
	}
}

// TestResolveRootsOverridesEnvDiscovery covers ResolveOptions.Roots: when the caller
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
	c, err := Resolve(ResolveOptions{Roots: roots})
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
	c, err := Resolve(ResolveOptions{EnvFile: path})
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
	if c, _ := Resolve(ResolveOptions{EnvFile: path}); c.BaseURL() != gatewayPrefix+"from-env" {
		t.Errorf("env should beat .env, got %q", c.BaseURL())
	}
	if c, _ := Resolve(ResolveOptions{CloudID: "from-flag", EnvFile: path}); c.BaseURL() != gatewayPrefix+"from-flag" {
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
		_, err := Resolve(ResolveOptions{CloudID: bad, EnvFile: path})
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
	c, err := Resolve(ResolveOptions{EnvFile: path})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// No cloud ID: both bases are the site, exactly as before the gateway existed.
	if c.BaseURL() != "https://wiki" || c.SiteURL() != "https://wiki" {
		t.Errorf("BaseURL/SiteURL = %q/%q, want https://wiki for both", c.BaseURL(), c.SiteURL())
	}
}

func TestSpaceKeyFromWebUI(t *testing.T) {
	if got := SpaceKeyFromWebUI("/spaces/ENG/pages/123/Title"); got != "ENG" {
		t.Errorf("space = %q, want ENG", got)
	}
	if got := SpaceKeyFromWebUI("/spaces/ENG/folder/123"); got != "ENG" {
		t.Errorf("space (folder) = %q, want ENG", got)
	}
	if got := SpaceKeyFromWebUI("/wiki/pages/viewpage.action?pageId=123"); got != "" {
		t.Errorf("space = %q, want empty for a webui link with no /spaces/ prefix", got)
	}
	if got := SpaceKeyFromWebUI(""); got != "" {
		t.Errorf("space = %q, want empty for an empty webui", got)
	}
}

// captureSecurityWarnings installs a recording warner for the duration of a
// test, restoring whatever was there before -- the hook is package-level, so a
// test that leaks one changes the next test's behavior.
func captureSecurityWarnings(t *testing.T) *[]string {
	t.Helper()
	var got []string
	prev := securityWarner
	securityWarner = func(msg string) { got = append(got, msg) }
	t.Cleanup(func() { securityWarner = prev })
	return &got
}

// TestWarnLoosePermissions covers both halves of the rule: the mode, and the
// token gate that keeps the warning off files with no secret in them.
func TestWarnLoosePermissions(t *testing.T) {
	const withToken = "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"
	const noToken = "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\n"

	tests := []struct {
		name string
		body string
		mode os.FileMode
		want bool
	}{
		{"world-readable with a token", withToken, 0o644, true},
		{"group-readable with a token", withToken, 0o640, true},
		{"world-writable with a token", withToken, 0o622, true},
		{"wide open with a token", withToken, 0o666, true},
		{"owner-only", withToken, 0o600, false},
		// More restrictive than required, not less: warning here would be
		// nonsense.
		{"owner read-only", withToken, 0o400, false},
		// The user execute bit is odd but leaks nothing.
		{"owner rwx", withToken, 0o700, false},
		// The gate: no secret in the file, so its mode is nobody's business.
		{"world-readable without a token", noToken, 0o644, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := captureSecurityWarnings(t)
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte(tt.body), tt.mode); err != nil {
				t.Fatal(err)
			}
			// WriteFile applies the umask, so set the mode explicitly.
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDotenv(path); err != nil {
				t.Fatalf("loadDotenv: %v", err)
			}
			if fired := len(*got) > 0; fired != tt.want {
				t.Errorf("warned = %v, want %v (%v)", fired, tt.want, *got)
			}
		})
	}
}

// TestWarnLoosePermissionsMessage pins what the reader is told: the path, the
// mode in the form chmod takes, why it matters, and the exact remedy.
func TestWarnLoosePermissionsMessage(t *testing.T) {
	got := captureSecurityWarnings(t)
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("CONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDotenv(path); err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("warnings = %v, want exactly one", *got)
	}
	for _, want := range []string{path, "mode 0644", "holds your API token", "chmod 600 " + path} {
		if !strings.Contains((*got)[0], want) {
			t.Errorf("message %q missing %q", (*got)[0], want)
		}
	}
}

// TestWarnLoosePermissionsNamesWhatIsWrong: the predicate is one rule, but the
// message is not, and a warning that says "readable" about a file nobody can
// read is one a reader checks and stops trusting.
func TestWarnLoosePermissionsNamesWhatIsWrong(t *testing.T) {
	tests := []struct {
		mode os.FileMode
		want string
	}{
		{0o644, "readable by others"},
		{0o640, "readable by others"},
		{0o622, "writable by others"},
		{0o611, "accessible to others"},
	}
	for _, tt := range tests {
		t.Run(tt.want+"/"+tt.mode.String(), func(t *testing.T) {
			got := captureSecurityWarnings(t)
			path := filepath.Join(t.TempDir(), ".env")
			if err := os.WriteFile(path, []byte("CONFLUENCE_TOKEN=secret\n"), tt.mode); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatal(err)
			}
			if _, err := loadDotenv(path); err != nil {
				t.Fatalf("loadDotenv: %v", err)
			}
			if len(*got) != 1 {
				t.Fatalf("warnings = %v, want exactly one", *got)
			}
			if !strings.Contains((*got)[0], tt.want) {
				t.Errorf("message %q, want it to say %q", (*got)[0], tt.want)
			}
		})
	}
}

// TestWarnLoosePermissionsRemedyIsPasteable: the chmod line is the point of the
// message, so a path a shell would mangle has to come out runnable.
func TestWarnLoosePermissionsRemedyIsPasteable(t *testing.T) {
	got := captureSecurityWarnings(t)
	dir := filepath.Join(t.TempDir(), "My Docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("CONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDotenv(path); err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}
	if len(*got) != 1 {
		t.Fatalf("warnings = %v, want exactly one", *got)
	}
	if !strings.Contains((*got)[0], "chmod 600 '"+path+"'") {
		t.Errorf("message %q, want a quoted chmod for a path with a space", (*got)[0])
	}
}

// TestWarnLoosePermissionsFollowsASymlink is why the check stats rather than
// lstats: a link's own mode is 0777 on every system that has them, so lstat
// would warn about a target that is perfectly safe.
func TestWarnLoosePermissionsFollowsASymlink(t *testing.T) {
	got := captureSecurityWarnings(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "real.env")
	if err := os.WriteFile(target, []byte("CONFLUENCE_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, ".env")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := loadDotenv(link); err != nil {
		t.Fatalf("loadDotenv: %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("warnings = %v, want none: the link points at a 0600 file", *got)
	}
}

// TestResolveWarnsThroughTheDiscoveredEnvFile exercises the real path a command
// takes -- Resolve, not loadDotenv -- so the check cannot be wired only to the
// explicit --env-file branch.
func TestResolveWarnsThroughTheDiscoveredEnvFile(t *testing.T) {
	clearConfluenceEnv(t)
	got := captureSecurityWarnings(t)
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	if _, err := Resolve(ResolveOptions{}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(*got) != 1 {
		t.Errorf("warnings = %v, want exactly one", *got)
	}
}

// A malformed markfluence.yaml used to be swallowed here, silently reading
// .env from the working directory instead of the project root. It matters most
// for a command with no per-file root of its own -- read, search, info -- which
// would otherwise never report the malformed file at all.
func TestResolveFailsOnAMalformedProjectFile(t *testing.T) {
	clearConfluenceEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markfluence.yaml"),
		[]byte("spce: ENG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"),
		[]byte("CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	if _, err := Resolve(ResolveOptions{}); err == nil {
		t.Fatal("Resolve succeeded with a malformed project file, want an error")
	} else if !strings.Contains(err.Error(), "unknown setting") {
		t.Errorf("error = %q, want it to name the unknown setting", err)
	}
}

// --env-file overrides discovery absolutely, which has to keep holding: an
// explicit path is how someone works around a project file they cannot fix.
func TestResolveEnvFileOverridesAMalformedProjectFile(t *testing.T) {
	clearConfluenceEnv(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markfluence.yaml"),
		[]byte("spce: ENG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	explicit := filepath.Join(root, "creds.env")
	if err := os.WriteFile(explicit,
		[]byte("CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)

	c, err := Resolve(ResolveOptions{EnvFile: explicit})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.SiteURL() != "https://wiki" {
		t.Errorf("SiteURL = %q, want https://wiki", c.SiteURL())
	}
}
