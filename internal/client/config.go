package client

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// The names of the four settings, as environment variables and as keys in
// the credentials file.
const (
	URLVar      = "CONFLUENCE_URL"
	UsernameVar = "CONFLUENCE_USERNAME"
	TokenVar    = "CONFLUENCE_TOKEN" // the API token; never a command-line flag
	CloudIDVar  = "CONFLUENCE_CLOUD_ID"
)

// CredentialsDoc is where the credential errors send a reader. It points at
// GitHub rather than at a help topic so the errors and the README share one
// reference; it describes main, which a released binary may trail. Renaming
// docs/credentials.md breaks the pointer in every released binary.
const CredentialsDoc = "https://github.com/mozilla/markfluence/blob/main/docs/credentials.md"

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
// environment variables, and the user's credentials file (CredentialsPath).
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
// Without a cloud ID, requests go to the site domain. An unscoped personal
// token works either way; a scoped one needs the cloud ID.
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

	credPath := CredentialsPath()
	if !complete(sources) && credPath != "" {
		creds, err := loadCredentials(credPath, loadDotenv)
		if err != nil {
			return nil, err
		}
		if creds != nil {
			sources = append(sources, source{label: displayPath(credPath), values: creds})
		}
	}

	siteURL, urlFrom := lookup(sources, URLVar)
	username, _ := lookup(sources, UsernameVar)
	token, tokenFrom := lookup(sources, TokenVar)

	var missing []string
	if siteURL == "" {
		missing = append(missing, "URL ("+URLVar+")")
	}
	if username == "" {
		missing = append(missing, "username ("+UsernameVar+")")
	}
	if token == "" {
		missing = append(missing, "token ("+TokenVar+")")
	}
	if len(missing) > 0 {
		setThem := "set it in "
		if len(missing) > 1 {
			setThem = "set them in one place: "
		}
		// When one of the URL and the token is already set, the other can
		// only go where it is: anywhere else fails the same-source rule.
		places := placesToSet(credPath)
		switch {
		case siteURL == "" && token != "":
			places = sources[tokenFrom].label + ", where " + TokenVar + " is"
		case token == "" && siteURL != "":
			places = sources[urlFrom].label + ", where " + URLVar + " is"
		case credPath != "":
			// The command writes the credentials file, so it is only worth
			// naming when there is one to write -- and not in the two cases
			// above, where a new file would fail the same-source rule.
			setThem = "run markfluence credentials-init, or " + setThem
		}
		return nil, fmt.Errorf("missing Confluence %s: %s%s. See %s",
			strings.Join(missing, ", "), setThem, places, CredentialsDoc)
	}
	if urlFrom != tokenFrom {
		return nil, fmt.Errorf("%s comes from %s, but %s comes from %s. Set both in the same place. See %s",
			URLVar, sources[urlFrom].label, TokenVar, sources[tokenFrom].label, CredentialsDoc)
	}
	cloudID := sources[urlFrom].values[CloudIDVar]
	warnIgnoredCloudID(sources, urlFrom)
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

// warnIgnoredCloudID reports a cloud ID that a place *above* the URL's set and
// Resolve ignored. That is someone who set one on purpose -- exported it for a
// scoped token while the URL lives in the credentials file -- and would
// otherwise get an unexplained 401 from their site. A cloud ID *below* the
// URL's place is not reported: the credentials file for one instance, while
// --env-file names another, is the ordinary case the rule exists for.
func warnIgnoredCloudID(sources []source, urlFrom int) {
	if securityWarner == nil {
		return
	}
	for _, s := range sources[:urlFrom] {
		if s.values[CloudIDVar] != "" {
			securityWarner(fmt.Sprintf(
				"%s from %s is ignored, because %s comes from %s: set the cloud ID in the same place as the URL",
				CloudIDVar, s.label, URLVar, sources[urlFrom].label))
			return
		}
	}
}

// environment is the CONFLUENCE_* variables as a source.
func environment() source {
	values := map[string]string{}
	for _, k := range []string{URLVar, UsernameVar, TokenVar, CloudIDVar} {
		values[k] = os.Getenv(k)
	}
	return source{label: "the environment", values: values}
}

// complete reports whether sources already supply the URL, username, and
// token. The cloud ID is not asked about: it comes only from the URL's source,
// which is already among these when the URL is.
func complete(sources []source) bool {
	for _, k := range []string{URLVar, UsernameVar, TokenVar} {
		if _, i := lookup(sources, k); i < 0 {
			return false
		}
	}
	return true
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
			"identifier, not a URL or path", cloudID, CloudIDVar, from)
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
// Only a regular file is judged. `--env-file <(pass show confluence)` reads a
// pipe, which reports mode 0440 on macOS: warning about it would tell the
// safest setup to run a chmod that cannot work.
//
// A stat failure is silent. The file was just read, so a failure here is
// exotic, and a warning about the inability to warn is noise.
func warnLoosePermissions(path string, env map[string]string) {
	if securityWarner == nil || env[TokenVar] == "" {
		return
	}
	fi, err := os.Stat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return
	}
	perm := fi.Mode().Perm()
	if !LooseMode(perm) {
		return
	}
	securityWarner(fmt.Sprintf(
		"%s is %s (mode %#o) and holds your API token; run: chmod 600 %s",
		path, accessDescription(perm), perm, shellArg(path)))
}

// LooseMode reports whether perm lets anyone but the owner reach a file. The
// owner's own execute bit does not count: 0700 is odd, but it is not a leak.
func LooseMode(perm os.FileMode) bool { return perm&0o077 != 0 }

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
	out, err := readDotenv(path)
	if err != nil {
		return nil, err
	}
	// Here rather than in Resolve: this is the one function both the
	// credentials file and an explicit --env-file go through, and the check
	// needs the parsed contents to know whether a token is in there.
	warnLoosePermissions(path, out)
	return out, nil
}

// readDotenv is loadDotenv without the permission warning, for a reader that
// is about to rewrite the file 0600 or has just written it.
func readDotenv(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseDotenv(data), nil
}

// parseDotenv is the env-file format itself.
func parseDotenv(data []byte) map[string]string {
	out := map[string]string{}
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
	return out
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
