// Package buildinfo exposes the binary's version, commit, and commit date.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

// Version is set at build time via -ldflags; it's "dev" for un-stamped builds.
var Version = "dev"

// setting returns a debug build setting (e.g. vcs.time, vcs.revision), or "".
func setting(key string) string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == key {
				return s.Value
			}
		}
	}
	return ""
}

// Revision returns the short (7-char) VCS commit hash (the vcs.revision build
// setting), or "" when it isn't available.
func Revision() string {
	rev := setting("vcs.revision")
	if len(rev) > 7 {
		return rev[:7]
	}
	return rev
}

// Stamp is the build stamp used both for --version and for the
// <!-- markfluence-version --> token embedded in published pages:
// "markfluence VERSION (SHA, DATE)". The commit hash and date are each omitted
// when unavailable (and the parenthetical drops entirely if both are).
func Stamp() string {
	s := "markfluence " + Version
	var meta []string
	if rev := Revision(); rev != "" {
		meta = append(meta, rev)
	}
	if ts := setting("vcs.time"); ts != "" {
		meta = append(meta, dateOnly(ts))
	}
	if len(meta) > 0 {
		s += " (" + strings.Join(meta, ", ") + ")"
	}
	return s
}

// dateOnly trims an RFC3339 timestamp to its YYYY-MM-DD date.
func dateOnly(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}
