package search

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/spf13/cobra"
)

// setFlags points the package-level flag vars at test values and restores them,
// so one test's --cql cannot leak into the next.
func setFlags(t *testing.T, space, ctype string, cql bool) {
	t.Helper()
	oldSpace, oldType, oldCQL := spaceOpt, typeOpt, cqlOpt
	t.Cleanup(func() { spaceOpt, typeOpt, cqlOpt = oldSpace, oldType, oldCQL })
	spaceOpt, typeOpt, cqlOpt = space, ctype, cql
}

// typeFlagCmd is a throwaway command carrying just the --type flag, so a test can
// control whether it counts as Changed.
func typeFlagCmd(t *testing.T, changed bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "x"}
	var v string
	cmd.Flags().StringVar(&v, "type", client.SearchTypePage, "")
	if changed {
		if err := cmd.Flags().Set("type", client.SearchTypeBlogpost); err != nil {
			t.Fatalf("setting --type: %v", err)
		}
	}
	return cmd
}

func TestParseLimit(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    int
		wantErr bool
	}{
		{"number", "25", 25, false},
		{"one", "1", 1, false},
		{"all", limitAll, 0, false},
		{"the default", defaultLimit, 10, false},
		// 0 is refused rather than read as unlimited, matching children --depth:
		// silently returning every match to someone who meant "none" is worse
		// than an error naming the value that does mean unlimited.
		{"zero", "0", 0, true},
		{"negative", "-1", 0, true},
		{"not a number", "lots", 0, true},
		{"empty", "", 0, true},
		{"float", "2.5", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseLimit(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseLimit(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err != nil {
				if !strings.Contains(err.Error(), limitAll) {
					t.Errorf("error = %q, want it to name %q as the unlimited value", err, limitAll)
				}
				return
			}
			if got != tt.want {
				t.Errorf("parseLimit(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestCheckType(t *testing.T) {
	for _, v := range []string{client.SearchTypePage, client.SearchTypeBlogpost, client.SearchTypeAll} {
		t.Run("accepts "+v, func(t *testing.T) {
			if err := checkType(v); err != nil {
				t.Errorf("checkType(%q) = %v, want nil", v, err)
			}
		})
	}
	for _, v := range []string{"attachment", "comment", "database", "whiteboard", "space", "", "Page"} {
		t.Run("refuses "+v, func(t *testing.T) {
			if err := checkType(v); err == nil {
				t.Errorf("checkType(%q) = nil, want an error", v)
			}
		})
	}
}

// TestCheckTypeRefusesFolderWithARemedy: full text cannot match a folder, which
// has no text, so accepting the value would always answer "no matches" to a
// question that cannot have one. The error has to name the command that can.
func TestCheckTypeRefusesFolderWithARemedy(t *testing.T) {
	err := checkType("folder")
	if err == nil {
		t.Fatal("checkType(\"folder\") = nil, want an error")
	}
	if !strings.Contains(err.Error(), "find") {
		t.Errorf("error = %q, want it to point at find", err)
	}
}

func TestCheckCQLFlags(t *testing.T) {
	t.Run("no cql, other flags fine", func(t *testing.T) {
		setFlags(t, "ENG", client.SearchTypeBlogpost, false)
		if err := checkCQLFlags(typeFlagCmd(t, true)); err != nil {
			t.Errorf("got %v, want nil -- these flags are only conflicts under --cql", err)
		}
	})
	t.Run("cql alone", func(t *testing.T) {
		setFlags(t, "", client.SearchTypePage, true)
		if err := checkCQLFlags(typeFlagCmd(t, false)); err != nil {
			t.Errorf("got %v, want nil", err)
		}
	})
	// Both are refused rather than ANDed onto the query: doing that to a query
	// containing `or` regroups it and silently answers a different question.
	t.Run("cql with space", func(t *testing.T) {
		setFlags(t, "ENG", client.SearchTypePage, true)
		err := checkCQLFlags(typeFlagCmd(t, false))
		if err == nil || !strings.Contains(err.Error(), "--space") {
			t.Errorf("got %v, want an error naming --space", err)
		}
	})
	t.Run("cql with an explicit type", func(t *testing.T) {
		setFlags(t, "", client.SearchTypeBlogpost, true)
		err := checkCQLFlags(typeFlagCmd(t, true))
		if err == nil || !strings.Contains(err.Error(), "--type") {
			t.Errorf("got %v, want an error naming --type", err)
		}
	})
	// --type has a non-empty default, so its value alone cannot say whether the
	// caller asked for it. Only Changed can.
	t.Run("cql with the default type is not a conflict", func(t *testing.T) {
		setFlags(t, "", client.SearchTypePage, true)
		if err := checkCQLFlags(typeFlagCmd(t, false)); err != nil {
			t.Errorf("got %v, want nil -- the default --type is not something the caller asked for", err)
		}
	})
}

func TestBlocks(t *testing.T) {
	got := blocks([]client.SearchMatch{
		{ID: "500", Type: "page", Title: "Deployment runbook", Space: "ENG",
			URL:     "https://wiki.example.net/wiki/spaces/ENG/pages/500/Deployment+runbook",
			Excerpt: "Deploying to prod requires an approved change request."},
		{ID: "300", Type: "blogpost", Title: "Why we deploy on Fridays", Space: "OPS",
			URL:     "https://wiki.example.net/wiki/spaces/OPS/blog/300",
			Excerpt: "It turns out we should not."},
	})
	want := strings.Join([]string{
		"Deployment runbook",
		"  page 500  ENG",
		"  https://wiki.example.net/wiki/spaces/ENG/pages/500/Deployment+runbook",
		"  Deploying to prod requires an approved change request.",
		"",
		"Why we deploy on Fridays",
		"  blogpost 300  OPS",
		"  https://wiki.example.net/wiki/spaces/OPS/blog/300",
		"  It turns out we should not.",
	}, "\n")
	if got != want {
		t.Errorf("blocks mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

// TestBlocksOmitsAnEmptyExcerpt: a hit matched on its title alone has no excerpt,
// and a blank indented line would read as a rendering bug.
func TestBlocksOmitsAnEmptyExcerpt(t *testing.T) {
	got := blocks([]client.SearchMatch{
		{ID: "1", Type: "page", Title: "Runbook", Space: "ENG", URL: "https://x/1"},
	})
	want := strings.Join([]string{"Runbook", "  page 1  ENG", "  https://x/1"}, "\n")
	if got != want {
		t.Errorf("blocks = %q, want %q", got, want)
	}
}

// TestBlocksDashesAMissingSpace: the id is still the answer, so a hit whose space
// key could not be derived is a usable block rather than a broken one.
func TestBlocksDashesAMissingSpace(t *testing.T) {
	got := blocks([]client.SearchMatch{{ID: "1", Type: "page", Title: "Runbook"}})
	want := "Runbook\n  page 1  -"
	if got != want {
		t.Errorf("blocks = %q, want %q", got, want)
	}
}

// TestBlocksPreservesOrder: the server's relevance order is the only ranking
// available, so the renderer must not impose one of its own.
func TestBlocksPreservesOrder(t *testing.T) {
	got := blocks([]client.SearchMatch{
		{ID: "3", Type: "page", Title: "Zebra"},
		{ID: "1", Type: "page", Title: "Apple"},
		{ID: "2", Type: "page", Title: "Mango"},
	})
	titles := []string{}
	for _, line := range strings.Split(got, "\n") {
		if !strings.HasPrefix(line, "  ") && line != "" {
			titles = append(titles, line)
		}
	}
	if want := "Zebra,Apple,Mango"; strings.Join(titles, ",") != want {
		t.Errorf("titles = %v, want %s (neither id- nor alphabetically sorted)", titles, want)
	}
}

// TestBlocksHasNoTrailingWhitespace keeps a block safe to copy out of a terminal.
func TestBlocksHasNoTrailingWhitespace(t *testing.T) {
	out := blocks([]client.SearchMatch{
		{ID: "1", Type: "page", Title: "A", Space: "ENG", URL: "https://x/1", Excerpt: "e"},
		{ID: "2", Type: "page", Title: "B"},
	})
	for i, line := range strings.Split(out, "\n") {
		if line != strings.TrimRight(line, " \t") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestCmdWiring(t *testing.T) {
	if Cmd.Name() != command {
		t.Errorf("Cmd.Name() = %q, want %q", Cmd.Name(), command)
	}
	for _, f := range []string{"space", "type", "limit", "cql"} {
		if Cmd.Flags().Lookup(f) == nil {
			t.Errorf("--%s not registered", f)
		}
	}
	// The defaults are part of the contract: --type page keeps a hit addressable,
	// and --limit's default is the bound the issue's "unlimited" was traded for.
	if got := Cmd.Flags().Lookup("type").DefValue; got != client.SearchTypePage {
		t.Errorf("--type default = %q, want %q", got, client.SearchTypePage)
	}
	if got := Cmd.Flags().Lookup("limit").DefValue; got != defaultLimit {
		t.Errorf("--limit default = %q, want %q", got, defaultLimit)
	}
	// A query is free text and a space key lives on the server, so nothing here
	// may complete to local files.
	if Cmd.ValidArgsFunction == nil {
		t.Error("no ValidArgsFunction")
	}
	if err := Cmd.Args(Cmd, []string{"a", "b"}); err == nil {
		t.Error("two args accepted, want exactly one")
	}
	if err := Cmd.Args(Cmd, nil); err == nil {
		t.Error("no args accepted, want exactly one")
	}
}

// TestHelpMentionsWhatSearchCannotFind: archived pages and folders are both
// invisible to full text, and a reader who does not know that reads an empty
// result as proof the page does not exist.
func TestHelpMentionsWhatSearchCannotFind(t *testing.T) {
	for _, want := range []string{"Archived", "folders", "find"} {
		if !strings.Contains(Cmd.Long, want) {
			t.Errorf("help does not mention %q", want)
		}
	}
}
