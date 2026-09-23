package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// credentialKeys are the settings the credentials file holds, in the order
// WriteCredentials writes them.
var credentialKeys = []string{urlEnv, usernameEnv, tokenEnv, cloudIDEnv}

// credentialsHeader opens a file WriteCredentials writes. One line, so
// UnkeptLines can recognize it and a rewrite of a written file is quiet.
const credentialsHeader = "# Written by markfluence credentials-init. See " + credentialsDoc + "\n"

// CredentialsPath is where the user's credentials file lives:
// $XDG_CONFIG_HOME/markfluence/credentials, else
// $HOME/.config/markfluence/credentials, on every platform. os.UserConfigDir is
// not used because on macOS it answers ~/Library/Application Support, which
// command-line users do not look in.
//
// A relative XDG_CONFIG_HOME is ignored, as the XDG spec says, and a relative
// or empty HOME means there is no credentials file ("" here). Either would
// make the file depend on the working directory, which is exactly what #188
// removed.
func CredentialsPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "markfluence", "credentials")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".config", "markfluence", "credentials")
}

// ReadCredentials reads the credentials file at path with the rules Resolve
// uses, returning nil when there is none. It raises no permission warning: its
// caller is credentials-init, which is about to rewrite the file 0600.
func ReadCredentials(path string) (map[string]string, error) {
	return loadCredentials(path, readDotenv)
}

// loadCredentials reads the credentials file with read, returning nil when
// there is none. A missing file, or a path that runs through something that is
// not a directory, means the user has not made one; a dangling symbolic link
// does not. Any other failure means they made one that cannot be used, and is
// an error: falling through silently would make a token rotation look like it
// had no effect.
func loadCredentials(path string, read func(string) (map[string]string, error)) (map[string]string, error) {
	env, err := read(path)
	switch {
	case err == nil:
		return env, nil
	case errors.Is(err, fs.ErrNotExist) && isSymlink(path):
		return nil, fmt.Errorf("reading credentials file %s: it is a symbolic link to a file that "+
			"does not exist", displayPath(path))
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return nil, nil
	default:
		return nil, fmt.Errorf("reading credentials file %s: %w", displayPath(path), err)
	}
}

// isSymlink reports whether path itself is a symbolic link. A dangling link
// at the credentials path reads as "no such file", but it is a file the user
// made -- into a dotfiles repository that moved, or a volume not mounted -- so
// it must not read as "you have no credentials file".
func isSymlink(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode()&fs.ModeSymlink != 0
}

// DisplayPath shows a path under the home directory as ~/..., the form the
// docs use, so a message names the file the way a reader knows it.
func DisplayPath(path string) string { return displayPath(path) }

func displayPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, err := filepath.Rel(home, path); err == nil && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "~" + string(filepath.Separator) + rel
	}
	return path
}

// WriteCredentials writes values -- keyed by the CONFLUENCE_* names, an empty
// value omitted -- as the credentials file at path, mode 0600, creating its
// directory 0700 if it is missing.
//
// The write is a temporary file renamed over the target, so an interrupted
// write never leaves half a file, and a loose existing file comes out 0600.
// When path is a symbolic link (a dotfiles repository), the link's target is
// written and the link survives.
//
// It reads the result back through the parser Resolve uses and compares, the
// way the frontmatter writer verifies its own output: a file that says
// something other than what was asked for is worse than an error.
func WriteCredentials(path string, values map[string]string) error {
	var b strings.Builder
	b.WriteString(credentialsHeader)
	want := map[string]string{}
	for _, k := range credentialKeys {
		v := values[k]
		if v == "" {
			continue
		}
		if strings.ContainsAny(v, "\r\n") {
			return fmt.Errorf("%s holds a line break, which a credentials file cannot", k)
		}
		fmt.Fprintf(&b, "%s=%s\n", k, envValue(v))
		want[k] = v
	}
	for k := range values {
		if !isCredentialKey(k) {
			return fmt.Errorf("%s is not a credentials setting", k)
		}
	}

	target := path
	if isSymlink(path) {
		t, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("resolving %s: %w", displayPath(path), err)
		}
		target = t
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", displayPath(dir), err)
	}
	if err := replaceFile(target, b.String()); err != nil {
		return fmt.Errorf("writing %s: %w", displayPath(path), err)
	}

	got, err := readDotenv(target)
	if err != nil {
		return fmt.Errorf("reading back %s: %w", displayPath(path), err)
	}
	if len(got) != len(want) {
		return fmt.Errorf("%s did not read back as written", displayPath(path))
	}
	for k, v := range want {
		if got[k] != v {
			return fmt.Errorf("%s did not read back as written: %s differs", displayPath(path), k)
		}
	}
	return nil
}

// UnkeptLines counts the lines of the credentials file at path that
// WriteCredentials would not keep: comments, other than its own header, and
// keys that are not credentials settings. A missing file has none.
// credentials-init says so before it rewrites a file somebody wrote by hand.
func UnkeptLines(path string) (int, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "", line+"\n" == credentialsHeader:
		case strings.HasPrefix(line, "#"):
			n++
		default:
			key, _, _ := strings.Cut(strings.TrimPrefix(line, "export "), "=")
			if !isCredentialKey(strings.TrimSpace(key)) {
				n++
			}
		}
	}
	return n, nil
}

// replaceFile writes content to a temporary file beside target and renames it
// over target. os.CreateTemp creates the file 0600.
func replaceFile(target, content string) error {
	f, err := os.CreateTemp(filepath.Dir(target), ".credentials-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.WriteString(content)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		_ = os.Remove(tmp)
	}
	return err
}

func isCredentialKey(k string) bool {
	for _, c := range credentialKeys {
		if k == c {
			return true
		}
	}
	return false
}

// envValue spells v so that readDotenv reads it back unchanged. The rule is the
// round trip itself rather than a list of characters: readDotenv trims all
// Unicode space and strips one pair of matching quotes, so v is written bare
// exactly when that leaves it alone, and double-quoted otherwise -- which
// unquote strips exactly, whatever v holds.
func envValue(v string) string {
	if unquote(strings.TrimSpace(v)) == v {
		return v
	}
	return `"` + v + `"`
}

// FetchCloudID asks a Confluence Cloud site for its cloud ID, through
// GET {site}/_edge/tenant_info, which answers {"cloudId": "..."} with no
// credentials (docs/confluence/api.md).
//
// Not through send, which always sets basic auth: the route needs none, and
// its caller has no token yet. That loses send's retries and retry logging,
// which is acceptable because the caller goes on without a cloud ID when this
// fails. Its errors are the shapes FromRequest knows.
func FetchCloudID(siteURL string) (string, error) {
	u := strings.TrimRight(siteURL, "/") + "/_edge/tenant_info"
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", wrapRequest(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: timeoutRead}).Do(req)
	if err != nil {
		return "", wrapRequest(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return "", wrapRequest(err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", &HTTPError{StatusCode: resp.StatusCode, Method: http.MethodGet, URL: u, Body: string(body)}
	}
	var out struct {
		CloudID string `json:"cloudId"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", wrapRequest(fmt.Errorf("GET %s: %w", u, err))
	}
	if out.CloudID == "" {
		return "", wrapRequest(fmt.Errorf("GET %s: the answer has no cloudId", u))
	}
	if err := validateCloudID(out.CloudID, u); err != nil {
		return "", wrapRequest(err)
	}
	return out.CloudID, nil
}
