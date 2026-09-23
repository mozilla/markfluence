// Package testenv runs a package's tests with no view of the developer's own
// Confluence credentials. It is its own package, importing nothing from this
// module, so internal/client's tests (package client) can use it too: a helper
// in internal/clienttest cannot serve them, since clienttest imports client.
package testenv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// credentialEnv lists the environment variables client.Resolve reads. It
// repeats internal/client's names because importing client here would put
// the cycle back; they are a published interface and do not change.
var credentialEnv = []string{
	"CONFLUENCE_URL", "CONFLUENCE_USERNAME", "CONFLUENCE_TOKEN", "CONFLUENCE_CLOUD_ID",
}

// RunIsolated runs m's tests and returns the exit code for TestMain to pass to
// os.Exit:
//
//	func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
//
// HOME and XDG_CONFIG_HOME point at an empty temporary directory, and the
// CONFLUENCE_* variables are unset, so a test sets exactly the settings it
// means to. Without it, a real credentials file or an exported variable fills
// in whatever a test left unset: a "missing token" test passes or fails
// depending on who runs it, and an exported CONFLUENCE_CLOUD_ID sends a test's
// requests to Atlassian's gateway instead of its httptest server.
//
// Every package whose code calls client.Resolve calls this from TestMain,
// whether or not its tests reach Resolve today, so a test added later is
// isolated without anyone remembering to do it.
func RunIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "markfluence-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "testenv:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(home) }()
	env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	for k, v := range env {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "testenv:", err)
			return 1
		}
	}
	for _, k := range credentialEnv {
		if err := os.Unsetenv(k); err != nil {
			fmt.Fprintln(os.Stderr, "testenv:", err)
			return 1
		}
	}
	return m.Run()
}
