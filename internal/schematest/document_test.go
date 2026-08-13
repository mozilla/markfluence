package schematest

import (
	"sort"
	"testing"
)

// branchCommands returns the command names allOf has an if/then branch for.
func branchCommands(t *testing.T) []string {
	t.Helper()
	doc := parseDocument(t)
	names := make([]string, 0, len(doc.AllOf))
	for _, b := range doc.AllOf {
		if name := b.If.Properties.Command.Const; name != "" {
			names = append(names, name)
		}
	}
	return names
}

// TestEveryCommandHasABranch is the guard described at the top of document.go: an
// enum entry with no branch means that command's results and summary are
// unconstrained, so ValidateEnvelope would pass anything it emitted.
func TestEveryCommandHasABranch(t *testing.T) {
	enum := Commands(t)
	branches := branchCommands(t)

	inBranch := make(map[string]bool, len(branches))
	for _, name := range branches {
		inBranch[name] = true
	}
	for _, name := range enum {
		if !inBranch[name] {
			t.Errorf("command %q is in the command enum but has no if/then branch: "+
				"its results and summary would be unvalidated", name)
		}
	}

	inEnum := make(map[string]bool, len(enum))
	for _, name := range enum {
		inEnum[name] = true
	}
	for _, name := range branches {
		if !inEnum[name] {
			t.Errorf("command %q has an if/then branch but is not in the command enum: "+
				"the branch is dead, and any output naming it fails the enum", name)
		}
	}
}

// TestEveryBranchConstrainsResultsAndSummary catches a branch that exists but
// says nothing -- the same hole as a missing branch, one level down. A branch
// whose `then` omits results.items constrains only the array-ness that the top
// level already required.
func TestEveryBranchConstrainsResultsAndSummary(t *testing.T) {
	doc := parseDocument(t)
	if len(doc.AllOf) == 0 {
		t.Fatal("schema has no allOf branches at all")
	}
	for _, b := range doc.AllOf {
		name := b.If.Properties.Command.Const
		if name == "" {
			t.Error("allOf has a branch that does not switch on a command const")
			continue
		}
		results := b.Then.Properties.Results
		if results == nil || len(results.Items) == 0 {
			t.Errorf("branch for %q does not constrain results.items", name)
		}
		if len(b.Then.Properties.Summary) == 0 {
			t.Errorf("branch for %q does not constrain summary", name)
		}
	}
}

// TestNoDuplicateBranches guards the other way a branch goes quiet: two branches
// for one command, where the stale one is easy to edit by mistake and easier to
// miss, since allOf applies both and the looser is invisible.
func TestNoDuplicateBranches(t *testing.T) {
	seen := map[string]int{}
	for _, name := range branchCommands(t) {
		seen[name]++
	}
	dupes := []string{}
	for name, n := range seen {
		if n > 1 {
			dupes = append(dupes, name)
		}
	}
	sort.Strings(dupes)
	for _, name := range dupes {
		t.Errorf("command %q has %d if/then branches, want 1", name, seen[name])
	}
}
