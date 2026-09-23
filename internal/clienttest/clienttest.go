// Package clienttest builds a *client.ConfluenceClient against an
// httptest.Server for tests across the repo -- internal/client's own suite and
// every other package whose tests need a client talking to canned responses.
// Before this existed, each package (and several test files within
// internal/client itself) reimplemented "start a server, register its
// shutdown, point a client at it" independently, with the same
// username/token placeholders retyped each time.
package clienttest

import (
	"net/http"
	"net/http/httptest"
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
