package labels_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/labels"
)

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestValidateRefusesTheSeparatorClass is the test this package exists for.
// Confluence splits a label name on a space or a comma, with a 200 and no
// warning, so an unvalidated "Runbook Two" publishes as two labels that read
// back as neither and are re-added on every run forever. Two labels in the SRE
// space are that bug having already happened to a person.
func TestValidateRefusesTheSeparatorClass(t *testing.T) {
	for _, name := range []string{"Runbook Two", "a,b", "trail ", " lead", "a\tb"} {
		t.Run(name, func(t *testing.T) {
			lowered, _ := labels.Normalize(name)
			if err := labels.Validate(lowered); err == nil {
				t.Errorf("Validate(%q) = nil, want a refusal", lowered)
			}
		})
	}
}

// TestValidateMirrorsTheServersRejectSet walks every character Confluence's own
// 400 body names, so the set cannot drift from what was measured.
func TestValidateMirrorsTheServersRejectSet(t *testing.T) {
	for _, r := range labels.RejectChars {
		name := "a" + string(r) + "b"
		if err := labels.Validate(name); err == nil {
			t.Errorf("Validate(%q) = nil, want a refusal for %q", name, string(r))
		}
	}
}

// TestValidateAcceptsRealLabels pins the decision to mirror the reject set
// rather than invent an allowlist: every one of these is a real shape, and
// ci/cd is a real label in the SRE space.
func TestValidateAcceptsRealLabels(t *testing.T) {
	for _, name := range []string{
		"runbook", "ci/cd", "dataops_reports", "héllo-wörld", "v1", "2026",
		"a+b", `a"b`, "a'b", "🎉",
	} {
		if err := labels.Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}
}

// TestValidateCountsUTF16Units pins the three points measured against the
// server. A byte-based check fails the first case and a rune-based one passes
// the third, so this is what keeps the units right in both directions.
func TestValidateCountsUTF16Units(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		wantErr bool
	}{
		{"255 accented runes is 255 units", strings.Repeat("é", 255), false},
		{"256 accented runes is 256 units", strings.Repeat("é", 256), true},
		{"128 emoji is 256 units", strings.Repeat("🎉", 128), true},
		{"127 emoji is 254 units", strings.Repeat("🎉", 127), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := labels.Validate(tt.label)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate(%d runes) = %v, wantErr %v",
					len([]rune(tt.label)), err, tt.wantErr)
			}
		})
	}
}

