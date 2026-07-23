package create

import (
	"testing"

	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/pagewidth"
)

func TestResolveTitle(t *testing.T) {
	mf := frontmatter.Parse("f.md", "---\ntitle: FM Title\n---\nb\n")
	if got := resolveTitle("CLI Title", mf); got != "CLI Title" {
		t.Errorf("flag override = %q, want CLI Title", got)
	}
	if got := resolveTitle("", mf); got != "FM Title" {
		t.Errorf("frontmatter = %q, want FM Title", got)
	}
	empty := frontmatter.Parse("f.md", "body, no frontmatter\n")
	if got := resolveTitle("", empty); got != "" {
		t.Errorf("absent = %q, want empty", got)
	}
}

func TestResolveWidth(t *testing.T) {
	withFM := map[string]string{"page_width": "wide"}
	t.Run("flag overrides frontmatter", func(t *testing.T) {
		if w, err := resolveWidth("narrow", withFM); err != nil || w != pagewidth.Narrow {
			t.Fatalf("= %q/%v, want narrow/nil", w, err)
		}
	})
	t.Run("frontmatter when no flag", func(t *testing.T) {
		if w, err := resolveWidth("", withFM); err != nil || w != pagewidth.Wide {
			t.Fatalf("= %q/%v, want wide/nil", w, err)
		}
	})
	t.Run("defaults to max when unset", func(t *testing.T) {
		if w, err := resolveWidth("", map[string]string{}); err != nil || w != pagewidth.Max {
			t.Fatalf("= %q/%v, want max/nil", w, err)
		}
	})
	t.Run("invalid flag errors", func(t *testing.T) {
		if _, err := resolveWidth("huge", map[string]string{}); err == nil {
			t.Fatal("want error for invalid --page-width")
		}
	})
}

func TestWantPersist(t *testing.T) {
	tests := []struct {
		name               string
		persist, noPersist bool
		want               bool
	}{
		{"default", true, false, true},
		{"no-persist", true, true, false},
		{"persist=false", false, false, false},
		{"both off", false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := wantPersist(tc.persist, tc.noPersist); got != tc.want {
				t.Errorf("wantPersist(%v,%v) = %v, want %v", tc.persist, tc.noPersist, got, tc.want)
			}
		})
	}
}

func TestOverrideNeedsSingleFile(t *testing.T) {
	if !overrideNeedsSingleFile("T", 2) {
		t.Error("--title with 2 files should require single FILE")
	}
	if overrideNeedsSingleFile("T", 1) {
		t.Error("--title with 1 file is fine")
	}
	if overrideNeedsSingleFile("", 3) {
		t.Error("no --title should not trigger the guard")
	}
}
