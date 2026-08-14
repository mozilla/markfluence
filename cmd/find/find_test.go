package find

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
)

func TestTableAligns(t *testing.T) {
	got := table([]client.TitleMatch{
		{ID: "500", Type: "page", Title: "Runbook", Status: "current", Space: "ENG",
			URL: "https://wiki.example.net/wiki/spaces/ENG/pages/500/Runbook"},
		{ID: "300", Type: "folder", Title: "Runbook", Status: "current", Space: "CLOUDSERVICES",
			URL: "https://wiki.example.net/wiki/spaces/CLOUDSERVICES/folder/300"},
	})
	want := strings.Join([]string{
		"TYPE    ID   SPACE          STATUS   TITLE    URL",
		"page    500  ENG            current  Runbook  https://wiki.example.net/wiki/spaces/ENG/pages/500/Runbook",
		"folder  300  CLOUDSERVICES  current  Runbook  https://wiki.example.net/wiki/spaces/CLOUDSERVICES/folder/300",
	}, "\n")
	if got != want {
		t.Errorf("table mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

// TestTableHasNoTrailingWhitespace: the last column is deliberately unpadded,
// so a row is safe to copy out of a terminal.
func TestTableHasNoTrailingWhitespace(t *testing.T) {
	out := table([]client.TitleMatch{
		{ID: "1", Type: "page", Title: "A", Status: "current", Space: "ENG", URL: "https://x/1"},
		{ID: "22", Type: "page", Title: "Longer title", Status: "archived", Space: "OPSOPS", URL: "https://x/22"},
	})
	for i, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " ") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

// TestTableDashesAMissingLink: a match whose space key could not be derived is
// still a usable row -- the id is the answer -- so it renders as "-" rather
// than collapsing the columns.
func TestTableDashesAMissingLink(t *testing.T) {
	out := table([]client.TitleMatch{{ID: "1", Type: "page", Title: "A", Status: "current"}})
	lines := strings.Split(out, "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2", len(lines))
	}
	if !strings.Contains(lines[1], " -  ") || !strings.HasSuffix(lines[1], "-") {
		t.Errorf("row = %q, want dashes for the missing space and url", lines[1])
	}
}

// TestArchivedStatusIsVisible: an archived page reserves its title but is
// absent from the page tree, so a reader who cannot see the status would treat
// an unusable id as a live page.
func TestArchivedStatusIsVisible(t *testing.T) {
	out := table([]client.TitleMatch{
		{ID: "400", Type: "page", Title: "Runbook", Status: "archived", Space: "ENG", URL: "https://x/400"},
	})
	if !strings.Contains(out, "archived") {
		t.Errorf("table = %q, want the archived status shown", out)
	}
}

func TestCmdWiring(t *testing.T) {
	if Cmd.Name() != "find" {
		t.Errorf("Cmd.Name() = %q, want find", Cmd.Name())
	}
	if Cmd.Flags().Lookup("space") == nil {
		t.Error("--space not registered")
	}
	// A title is free text and a space key lives on the server, so nothing here
	// may complete to local files.
	if Cmd.ValidArgsFunction == nil {
		t.Error("no ValidArgsFunction")
	}
	if err := Cmd.Args(Cmd, []string{"a", "b"}); err == nil {
		t.Error("two args accepted, want exactly one")
	}
}
