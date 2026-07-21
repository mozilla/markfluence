// Package ui provides consistent terminal output helpers for markfluence:
// colored Header/Success/Warn/Error lines, errors routed to stderr, and
// NO_COLOR / piped-output detection.
package ui

import (
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
)

var debug bool

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
)

// Header prints a bold section heading.
func Header(msg string) {
	fmt.Println("\n" + bold.Render(msg))
}

// Success prints a green check line.
func Success(msg string) {
	fmt.Println(green.Render("  ✓ ") + msg)
}

// Warn prints a yellow warning line to stderr.
func Warn(msg string) {
	fmt.Fprintln(os.Stderr, yellow.Render("  ! ")+msg)
}

// Error prints a red error line to stderr.
func Error(msg string) {
	fmt.Fprintln(os.Stderr, red.Render("  ✗ ")+msg)
}

// Info prints a plain info line.
func Info(msg string) {
	fmt.Println("    " + msg)
}

// Dim prints a dimmed line.
func Dim(msg string) {
	fmt.Println(gray.Render("    " + msg))
}

// Debug prints a line only when debug mode is enabled.
func Debug(msg string) {
	if debug {
		fmt.Fprintln(os.Stderr, gray.Render("  … "+msg))
	}
}
