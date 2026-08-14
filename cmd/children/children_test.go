package children

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/pagetree"
)

// TestParseDepth covers the flag's whole vocabulary. 0 is the interesting case:
// it is a common spelling of "unlimited" elsewhere, so accepting it would launch
// an unbounded walk for someone who may have meant the opposite.
func TestParseDepth(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    int
		wantErr bool
	}{
		{in: "1", want: 1},
		{in: "3", want: 3},
		{in: "all", want: pagetree.AllDepths},
		{in: "0", wantErr: true},
		{in: "-1", wantErr: true},
		{in: "", wantErr: true},
		{in: "deep", wantErr: true},
		{in: "1.5", wantErr: true},
		{in: "ALL", wantErr: true}, // the vocabulary is lowercase, like page_width's
	} {
		got, err := parseDepth(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseDepth(%q) = %d, want an error", tc.in, got)
				continue
			}
			// The message has to name the value that does mean unlimited, or a
			// caller who tried 0 has nothing to go on.
			if !strings.Contains(err.Error(), `"all"`) {
				t.Errorf("parseDepth(%q) error = %q, must mention \"all\"", tc.in, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDepth(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Errorf("parseDepth(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// TestTreeIndentsByDepth pins the shape of the human output: titles indent so the
// hierarchy is visible, while TYPE and ID stay aligned so it is still greppable.
func TestTreeIndentsByDepth(t *testing.T) {
	got := tree([]pagetree.Node{
		{ID: "11", Type: "page", Title: "Alpha", Depth: 1},
		{ID: "2222", Type: "folder", Title: "Articles", Depth: 1},
		{ID: "33", Type: "page", Title: "Inside", Depth: 2},
		{ID: "44", Type: "page", Title: "Deeper", Depth: 3},
	})
	want := strings.Join([]string{
		"TYPE    ID    TITLE",
		"page    11    Alpha",
		"folder  2222  Articles",
		"page    33      Inside",
		"page    44        Deeper",
	}, "\n")
	if got != want {
		t.Errorf("tree mismatch:\n got:\n%s\n want:\n%s", got, want)
	}
}

func TestTreeHasNoTrailingSpaces(t *testing.T) {
	got := tree([]pagetree.Node{{ID: "11", Type: "page", Title: "Alpha", Depth: 1}})
	for i, line := range strings.Split(got, "\n") {
		if strings.TrimRight(line, " ") != line {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}
