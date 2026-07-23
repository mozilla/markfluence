package buildinfo

import (
	"strings"
	"testing"
)

func TestDateOnly(t *testing.T) {
	tests := []struct{ in, want string }{
		{"2026-07-23T19:27:49Z", "2026-07-23"},
		{"2026-07-23", "2026-07-23"},
		{"short", "short"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := dateOnly(tt.in); got != tt.want {
			t.Errorf("dateOnly(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestStampPrefix(t *testing.T) {
	// Under `go test` the toolchain embeds vcs.* settings, so the parenthetical is
	// present; regardless, the stamp always leads with "markfluence <version>".
	if got := Stamp(); !strings.HasPrefix(got, "markfluence "+Version) {
		t.Errorf("Stamp() = %q, want prefix %q", got, "markfluence "+Version)
	}
}
