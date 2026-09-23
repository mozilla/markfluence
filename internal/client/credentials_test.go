package client

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// credentialsTarget is a credentials path under a fresh directory that does
// not exist yet, so the write has to create it.
func credentialsTarget(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "markfluence", "credentials")
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// noTempFiles fails if a write left its temporary file behind.
func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".credentials-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}

func TestWriteCredentialsModes(t *testing.T) {
	path := credentialsTarget(t)
	values := map[string]string{urlEnv: "https://wiki", usernameEnv: "bot", tokenEnv: "secret"}
	if err := WriteCredentials(path, values); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("file mode = %#o, want 0600", m)
	}
	if m := mode(t, filepath.Dir(path)); m != 0o700 {
		t.Errorf("new directory mode = %#o, want 0700", m)
	}
	noTempFiles(t, filepath.Dir(path))

	got, err := ReadCredentials(path)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range values {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if _, ok := got[cloudIDEnv]; ok {
		t.Errorf("an empty cloud ID was written: %v", got)
	}
}

// TestWriteCredentialsLeavesAnExistingDirectoryAlone: the directory's mode is
// the user's business once it exists.
func TestWriteCredentialsLeavesAnExistingDirectoryAlone(t *testing.T) {
	path := credentialsTarget(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentials(path, map[string]string{tokenEnv: "secret"}); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, filepath.Dir(path)); m != 0o755 {
		t.Errorf("existing directory mode = %#o, want 0755 unchanged", m)
	}
}

// TestWriteCredentialsTightensALooseFile: rewriting a 0644 file leaves it 0600,
// and says nothing about the old mode on the way.
func TestWriteCredentialsTightensALooseFile(t *testing.T) {
	warnings := captureSecurityWarnings(t)
	path := credentialsTarget(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(full), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredentials(path); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentials(path, map[string]string{tokenEnv: "new"}); err != nil {
		t.Fatal(err)
	}
	if m := mode(t, path); m != 0o600 {
		t.Errorf("mode = %#o, want 0600", m)
	}
	if len(*warnings) != 0 {
		t.Errorf("warnings = %q, want none from reading or writing", *warnings)
	}
}

// TestWriteCredentialsWritesThroughASymlink: a credentials file kept in a
// dotfiles repository and linked into place stays linked.
func TestWriteCredentialsWritesThroughASymlink(t *testing.T) {
	path := credentialsTarget(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "dotfiles-credentials")
	if err := os.WriteFile(real, []byte(full), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredentials(path, map[string]string{tokenEnv: "rotated"}); err != nil {
		t.Fatal(err)
	}
	if !isSymlink(path) {
		t.Fatal("the symbolic link was replaced by a file")
	}
	got, err := ReadCredentials(real)
	if err != nil {
		t.Fatal(err)
	}
	if got[tokenEnv] != "rotated" {
		t.Errorf("link target token = %q, want rotated", got[tokenEnv])
	}
	noTempFiles(t, filepath.Dir(path))
	noTempFiles(t, filepath.Dir(real))
}

// TestWriteCredentialsRoundTrips: every value reads back exactly, including the
// ones the parser would otherwise trim or unquote.
func TestWriteCredentialsRoundTrips(t *testing.T) {
	for _, v := range []string{
		"plain",
		" leading space",
		"trailing nbsp ",
		`"quoted"`,
		`'single'`,
		`"`,
		`"half`,
		"has#hash",
		"a=b=c",
		"export x",
		`back\slash`,
	} {
		path := credentialsTarget(t)
		if err := WriteCredentials(path, map[string]string{tokenEnv: v}); err != nil {
			t.Errorf("%q: %v", v, err)
			continue
		}
		got, err := ReadCredentials(path)
		if err != nil {
			t.Fatal(err)
		}
		if got[tokenEnv] != v {
			t.Errorf("wrote %q, read back %q", v, got[tokenEnv])
		}
	}
}

// TestWriteCredentialsRefusesALineBreak: a value the format cannot hold is an
// error, and the file already there is untouched.
func TestWriteCredentialsRefusesALineBreak(t *testing.T) {
	for _, v := range []string{"a\nb", "a\rb"} {
		path := credentialsTarget(t)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(full), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := WriteCredentials(path, map[string]string{tokenEnv: v}); err == nil {
			t.Errorf("%q: want an error", v)
		}
		if b, _ := os.ReadFile(path); string(b) != full {
			t.Errorf("%q: the existing file changed to %q", v, b)
		}
		noTempFiles(t, filepath.Dir(path))
	}
}

func TestWriteCredentialsRefusesAnUnknownKey(t *testing.T) {
	path := credentialsTarget(t)
	if err := WriteCredentials(path, map[string]string{"CONFLUENCE_URLL": "x"}); err == nil {
		t.Error("want an error for a key that is not a setting")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file was written: %v", err)
	}
}

func TestReadCredentialsMissingIsNil(t *testing.T) {
	got, err := ReadCredentials(credentialsTarget(t))
	if err != nil || got != nil {
		t.Errorf("ReadCredentials = %v, %v; want nil, nil", got, err)
	}
}

func TestFetchCloudID(t *testing.T) {
	var auth []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = append(auth, r.Header.Get("Authorization"))
		if r.URL.Path != "/_edge/tenant_info" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"cloudId":"d8febd08-5555-5555-5555-db37c2369ce5"}`))
	}))
	defer srv.Close()

	got, err := FetchCloudID(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "d8febd08-5555-5555-5555-db37c2369ce5" {
		t.Errorf("cloud ID = %q", got)
	}
	if len(auth) != 1 || auth[0] != "" {
		t.Errorf("Authorization headers = %q, want one request with none", auth)
	}
}

func TestFetchCloudIDFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{"not found", http.StatusNotFound, "no"},
		{"no cloudId", http.StatusOK, `{"other":"x"}`},
		{"not JSON", http.StatusOK, `<html>`},
		{"a URL for a cloud ID", http.StatusOK, `{"cloudId":"https://x/y"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()
			if id, err := FetchCloudID(srv.URL); err == nil || !FromRequest(err) {
				t.Errorf("FetchCloudID = %q, %v; want a request error", id, err)
			}
		})
	}
}

