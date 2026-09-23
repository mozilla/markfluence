package client

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// full is a complete set of credentials in env-file form.
const full = "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"

// clearConfluenceEnv unsets the CONFLUENCE_* vars for a test. TestMain already
// does this for the package; a test calls it again only to undo its own Setenv.
func clearConfluenceEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{URLVar, UsernameVar, TokenVar, CloudIDVar} {
		t.Setenv(k, "")
	}
}

func writeEnvFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "custom.env")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// withCredentialsFile points XDG_CONFIG_HOME at a fresh directory and writes
// body as the credentials file there, returning its path.
func withCredentialsFile(t *testing.T, body string) string {
	t.Helper()
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "markfluence", "credentials")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestResolveUsesExplicitEnvFile(t *testing.T) {
	c, err := Resolve(writeEnvFile(t, full))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://wiki" {
		t.Errorf("baseURL = %q, want https://wiki", c.BaseURL())
	}
}

func TestResolveMissingExplicitEnvFileErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.env")
	if _, err := Resolve(missing); err == nil {
		t.Fatal("Resolve: want error for a missing --env-file path")
	}
}

func TestResolveUsesTheCredentialsFile(t *testing.T) {
	withCredentialsFile(t, full)
	c, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.SiteURL() != "https://wiki" {
		t.Errorf("SiteURL = %q, want https://wiki from the credentials file", c.SiteURL())
	}
}

// TestResolvePrecedence: --env-file beats the environment, and the environment
// beats the credentials file, one key at a time. The username is the key under
// test, since it is the one that may legitimately come from anywhere.
func TestResolvePrecedence(t *testing.T) {
	withCredentialsFile(t, "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=from-creds\nCONFLUENCE_TOKEN=secret\n")
	if c, err := Resolve(""); err != nil || c.username != "from-creds" {
		t.Fatalf("credentials file alone: username = %v, %v", c, err)
	}
	t.Setenv(UsernameVar, "from-env")
	if c, err := Resolve(""); err != nil || c.username != "from-env" {
		t.Errorf("environment should beat the credentials file: %v, %v", c, err)
	}
	path := writeEnvFile(t, "CONFLUENCE_USERNAME=from-file\n")
	if c, err := Resolve(path); err != nil || c.username != "from-file" {
		t.Errorf("--env-file should beat the environment: %v, %v", c, err)
	}
}

