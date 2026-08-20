// Package ui provides consistent terminal output helpers for markfluence:
// colored Header/Success/Warn/Error lines, errors routed to stderr, and
// NO_COLOR / piped-output detection.
package ui

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

// silentErr marks a failure a command has already reported (via Error, or via a
// JSON payload/error object). The root exits with the carried code without
// printing anything further.
type silentErr struct{ code int }

func (e *silentErr) Error() string { return "reported" }

// ErrSilent marks a reported failure with the default operational exit code (1).
// Any other error reaching the root is cobra-generated (bad args/flags).
var ErrSilent error = &silentErr{code: 1}

// SilentExit returns a reported-failure error carrying a specific exit code
// (1 = operational failure, 2 = config/usage/pre-flight).
func SilentExit(code int) error { return &silentErr{code: code} }

// IsSilent reports whether err is a reported (silent) failure.
func IsSilent(err error) bool {
	var s *silentErr
	return errors.As(err, &s)
}

// ExitCode returns the exit code carried by a silent error, or 1 for any other
// non-nil error.
func ExitCode(err error) int {
	var s *silentErr
	if errors.As(err, &s) {
		return s.code
	}
	return 1
}

var debug bool

// jsonMode, when set, silences the stdout helpers (Header/Success/Info/Dim) and
// the stderr Warn/Error lines: in --json mode all of that content is carried in
// the structured payload / error object instead, and stdout must stay valid
// JSON. Debug is exempt (it is --debug-gated and goes to stderr).
var jsonMode bool

// SetJSON enables or disables JSON mode. See jsonMode.
func SetJSON(v bool) { jsonMode = v }

// IsJSON reports whether JSON mode is active.
func IsJSON() bool { return jsonMode }

// IsPiped reports whether stdout is piped (not a terminal).
var IsPiped = func() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) == 0
}

// SetDebug enables or disables debug output.
func SetDebug(v bool) { debug = v }

// IsDebug reports whether debug mode is active.
func IsDebug() bool { return debug }

var (
	green  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	yellow = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	red    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	gray   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold   = lipgloss.NewStyle().Bold(true)
	invert = lipgloss.NewStyle().Reverse(true)
)

// Header prints a bold section heading. No-op in JSON mode.
func Header(msg string) {
	if jsonMode {
		return
	}
	fmt.Println("\n" + bold.Render(msg))
}

// Success prints a green check line. No-op in JSON mode.
func Success(msg string) {
	if jsonMode {
		return
	}
	fmt.Println(green.Render("  ✓ ") + msg)
}

// Warn prints a yellow warning line to stderr. No-op in JSON mode (warnings are
// carried in the payload instead).
func Warn(msg string) {
	if jsonMode {
		return
	}
	fmt.Fprintln(os.Stderr, yellow.Render("  ! ")+msg)
}

// Error prints a red error line to stderr. No-op in JSON mode (errors are
// carried in the payload / error object instead).
func Error(msg string) {
	if jsonMode {
		return
	}
	fmt.Fprintln(os.Stderr, red.Render("  ✗ ")+msg)
}

// Info prints a plain info line. No-op in JSON mode.
func Info(msg string) {
	if jsonMode {
		return
	}
	fmt.Println("    " + msg)
}

// Dim prints a dimmed line. No-op in JSON mode.
func Dim(msg string) {
	if jsonMode {
		return
	}
	fmt.Println(gray.Render("    " + msg))
}

// Match styles a run of text that a search reported as a matched term, and
// returns it rather than printing: callers build a block and print it once.
//
// Reverse video rather than a color. The four colors here are semantically
// spoken for -- green success, yellow warning, red error, gray dim -- and
// reusing one would say something false about a matched word. Reversing also
// survives a light or dark terminal, where a fixed color may not.
//
// Bold was tried first and is too subtle: a matched term sits inside a 150-450
// character excerpt of ordinary prose, where weight alone does not find the eye
// the way a flipped background does.
//
// No jsonMode guard, unlike the printing helpers: this returns a string to a
// caller that is already inside a non-JSON branch. lipgloss renders it unstyled
// when color is off (NO_COLOR, --no-color, or stdout not a terminal), so the
// escape codes never reach a pipe.
func Match(s string) string { return invert.Render(s) }

// Debug prints a line only when debug mode is enabled.
func Debug(msg string) {
	if debug {
		fmt.Fprintln(os.Stderr, gray.Render("  … "+msg))
	}
}
