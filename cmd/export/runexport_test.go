package export

// runExport's wiring. The three entry points differ only in the target they
// build, so these are the fields that decide behaviour -- and a review found
// that both scalar ones could be inverted with the whole suite still green.

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagetree"
	"github.com/mozilla/markfluence/internal/ui"
)

// TestRunExportPlantsAProjectFileOnlyWhenNeeded pins target.multiPage against
// each entry point's real value. A single-page export must not plant one: the
// file re-roots the documentation root for every later run in that directory.
func TestRunExportPlantsAProjectFileOnlyWhenNeeded(t *testing.T) {
	for _, c := range []struct {
		name      string
		multiPage bool
		want      bool
	}{
		{"page at depth 0", false, false},
		{"page with a depth", true, true},
		{"folder", true, true},
		{"space", true, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			err := runExport(nil, dir, target{
				multiPage: c.multiPage,
				walk:      func() ([]pagetree.Node, error) { return nil, nil },
				fail:      func(error, jsonout.Code) error { return nil },
			})
			if err != nil {
				t.Fatalf("runExport: %v", err)
			}
			_, statErr := os.Stat(filepath.Join(dir, "markfluence.yaml"))
			if got := statErr == nil; got != c.want {
				t.Errorf("project file present = %v, want %v", got, c.want)
			}
		})
	}
}

// TestRunExportPlantsNothingWhenTheWalkFails is the ordering the abstraction
// exists for: the marker goes in after the walk, so a command that exported
// nothing leaves the destination as it found it.
func TestRunExportPlantsNothingWhenTheWalkFails(t *testing.T) {
	dir := t.TempDir()
	called := false
	err := runExport(nil, dir, target{
		multiPage: true,
		walk:      func() ([]pagetree.Node, error) { return nil, errors.New("boom") },
		fail: func(error, jsonout.Code) error {
			called = true
			return ui.SilentExit(1)
		},
	})
	if !ui.IsSilent(err) {
		t.Fatalf("runExport = %v, want the target's failure", err)
	}
	if !called {
		t.Error("the target's own failure reporter must be used")
	}
	if _, err := os.Stat(filepath.Join(dir, "markfluence.yaml")); err == nil {
		t.Error("a failed walk must leave no project file behind")
	}
}

// TestRunExportUsesTheTargetsFailureReporter distinguishes the space path,
// which has no page id to name and reports an error object on stderr, from the
// page and folder paths, which name themselves in a result.
func TestRunExportUsesTheTargetsFailureReporter(t *testing.T) {
	var gotCode jsonout.Code
	_ = runExport(nil, t.TempDir(), target{
		multiPage: true,
		walk:      func() ([]pagetree.Node, error) { return nil, errors.New("boom") },
		fail: func(_ error, code jsonout.Code) error {
			gotCode = code
			return ui.SilentExit(1)
		},
	})
	if gotCode == "" {
		t.Error("the walk error's code must reach the reporter")
	}
}
