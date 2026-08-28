package client

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/project"
)

const (
	urlEnv      = "CONFLUENCE_URL"
	usernameEnv = "CONFLUENCE_USERNAME"
	tokenEnv    = "CONFLUENCE_TOKEN" // the API token; never a command-line flag
	cloudIDEnv  = "CONFLUENCE_CLOUD_ID"
	dotenvPath  = ".env"
)

var spaceKeyRE = regexp.MustCompile(`^/spaces/([^/]+)/`)

// Options carries the flag values Resolve needs, named so the two URL-ish fields
// can't be transposed at a call site.
type Options struct {
	// URL is the --url value (the Confluence site).
	URL string
	// Username is the --username value.
	Username string
	// CloudID is the --cloud-id value; set it to route requests through the
	// platform API gateway, which a scoped service-account token requires.
	CloudID string
	// EnvFile is the --env-file value; empty means .env at the discovered
	// project root (see loadEnvFile).
	EnvFile string
}

// Resolve builds a client from the site URL, username, cloud ID, and token. Each
// value is resolved with the precedence flag > environment variable > .env file:
// the URL, username, and cloud ID come from opts when set, then
// $CONFLUENCE_URL/$CONFLUENCE_USERNAME/$CONFLUENCE_CLOUD_ID, then the .env file;
// the API token comes only from $CONFLUENCE_TOKEN, then .env -- never a flag.
// opts.EnvFile selects which .env is read: when empty, .env at the discovered
// project root is read best-effort (a missing file is fine; see loadEnvFile
// for what "discovered" means here); when set it's an explicit path that must
// be readable. It returns a friendly error listing whatever is missing.
//
// The cloud ID is optional: without one, requests go to the site domain exactly
// as before, which is what an unscoped personal token and any Data Center site
// need.
func Resolve(opts Options) (*ConfluenceClient, error) {
	env, err := loadEnvFile(opts.EnvFile)
	if err != nil {
		return nil, err
	}

	siteURL := resolveValue(opts.URL, urlEnv, env)
	username := resolveValue(opts.Username, usernameEnv, env)
	cloudID := resolveValue(opts.CloudID, cloudIDEnv, env)
	token := resolveValue("", tokenEnv, env)

	var missing []string
	if siteURL == "" {
		missing = append(missing, "URL (--url or "+urlEnv+")")
	}
	if username == "" {
		missing = append(missing, "username (--username or "+usernameEnv+")")
	}
	if token == "" {
		missing = append(missing, "token ("+tokenEnv+")")
	}
	if len(missing) > 0 {
		return nil, errors.New("missing Confluence " + strings.Join(missing, ", "))
	}
	if err := validateCloudID(cloudID); err != nil {
		return nil, err
	}
	return New(Config{
		SiteURL:  siteURL,
		CloudID:  cloudID,
		Username: username,
		Token:    token,
	}), nil
}

// validateCloudID rejects a cloud ID that looks like a URL or a path fragment.
// The value is joined straight onto the gateway prefix, so pasting a whole
// gateway URL would otherwise produce an opaque 404 rather than a usable error.
func validateCloudID(cloudID string) error {
	if cloudID == "" {
		return nil
	}
	if strings.ContainsAny(cloudID, "/:") {
		return fmt.Errorf("invalid Confluence cloud ID %q (--cloud-id or %s): expected just the "+
			"identifier, not a URL or path", cloudID, cloudIDEnv)
	}
	return nil
}

// resolveValue applies the flag > environment > .env precedence for one setting.
func resolveValue(flagVal, envKey string, dotenv map[string]string) string {
	if flagVal != "" {
		return flagVal
	}
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return dotenv[envKey]
}

// loadEnvFile resolves which .env to read and parses it. An explicit envFile
// (from --env-file) must be readable, so a read failure is an error. With no
// explicit path, .env is read from the discovered project root -- the
// directory holding markfluence.yaml, found by walking up from the working
// directory, or the working directory itself when there is none (today's
// behavior). This is its own discovery pass, separate from the per-file root
// the converter uses: it starts at the working directory rather than a
// markdown file's directory, runs once before any file is touched, and
// doesn't bound anything -- it only answers "where is .env." A missing .env,
// wherever it lands, is fine and yields an empty map, matching prior
// behavior.
func loadEnvFile(envFile string) (map[string]string, error) {
	if envFile != "" {
		env, err := loadDotenv(envFile)
		if err != nil {
			return nil, fmt.Errorf("reading env file %q: %w", envFile, err)
		}
		return env, nil
	}

	dir := "."
	if cwd, err := os.Getwd(); err == nil {
		if root, err := project.Discover(cwd); err == nil {
			dir = root.Dir
			_ = root.FS.Close()
		}
	}

	env, err := loadDotenv(filepath.Join(dir, dotenvPath))
	if err != nil {
		return map[string]string{}, nil // a missing .env is fine
	}
	return env, nil
}

// loadDotenv reads a simple .env file into a map: KEY=value lines, with blank
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