// TestFetchCloudIDUnreachable: no response at all is a request error with no
// status, which is what lets a caller tell "offline" from "refused".
func TestFetchCloudIDUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	_, err := FetchCloudID(u)
	var he *HTTPError
	if err == nil || !FromRequest(err) || errors.As(err, &he) {
		t.Errorf("err = %v, want a request error that is not an HTTPError", err)
	}
}

// TestHTTPErrorShapePredicates: each predicate answers for its own measured
// shape and no other, since credentials-init decides between refusing and
// asking on them.
func TestHTTPErrorShapePredicates(t *testing.T) {
	const (
		gw   = gatewayPrefix + "cloud-id/wiki/rest/api/user/current"
		site = "https://example.atlassian.net/wiki/rest/api/user/current"
	)
	tests := []struct {
		name                      string
		err                       HTTPError
		scope, siteAuth, rejected bool
	}{
		{"scope mismatch", HTTPError{StatusCode: 401, URL: gw,
			Body: `{"code":401,"message":"Unauthorized; scope does not match"}`}, true, false, false},
		{"site HTML 401", HTTPError{StatusCode: 401, URL: site,
			Body: `<!doctype html><title>HTTP Status 401</title>`}, false, true, false},
		{"gateway HTML 401", HTTPError{StatusCode: 401, URL: gw,
			Body: `<!doctype html><title>HTTP Status 401</title>`}, false, false, false},
		{"v1 rejected", HTTPError{StatusCode: 403, URL: site,
			Body: `{"message":"caller cannot access Confluence"}`}, false, false, true},
		{"plain JSON 401", HTTPError{StatusCode: 401, URL: site,
			Body: `{"code":401,"message":"Unauthorized"}`}, false, false, false},
	}
	for _, tt := range tests {
		if got := tt.err.ScopeMismatch(); got != tt.scope {
			t.Errorf("%s: ScopeMismatch = %v", tt.name, got)
		}
		if got := tt.err.SiteRejectedAuth(); got != tt.siteAuth {
			t.Errorf("%s: SiteRejectedAuth = %v", tt.name, got)
		}
		if got := tt.err.RejectedCredential(); got != tt.rejected {
			t.Errorf("%s: RejectedCredential = %v", tt.name, got)
		}
	}
}

// TestUnkeptLines: a hand-written file's comments and stray keys are counted,
// and a file WriteCredentials wrote has none.
func TestUnkeptLines(t *testing.T) {
	path := credentialsTarget(t)
	if n, err := UnkeptLines(path); n != 0 || err != nil {
		t.Errorf("missing file: %d, %v; want 0, nil", n, err)
	}
	if err := WriteCredentials(path, map[string]string{urlEnv: "https://wiki", tokenEnv: "s"}); err != nil {
		t.Fatal(err)
	}
	if n, err := UnkeptLines(path); n != 0 || err != nil {
		t.Errorf("written file: %d, %v; want 0, nil", n, err)
	}
	body := "# my token, rotated in May\nexport CONFLUENCE_URL=https://wiki\n\nOTHER=1\nCONFLUENCE_TOKEN=s\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, err := UnkeptLines(path); n != 2 || err != nil {
		t.Errorf("hand-written file: %d, %v; want 2, nil", n, err)
	}
}
