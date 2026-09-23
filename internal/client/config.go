package client

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

const (
	urlEnv      = "CONFLUENCE_URL"
	usernameEnv = "CONFLUENCE_USERNAME"
	tokenEnv    = "CONFLUENCE_TOKEN" // the API token; never a command-line flag
	cloudIDEnv  = "CONFLUENCE_CLOUD_ID"
)

var spaceKeyRE = regexp.MustCompile(`^/spaces/([^/]+)/`)

// source is one place credentials come from, and the name an error uses for it.
type source struct {
	label  string
	values map[string]string
}

// lookup returns a setting's value and the index of the source that supplied
// it, or -1. An empty value counts as unset, so a lower source can supply it.
func lookup(sources []source, key string) (string, int) {
	for i, s := range sources {
		if v := s.values[key]; v != "" {
			return v, i
		}
	}
	return "", -1
}

// Resolve builds a client from the site URL, username, token, and optional
// cloud ID. Each resolves key by key from three sources, highest first: the
// file named by --env-file (envFile; it must be readable), the CONFLUENCE_*
// environment variables, and the user's credentials file (credentialsPath).
// Credentials come only from sources the user chose: nothing is discovered
// from the working directory, so a checkout cannot supply them (#188).
//
// The explicit file ranks above the environment on purpose: a token exported
// in a shell profile must not reach the URL in a file named for another
// instance.
//
// Three rules keep a token from going somewhere it was not meant for:
//
//   - The URL and the token must come from the same source, or Resolve fails
//     naming both. Key-by-key merging would otherwise send one source's token
//     to another source's URL.
//   - The cloud ID is read only from the URL's source, and ignored anywhere
//     else. It names one site, as the URL does, so a cloud ID from another
//     source never names this URL's site. Ignored rather than an error because
//     a higher source cannot say "no cloud ID": an empty value is unset.
//   - The username may come from any source; a wrong one fails with a 401.
//
// The credentials file is read only when the higher sources leave a setting
// unset, so a broken or loose file cannot fail or warn on a run that never
// uses it.
//
// Without a cloud ID, requests go to the site domain, which is what an
// unscoped personal token needs.
func Resolve(envFile string) (*ConfluenceClient, error) {
	var sources []source
	if envFile != "" {
		env, err := loadDotenv(envFile)
		if err != nil {
			return nil, fmt.Errorf("reading env file %q: %w", envFile, err)
		}
		sources = append(sources, source{label: "--env-file " + envFile, values: env})
	}
	sources = append(sources, environment())

	credPath := credentialsPath()
	if !complete(sources) && credPath != "" {
		creds, err := loadCredentials(credPath)
		if err != nil {
			return nil, err
		}
		if creds != nil {
			sources = append(sources, source{label: displayPath(credPath), values: creds})
		}
	}

	siteURL, urlFrom := lookup(sources, urlEnv)
	username, _ := lookup(sources, usernameEnv)
	token, tokenFrom := lookup(sources, tokenEnv)

	var missing []string
	if siteURL == "" {
		missing = append(missing, "URL ("+urlEnv+")")
	}
	if username == "" {
		missing = append(missing, "username ("+usernameEnv+")")
	}
	if token == "" {
		missing = append(missing, "token ("+tokenEnv+")")
	}
	if len(missing) > 0 {
		setThem := "set it in "
		if len(missing) > 1 {
			setThem = "set them in one place: "
		}
		return nil, fmt.Errorf("missing Confluence %s: %s%s",
			strings.Join(missing, ", "), setThem, placesToSet(credPath))
	}
	if urlFrom != tokenFrom {
		return nil, fmt.Errorf("%s comes from %s, but %s comes from %s. Set both in the same place",
			urlEnv, sources[urlFrom].label, tokenEnv, sources[tokenFrom].label)
	}
	cloudID := sources[urlFrom].values[cloudIDEnv]
	if err := validateCloudID(cloudID, sources[urlFrom].label); err != nil {
		return nil, err
	}
	return New(Config{
		SiteURL:  siteURL,
		CloudID:  cloudID,
		Username: username,
		Token:    token,
	}), nil
}

// environment is the CONFLUENCE_* variables as a source.
func environment() source {
	values := map[string]string{}
	for _, k := range []string{urlEnv, usernameEnv, tokenEnv, cloudIDEnv} {
		values[k] = os.Getenv(k)
	}
	return source{label: "the environment", values: values}
}

// complete reports whether sources already supply the URL, username, and
// token. The cloud ID is not asked about: it comes only from the URL's source,
// which is already among these when the URL is.
func complete(sources []source) bool {
	for _, k := range []string{urlEnv, usernameEnv, tokenEnv} {
		if _, i := lookup(sources, k); i < 0 {
			return false
		}
	}
	return true
}

// credentialsPath is where the user's credentials file lives:
// $XDG_CONFIG_HOME/markfluence/credentials, else
// $HOME/.config/markfluence/credentials, on every platform. os.UserConfigDir is
// not used because on macOS it answers ~/Library/Application Support, which
// command-line users do not look in.
//
// A relative XDG_CONFIG_HOME is ignored, as the XDG spec says, and a relative
// or empty HOME means there is no credentials file ("" here). Either would
// make the file depend on the working directory, which is exactly what #188
// removed.
func credentialsPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(x) {
		return filepath.Join(x, "markfluence", "credentials")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return ""
	}
	return filepath.Join(home, ".config", "markfluence", "credentials")
}