// TestValidateQuotesTheRejectSet: half the set is surprising -- a "." means a
// version-shaped label is impossible -- and an author should not have to bisect
// their own label to discover it.
func TestValidateQuotesTheRejectSet(t *testing.T) {
	err := labels.Validate("v1.2")
	if err == nil {
		t.Fatal("Validate(v1.2) = nil, want a refusal")
	}
	for _, want := range []string{"space", "."} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

// TestValidateExplainsSplittingOnlyWhenItSplits. The separator explanation is
// true of exactly two characters, and claiming it about a "." told an author
// that v1.2 "would publish as several labels" -- which it would not. A warning
// that is wrong about the case in front of you is how the accurate one stops
// being read.
func TestValidateExplainsSplittingOnlyWhenItSplits(t *testing.T) {
	splits := []string{"a b", "a,b"}
	for _, name := range splits {
		err := labels.Validate(name)
		if err == nil {
			t.Fatalf("Validate(%q) = nil, want a refusal", name)
		}
		if !strings.Contains(err.Error(), "several labels") {
			t.Errorf("Validate(%q) = %q, want the splitting explanation", name, err)
		}
	}
	for _, name := range []string{"v1.2", "a#b", "a[b", "a?b"} {
		err := labels.Validate(name)
		if err == nil {
			t.Fatalf("Validate(%q) = nil, want a refusal", name)
		}
		if strings.Contains(err.Error(), "several labels") {
			t.Errorf("Validate(%q) = %q, want no splitting claim for a non-separator", name, err)
		}
	}
}

func TestDeclaredNormalizesAndWarns(t *testing.T) {
	set, err := labels.Declared(map[string][]string{"labels": {"Runbook", "HOWTO"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eq(set.Names, []string{"howto", "runbook"}) {
		t.Errorf("Names = %q, want [howto runbook]", set.Names)
	}
	if len(set.Warnings) != 2 {
		t.Errorf("Warnings = %q, want one per label changed", set.Warnings)
	}
	if !strings.Contains(strings.Join(set.Warnings, " "), "Runbook") {
		t.Errorf("warnings do not name the label as written: %q", set.Warnings)
	}
}

// TestDeclaredSortsAndDedupes: the set is what matters to Confluence, and
// neither label GET returns a useful order, so a generated list is sorted or it
// is unstable across runs.
func TestDeclaredSortsAndDedupes(t *testing.T) {
	set, err := labels.Declared(map[string][]string{
		"labels": {"runbook", "ci/cd", "runbook", "Ci/CD"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !eq(set.Names, []string{"ci/cd", "runbook"}) {
		t.Errorf("Names = %q, want [ci/cd runbook]", set.Names)
	}
}

// TestDeclaredSeparatesAbsentFromEmpty pins the asymmetry the whole field rests
// on. Absent means the live labels are untouched; empty means remove them all.
// Collapsing the two either strips a hand-labeled page on a run that never
// mentioned labels, or makes "remove them all" impossible to say.
func TestDeclaredSeparatesAbsentFromEmpty(t *testing.T) {
	absent, err := labels.Declared(map[string][]string{}, map[string]string{"title": "T"})
	if err != nil {
		t.Fatal(err)
	}
	if absent.Declared {
		t.Error("Declared = true for a file with no labels key")
	}

	empty, err := labels.Declared(map[string][]string{"labels": {}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !empty.Declared {
		t.Error("Declared = false for labels: []")
	}
	if len(empty.Names) != 0 {
		t.Errorf("Names = %q, want empty", empty.Names)
	}
}

// TestDeclaredRefusesAScalar: the field is destructive, so "labels:" with
// nothing after it must not be read as "remove every label" on a file where an
// author typed a key and stopped.
//
// Driven through frontmatter.Parse rather than a hand-built map, because the
// null spellings are the cases worth proving and the parser is what flattens
// them: "~" and "null" both arrive here as "", so a test that passed "" three
// times would prove one case and claim three.
func TestDeclaredRefusesAScalar(t *testing.T) {
	tests := []struct{ name, block, wantSubstr string }{
		{"a single name", "labels: runbook", "must be a list"},
		{"no value", "labels:", "no value"},
		{"tilde null", "labels: ~", "no value"},
		{"literal null", "labels: null", "no value"},
		{"blank string", `labels: ""`, "no value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mf, err := frontmatter.Parse("doc.md", "---\n"+tt.block+"\n---\nbody\n")
			if err != nil {
				t.Fatalf("Parse(%q) = %v", tt.block, err)
			}
			_, err = labels.Declared(mf.Lists, mf.Frontmatter)
			if err == nil {
				t.Fatalf("Declared(%q) = nil error, want one", tt.block)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Errorf("err = %q, want it to mention %q", err, tt.wantSubstr)
			}
		})
	}
}

// TestDeclaredReadsBothYAMLStyles closes the loop with internal/frontmatter:
// this package never sees the spelling, so a block list and a flow list must
// reach it identically.
func TestDeclaredReadsBothYAMLStyles(t *testing.T) {
	for _, block := range []string{
		"labels: [runbook, ci/cd]",
		"labels:\n  - runbook\n  - ci/cd",
	} {
		mf, err := frontmatter.Parse("doc.md", "---\n"+block+"\n---\nbody\n")
		if err != nil {
			t.Fatalf("Parse(%q) = %v", block, err)
		}
		set, err := labels.Declared(mf.Lists, mf.Frontmatter)
		if err != nil {
			t.Fatalf("Declared(%q) = %v", block, err)
		}
		if !set.Declared || !eq(set.Names, []string{"ci/cd", "runbook"}) {
			t.Errorf("%q gave Declared=%v Names=%q", block, set.Declared, set.Names)
		}
	}
}

// TestDeclaredRefusesAnInvalidLabel also pins *which spelling* the message
// quotes: the one in the file. Validating the lowercased name first reported
// `label "runbook two"` for a file that says "Runbook Two", sending an author
// to search for a string their file does not contain.
func TestDeclaredRefusesAnInvalidLabel(t *testing.T) {
	_, err := labels.Declared(map[string][]string{"labels": {"ok", "Runbook Two"}}, nil)
	if err == nil {
		t.Fatal("Declared = nil error, want the invalid label reported")
	}
	if !strings.Contains(err.Error(), "Runbook Two") {
		t.Errorf("err = %q, want it to name the label as written", err)
	}
}

// TestGlobalAndUnmanagedSplitByPrefix pins that markfluence manages one
// namespace. A my: label is personal and a team: one belongs to a space's own
// vocabulary; neither has a frontmatter spelling, so removing one would delete
// data the file could not have expressed.
func TestGlobalAndUnmanagedSplitByPrefix(t *testing.T) {
	live := []client.Label{
		{Name: "runbook", Prefix: "global"},
		{Name: "mine", Prefix: "my"},
		{Name: "eng", Prefix: "team"},
		{Name: "ci/cd", Prefix: "global"},
	}
	if got := labels.Global(live); !eq(got, []string{"ci/cd", "runbook"}) {
		t.Errorf("Global = %q, want [ci/cd runbook]", got)
	}
	if got := labels.Unmanaged(live); !eq(got, []string{"my:mine", "team:eng"}) {
		t.Errorf("Unmanaged = %q, want [my:mine team:eng]", got)
	}
}

func TestDiffIsSetArithmetic(t *testing.T) {
	add, remove, unchanged := labels.Diff(
		[]string{"howto", "runbook"},
		[]string{"runbook", "stale"},
	)
	if !eq(add, []string{"howto"}) {
		t.Errorf("add = %q, want [howto]", add)
	}
	if !eq(remove, []string{"stale"}) {
		t.Errorf("remove = %q, want [stale]", remove)
	}
	if !eq(unchanged, []string{"runbook"}) {
		t.Errorf("unchanged = %q, want [runbook]", unchanged)
	}
}

// TestDiffIgnoresOrderAndDuplicates: a file whose list is merely reordered
// needs no write at all, which is what lets fix leave an author's ordering and
// any duplicate alone.
func TestDiffIgnoresOrderAndDuplicates(t *testing.T) {
	add, remove, unchanged := labels.Diff(
		[]string{"runbook", "howto", "runbook"},
		[]string{"howto", "runbook"},
	)
	if len(add) != 0 || len(remove) != 0 {
		t.Errorf("add = %q remove = %q, want both empty", add, remove)
	}
	if !eq(unchanged, []string{"howto", "runbook"}) {
		t.Errorf("unchanged = %q", unchanged)
	}
}

// TestActionsReportTheWholeSet: the full declared set, not just the changes, so
// a consumer can read a page's labels off a publish without a second call.
func TestActionsReportTheWholeSet(t *testing.T) {
	got := labels.Actions([]string{"howto"}, []string{"stale"}, []string{"runbook"})
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := map[string]string{"howto": "added", "stale": "removed", "runbook": "unchanged"}
	for _, a := range got {
		if want[a.Name] != a.Action {
			t.Errorf("%s = %s, want %s", a.Name, a.Action, want[a.Name])
		}
	}
	// Sorted by name, since nothing upstream supplies a meaningful order.
	if got[0].Name != "howto" || got[1].Name != "runbook" || got[2].Name != "stale" {
		t.Errorf("order = %v, want sorted by name", got)
	}
}
