// Package buildinfo exposes the binary's version and commit date.
package buildinfo

import "runtime/debug"

// Version is set at build time via -ldflags; it's "dev" for un-stamped builds.
var Version = "dev"

// CommitDate returns the VCS commit time the Go toolchain embeds at build time
// (the vcs.time build setting), or "unknown" when it isn't available.
func CommitDate() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" {
				return s.Value
			}
		}
	}
	return "unknown"
}

// Stamp is the human-readable build stamp: "markfluence vVERSION COMMITDATE".
func Stamp() string {
	return "markfluence v" + Version + " " + CommitDate()
}