// loadCredentials reads the credentials file, returning nil when there is
// none. A missing file, or a path that runs through something that is not a
// directory, means the user has not made one. Any other failure means they
// made one that cannot be used, and is an error: falling through silently
// would make a token rotation look like it had no effect.
func loadCredentials(path string) (map[string]string, error) {
	env, err := loadDotenv(path)
	switch {
	case err == nil:
		return env, nil
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		return nil, nil
	default:
		return nil, fmt.Errorf("reading credentials file %s: %w", displayPath(path), err)
	}
}

// displayPath shows a path under the home directory as ~/..., the form the
// docs use, so an error names the file the way a reader knows it.
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

// placesToSet lists where credentials can go, for the "missing" error.
func placesToSet(credPath string) string {
	if credPath == "" {
		return "the environment, or a file named by --env-file"
	}
	return "the environment, " + displayPath(credPath) + ", or a file named by --env-file"
}

// validateCloudID rejects a cloud ID that looks like a URL or a path fragment.
// The value is joined straight onto the gateway prefix, so pasting a whole
// gateway URL would otherwise produce an opaque 404 rather than a usable error.
func validateCloudID(cloudID, from string) error {
	if cloudID == "" {
		return nil
	}
	if strings.ContainsAny(cloudID, "/:") {
		return fmt.Errorf("invalid Confluence cloud ID %q (%s, from %s): expected just the "+
			"identifier, not a URL or path", cloudID, cloudIDEnv, from)
	}
	return nil
}

// securityWarner receives a credential-hygiene warning. Package-level and set
// once from the command layer for the same reason SetRetryLogger is
// (retrylog.go): fifteen commands build a client through Resolve with an
// identical call, so anything passed per-call is something the sixteenth
// silently forgets -- and internal/client deliberately produces no output and
// imports no ui.
var securityWarner func(string)

// SetSecurityWarner installs fn as the credential-hygiene reporter, replacing
// any previous one. Pass nil to silence it.
func SetSecurityWarner(fn func(string)) { securityWarner = fn }

// warnLoosePermissions reports a credentials file (the user's own, or one named
// by --env-file) that anyone but its owner can reach, when it holds the API
// token.
//
// The token gate is what keeps this worth reading. A file carrying only
// CONFLUENCE_URL and CONFLUENCE_USERNAME at 0644 leaks nothing -- neither is a
// secret, and the cloud ID is documented as not one either -- and a warning
// that fires on a file with no secret in it is how a security warning becomes
// something people learn to scroll past.
//
// os.Stat, not Lstat: a file symlinked to a 0600 file is perfectly safe, and
// the link's own 0777 would cry wolf on every run. The user execute bit is
// ignored for the same reason -- 0700 is odd, but it is not a leak.
//
// A stat failure is silent. The file was just read, so a failure here is
// exotic, and a warning about the inability to warn is noise.
func warnLoosePermissions(path string, env map[string]string) {
	if securityWarner == nil || env[tokenEnv] == "" {
		return
	}
	fi, err := os.Stat(path)
	if err != nil {
		return
	}
	perm := fi.Mode().Perm()
	if perm&0o077 == 0 {
		return
	}
	securityWarner(fmt.Sprintf(
		"%s is %s (mode %#o) and holds your API token; run: chmod 600 %s",
		path, accessDescription(perm), perm, shellArg(path)))
}

// accessDescription names what is actually wrong with a mode, rather than
// assuming the readable case: 0622 is a real finding but nobody can read it,
// and a message that says otherwise is one a reader can check and disbelieve.
func accessDescription(perm os.FileMode) string {
	switch {
	case perm&0o044 != 0:
		return "readable by others"
	case perm&0o022 != 0:
		return "writable by others"
	default:
		return "accessible to others"
	}
}

// shellArg quotes a path that would not survive being pasted into a shell. The
// remedy is the point of the warning, so a path with a space in it has to come
// out runnable; a path without one stays unquoted, since that is every path
// anyone actually has.
func shellArg(path string) string {
	if strings.ContainsAny(path, " \t\n'\"$`\\&;|<>()*?[]#~") {
		return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
	}
	return path
}

// loadDotenv reads a simple env file into a map: KEY=value lines, with blank
// lines and # comments skipped, an optional leading "export ", and optional
// surrounding single or double quotes stripped. Values are taken verbatim (no
// shell expansion). It errors if the file can't be read.
func loadDotenv(path string) (map[string]string, error) {
	out := map[string]string{}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(key)] = unquote(strings.TrimSpace(value))
	}
	// Here rather than in Resolve: this is the one function both the
	// credentials file and an explicit --env-file go through, and the check
	// needs the parsed contents to know whether a token is in there.
	warnLoosePermissions(path, out)
	return out, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		if q := s[0]; (q == '"' || q == '\'') && s[len(s)-1] == q {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// SpaceKeyFromWebUI extracts the space key from a "/spaces/{key}/pages/..."
// webui link, returning "" if it doesn't match.
func SpaceKeyFromWebUI(webui string) string {
	if m := spaceKeyRE.FindStringSubmatch(webui); m != nil {
		return m[1]
	}
	return ""
}