func TestCredentialsPath(t *testing.T) {
	home := t.TempDir()
	tests := []struct {
		name, xdg, home, want string
	}{
		{"absolute XDG_CONFIG_HOME", "/cfg", home, "/cfg/markfluence/credentials"},
		{"relative XDG_CONFIG_HOME is ignored", "cfg", home, filepath.Join(home, ".config/markfluence/credentials")},
		{"unset XDG_CONFIG_HOME", "", home, filepath.Join(home, ".config/markfluence/credentials")},
		{"relative HOME means no file", "", "home", ""},
		{"empty HOME means no file", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", tt.xdg)
			t.Setenv("HOME", tt.home)
			if got := CredentialsPath(); got != tt.want {
				t.Errorf("CredentialsPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveWithNoHomeIsNotAnError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	if _, err := Resolve(writeEnvFile(t, full)); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
}

// TestResolveMissingCredentialsFileIsFine: no file, and a path that runs
// through a regular file, both mean the user has not made one.
func TestResolveMissingCredentialsFileIsFine(t *testing.T) {
	// No username anywhere, so the credentials file is consulted.
	t.Setenv(URLVar, "https://wiki")
	t.Setenv(TokenVar, "secret")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty: no markfluence/ in it
	if _, err := Resolve(""); err == nil || !strings.Contains(err.Error(), "missing Confluence username") {
		t.Errorf("no credentials file: err = %v, want only the missing-username error", err)
	}

	notADir := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADir, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", notADir)
	if _, err := Resolve(""); err == nil || !strings.Contains(err.Error(), "missing Confluence username") {
		t.Errorf("ENOTDIR: err = %v, want only the missing-username error", err)
	}
}

func TestResolveUnreadableCredentialsFileFails(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "markfluence", "credentials")
	if err := os.MkdirAll(path, 0o700); err != nil { // a directory where the file goes
		t.Fatal(err)
	}
	_, err := Resolve("")
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Errorf("err = %v, want an error naming %s", err, path)
	}
}

// TestResolveSkipsTheCredentialsFileWhenComplete: a broken or loose file must
// not fail or warn on a run that never uses it.
func TestResolveSkipsTheCredentialsFileWhenComplete(t *testing.T) {
	t.Setenv(URLVar, "https://wiki")
	t.Setenv(UsernameVar, "bot")
	t.Setenv(TokenVar, "secret")

	got := captureSecurityWarnings(t)
	path := withCredentialsFile(t, "CONFLUENCE_TOKEN=secret\n")
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(""); err != nil {
		t.Fatalf("loose file: Resolve: %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("warnings = %v, want none: the file was never needed", *got)
	}

	// A directory where the file goes fails if it is read; it must not be.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(""); err != nil {
		t.Errorf("unreadable file: Resolve: %v, want the file never read", err)
	}
}

func TestResolveURLAndTokenMustShareASource(t *testing.T) {
	credPath := withCredentialsFile(t, full)

	t.Setenv(URLVar, "https://elsewhere")
	_, err := Resolve("")
	if err == nil {
		t.Fatal("URL from the environment, token from the credentials file: want an error")
	}
	for _, want := range []string{"CONFLUENCE_URL comes from the environment",
		"CONFLUENCE_TOKEN comes from " + displayPath(credPath), "Set both in the same place", CredentialsDoc} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}

	clearConfluenceEnv(t)
	t.Setenv(TokenVar, "other")
	envFile := writeEnvFile(t, "CONFLUENCE_URL=https://b\nCONFLUENCE_USERNAME=bot\n")
	if _, err := Resolve(envFile); err == nil ||
		!strings.Contains(err.Error(), "CONFLUENCE_URL comes from --env-file "+envFile) {
		t.Errorf("URL from --env-file, token from the environment: err = %v", err)
	}

	// An empty key in a higher source is unset, and is not blamed.
	envFile = writeEnvFile(t, "CONFLUENCE_URL=https://b\nCONFLUENCE_TOKEN=\n")
	if _, err := Resolve(envFile); err == nil ||
		!strings.Contains(err.Error(), "CONFLUENCE_TOKEN comes from the environment") {
		t.Errorf("empty token in --env-file: err = %v, want the environment named", err)
	}

	// Both from one place, with the username from another, is fine.
	clearConfluenceEnv(t)
	t.Setenv(UsernameVar, "someone")
	if _, err := Resolve(writeEnvFile(t, "CONFLUENCE_URL=https://b\nCONFLUENCE_TOKEN=t\n")); err != nil {
		t.Errorf("URL and token from --env-file, username from the environment: %v", err)
	}
}

// TestResolveCloudIDFollowsTheURL: the cloud ID names one site, so it is read
// only from the source that supplied the URL.
func TestResolveCloudIDFollowsTheURL(t *testing.T) {
	withCredentialsFile(t, full+"CONFLUENCE_CLOUD_ID=instance-a\n")

	c, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := gatewayPrefix + "instance-a"; c.BaseURL() != want {
		t.Errorf("URL and cloud ID from the credentials file: BaseURL = %q, want %q", c.BaseURL(), want)
	}

	c, err = Resolve(writeEnvFile(t, "CONFLUENCE_URL=https://b\nCONFLUENCE_TOKEN=t\n"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://b" {
		t.Errorf("URL from --env-file: BaseURL = %q, want https://b and no cloud ID from the credentials file", c.BaseURL())
	}

	// URL and token from the environment; the username is left unset so the
	// credentials file is read, and its cloud ID must still be ignored.
	t.Setenv(URLVar, "https://c")
	t.Setenv(TokenVar, "t")
	c, err = Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if c.BaseURL() != "https://c" {
		t.Errorf("URL from the environment: BaseURL = %q, "+
			"want https://c and no cloud ID from the credentials file", c.BaseURL())
	}
	clearConfluenceEnv(t)

	t.Setenv(CloudIDVar, "from-env")
	c, err = Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := gatewayPrefix + "instance-a"; c.BaseURL() != want {
		t.Errorf("cloud ID in the environment, URL in the credentials file: BaseURL = %q, want %q", c.BaseURL(), want)
	}
}

func TestResolveRejectsURLishCloudID(t *testing.T) {
	// Pasting a whole gateway URL (or any path fragment) is the likely mistake;
	// it must fail with a usable message rather than a 404 at request time.
	for _, bad := range []string{
		"https://api.atlassian.com/ex/confluence/abc",
		"ex/confluence/abc",
		"abc/wiki",
	} {
		_, err := Resolve(writeEnvFile(t, full+"CONFLUENCE_CLOUD_ID="+bad+"\n"))
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
	c, err := Resolve(writeEnvFile(t, "CONFLUENCE_URL=https://wiki/\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// No cloud ID: both bases are the site, exactly as before the gateway existed.
	if c.BaseURL() != "https://wiki" || c.SiteURL() != "https://wiki" {
		t.Errorf("BaseURL/SiteURL = %q/%q, want https://wiki for both", c.BaseURL(), c.SiteURL())
	}
}

// TestResolveMissingNamesEverySource: someone whose settings are not found
// learns from the error where to put them, and that they go in one place.
func TestResolveMissingNamesEverySource(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	_, err := Resolve("")
	if err == nil {
		t.Fatal("want a missing-settings error")
	}
	for _, want := range []string{"missing Confluence URL (CONFLUENCE_URL)", "token (CONFLUENCE_TOKEN)",
		"run markfluence credentials-init, or set them in one place", "the environment",
		filepath.Join(cfg, "markfluence", "credentials"),
		"--env-file", CredentialsDoc} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

// TestResolveShowsTheCredentialsFileUnderHome: errors name a file under the
// home directory as ~/..., the form the docs use.
func TestResolveShowsTheCredentialsFileUnderHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	const shown = "~/.config/markfluence/credentials"

	if _, err := Resolve(""); err == nil || !strings.Contains(err.Error(), "the environment, "+shown+",") {
		t.Errorf("no file: err = %v, want it to name %s", err, shown)
	}

	path := filepath.Join(home, ".config", "markfluence", "credentials")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(URLVar, "https://elsewhere")
	if _, err := Resolve(""); err == nil || !strings.Contains(err.Error(), "CONFLUENCE_TOKEN comes from "+shown+".") {
		t.Errorf("same-source: err = %v, want it to name %s", err, shown)
	}
}

// TestResolveMissingWithNoHome: with no credentials file possible, the error
// offers only the places that exist, and one missing setting is "it".
func TestResolveMissingWithNoHome(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv(URLVar, "https://wiki")
	t.Setenv(TokenVar, "secret")
	_, err := Resolve("")
	want := "missing Confluence username (CONFLUENCE_USERNAME): " +
		"set it in the environment, or a file named by --env-file. See " + CredentialsDoc
	if err == nil || err.Error() != want {
		t.Errorf("err = %v, want %q", err, want)
	}
}

// TestResolveMissingHalfOfThePair: when the URL or the token is set, the
// other can only go in the same place, so that is the only place offered.
func TestResolveMissingHalfOfThePair(t *testing.T) {
	withCredentialsFile(t, "CONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n")
	_, err := Resolve("")
	if err == nil || !strings.Contains(err.Error(), "set it in ") ||
		!strings.Contains(err.Error(), "markfluence/credentials, where CONFLUENCE_TOKEN is.") ||
		strings.Contains(err.Error(), "the environment") || strings.Contains(err.Error(), "credentials-init") {
		t.Errorf("token in the credentials file: err = %v, want only its place offered for the URL", err)
	}

	t.Setenv(URLVar, "https://wiki")
	t.Setenv(UsernameVar, "bot")
	if err := os.WriteFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "markfluence", "credentials"),
		nil, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Resolve("")
	if want := "set it in the environment, where CONFLUENCE_URL is."; err == nil || !strings.Contains(err.Error(), want) ||
		strings.Contains(err.Error(), "credentials-init") {
		t.Errorf("URL in the environment: err = %v, want %q and no credentials-init", err, want)
	}
}

// TestResolveWarnsAboutAnIgnoredCloudIDAboveTheURL: a cloud ID set on purpose
// in a higher place than the URL is reported; one in a lower place is the
// ordinary case of a credentials file for another instance, and is not.
func TestResolveWarnsAboutAnIgnoredCloudIDAboveTheURL(t *testing.T) {
	got := captureSecurityWarnings(t)
	withCredentialsFile(t, full)
	t.Setenv(CloudIDVar, "exported")
	if _, err := Resolve(""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(*got) != 1 || !strings.Contains((*got)[0], "CONFLUENCE_CLOUD_ID from the environment is ignored") {
		t.Errorf("warnings = %v, want one about the ignored cloud ID", *got)
	}

	*got = nil
	clearConfluenceEnv(t)
	withCredentialsFile(t, full+"CONFLUENCE_CLOUD_ID=instance-a\n")
	if _, err := Resolve(writeEnvFile(t, "CONFLUENCE_URL=https://b\nCONFLUENCE_TOKEN=t\n")); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(*got) != 0 {
		t.Errorf("warnings = %v, want none for a cloud ID below the URL's place", *got)
	}
}

// TestResolveDanglingCredentialsLinkFails: a symbolic link to nothing is a
// file the user made, not a missing one.
func TestResolveDanglingCredentialsLinkFails(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	path := filepath.Join(cfg, "markfluence", "credentials")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(cfg, "gone"), path); err != nil {
		t.Fatal(err)
	}
	_, err := Resolve("")
	if err == nil || !strings.Contains(err.Error(), "symbolic link to a file that does not exist") {
		t.Errorf("err = %v, want the dangling-link error", err)
	}
}

// TestResolveReadsNoDotenv: a .env in the working directory, or at the root
// of the project the working directory is in, is never read (#188). A checkout
// must not be able to supply credentials.
func TestResolveReadsNoDotenv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markfluence.yaml"), []byte("# marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".env"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "docs")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, ".env"), []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{root, sub} {
		t.Chdir(dir)
		if _, err := Resolve(""); err == nil || !strings.Contains(err.Error(), "missing Confluence") {
			t.Errorf("in %s: err = %v, want the missing-settings error", dir, err)
		}
	}
}

// TestResolveIgnoresAMalformedProjectFile: credential resolution no longer
// discovers a root, so a project file it cannot parse is not its business.
// Commands that read the project file report it themselves.
func TestResolveIgnoresAMalformedProjectFile(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "markfluence.yaml"), []byte("spce: ENG\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	// Not through --env-file: that skipped discovery even before #188.
	withCredentialsFile(t, full)
	if _, err := Resolve(""); err != nil {
		t.Errorf("Resolve: %v", err)
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

// TestWarnLoosePermissionsIgnoresAPipe: `--env-file <(pass show …)` reads a
// pipe, whose mode no chmod can fix.
func TestWarnLoosePermissionsIgnoresAPipe(t *testing.T) {
	got := captureSecurityWarnings(t)
	fifo := filepath.Join(t.TempDir(), "fifo")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	warnLoosePermissions(fifo, map[string]string{TokenVar: "secret"})
	if len(*got) != 0 {
		t.Errorf("warnings = %v, want none for a pipe", *got)
	}
}

// TestResolveWarnsThroughTheCredentialsFile exercises the real path a command
// takes -- Resolve, not loadDotenv -- so the check cannot be wired only to the
// explicit --env-file branch.
func TestResolveWarnsThroughTheCredentialsFile(t *testing.T) {
	got := captureSecurityWarnings(t)
	path := withCredentialsFile(t, full)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(""); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(*got) != 1 {
		t.Errorf("warnings = %v, want exactly one", *got)
	}
}
