// Package clienttest builds a *client.ConfluenceClient against an
// httptest.Server for tests across the repo -- internal/client's own suite and
// every other package whose tests need a client talking to canned responses.
// Before this existed, each package (and several test files within
// internal/client itself) reimplemented "start a server, register its
// shutdown, point a client at it" independently, with the same
// username/token placeholders retyped each time.
package clienttest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

// New starts an httptest.Server running handler, registers its shutdown as
// test cleanup, and returns a *client.ConfluenceClient pointed at it. The
// username and token are fixed placeholders: nothing in a test server built
// this way checks them, since real auth happens on Atlassian's side.
func New(t *testing.T, handler http.HandlerFunc) *client.ConfluenceClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return client.New(client.Config{SiteURL: srv.URL, Username: "u", Token: "t"})
}

// credentialEnv lists the environment variables client.Resolve reads. It
// repeats internal/client's own names rather than exporting them, since they
// are a published interface and cannot change without breaking every user.
var credentialEnv = []string{
	"CONFLUENCE_URL", "CONFLUENCE_USERNAME", "CONFLUENCE_TOKEN", "CONFLUENCE_CLOUD_ID",
}

// RunIsolated runs a package's tests with no view of the developer's own
// credentials, and returns the exit code for TestMain to pass to os.Exit:
//
//	func TestMain(m *testing.M) { os.Exit(clienttest.RunIsolated(m)) }
//
// HOME and XDG_CONFIG_HOME point at an empty temporary directory, and the
// CONFLUENCE_* variables are unset, so a test sets exactly the settings it
// means to. Without it, a real credentials file or an exported token fills in
// whatever a test left unset: a "missing token" test passes or fails depending
// on who runs it, and a real cloud ID sends a test's requests to Atlassian's
// gateway instead of its httptest server.
//
// Every package whose tests reach client.Resolve calls it from TestMain, and
// not per test, so a test added later cannot forget it.
func RunIsolated(m *testing.M) int {
	home, err := os.MkdirTemp("", "markfluence-test-home-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "clienttest:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(home) }()
	env := map[string]string{"HOME": home, "XDG_CONFIG_HOME": filepath.Join(home, ".config")}
	for k, v := range env {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "clienttest:", err)
			return 1
		}
	}
	for _, k := range credentialEnv {
		if err := os.Unsetenv(k); err != nil {
			fmt.Fprintln(os.Stderr, "clienttest:", err)
			return 1
		}
	}
	return m.Run()
}
