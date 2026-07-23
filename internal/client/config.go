package client

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

const (
	urlEnv      = "CONFLUENCE_URL"
	usernameEnv = "CONFLUENCE_USERNAME"
	tokenEnv    = "CONFLUENCE_TOKEN" // the API token; never a command-line flag
	dotenvPath  = ".env"
)

var spaceKeyRE = regexp.MustCompile(`^/spaces/([^/]+)/`)

// Resolve builds a client from the base URL, username, and token. Each value is
// resolved with the precedence flag > environment variable > .env file: the URL
// and username come from the urlFlag/usernameFlag values when set, then
// $CONFLUENCE_URL/$CONFLUENCE_USERNAME, then the .env file; the API token comes
// only from $CONFLUENCE_TOKEN, then .env -- never a flag. envFile selects which
// .env is read: when empty the default ./.env is read best-effort (a missing
// file is fine); when set it's an explicit path that must be readable. It
// returns a friendly error listing whatever is missing.
func Resolve(urlFlag, usernameFlag, envFile string) (*ConfluenceClient, error) {
	env, err := loadEnvFile(envFile)
	if err != nil {
		return nil, err
	}

	baseURL := resolveValue(urlFlag, urlEnv, env)
	username := resolveValue(usernameFlag, usernameEnv, env)
	token := resolveValue("", tokenEnv, env)

	var missing []string
	if baseURL == "" {
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
	return New(baseURL, username, token), nil
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
// explicit path the default ./.env is best-effort: a missing file yields an
// empty map, matching the prior behavior.
func loadEnvFile(envFile string) (map[string]string, error) {
	if envFile != "" {
		env, err := loadDotenv(envFile)
		if err != nil {
			return nil, fmt.Errorf("reading env file %q: %w", envFile, err)
		}
		return env, nil
	}
	env, err := loadDotenv(dotenvPath)
	if err != nil {
		return map[string]string{}, nil // a missing ./.env is fine
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
