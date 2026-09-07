package ui

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr redirected, returning what it wrote.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestSecurityWarnIsNotSilencedByJSONMode is the whole reason this helper
// exists rather than reusing Warn. Every other helper here goes quiet under
// --json because its content is carried in the structured payload instead; a
// credential warning has no payload to be carried in, so silencing it would
// drop it in exactly the automated runs most likely to need it. A refactor
// that makes JSON mode silence everything should fail here.
func TestSecurityWarnIsNotSilencedByJSONMode(t *testing.T) {
	SetJSON(true)
	t.Cleanup(func() { SetJSON(false) })

	out := captureStderr(t, func() { SecurityWarn(".env is readable by others") })
	if !strings.Contains(out, ".env is readable by others") {
		t.Errorf("stderr = %q, want the warning even in JSON mode", out)
	}

	// The contrast: ordinary warnings stay silent, so this is a deliberate
	// exception and not a hole in the rule.
	quiet := captureStderr(t, func() { Warn("an ordinary warning") })
	if quiet != "" {
		t.Errorf("Warn wrote %q under --json, want nothing", quiet)
	}
}

// TestSecurityWarnWritesToStderr keeps it off stdout, which under --json is a
// JSON document and under human output may be a table on its way into a pipe.
func TestSecurityWarnWritesToStderr(t *testing.T) {
	out := captureStderr(t, func() { SecurityWarn("mind the mode") })
	if !strings.Contains(out, "mind the mode") {
		t.Errorf("stderr = %q, want the warning", out)
	}
}
