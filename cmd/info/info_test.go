package info

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mozilla/markfluence/internal/client"
)

func TestRenderValue(t *testing.T) {
	if got := renderValue("max"); got != "max" {
		t.Errorf("renderValue(string) = %q, want max", got)
	}
	if got := renderValue(map[string]any{"version": "v2"}); got != `{"version":"v2"}` {
		t.Errorf("renderValue(map) = %q", got)
	}
	if got := renderValue(3); got != "3" {
		t.Errorf("renderValue(3) = %q, want 3", got)
	}
}

func TestRenderValueTruncatesLongValues(t *testing.T) {
	rendered := renderValue(strings.Repeat("x", 500))
	if n := utf8.RuneCountInString(rendered); n != valueMax {
		t.Errorf("rendered length = %d, want %d", n, valueMax)
	}
	if !strings.HasSuffix(rendered, "…") {
		t.Errorf("rendered = %q, want it to end with …", rendered)
	}
}

func TestPropertiesSectionSortsByKey(t *testing.T) {
	props := []client.Property{
		{Key: "editor", Value: "v2"},
		{Key: "content-appearance-published", Value: "max"},
	}
	want := "content properties:\n  content-appearance-published: max\n  editor: v2"
	if got := propertiesSection(props, nil); got != want {
		t.Errorf("propertiesSection =\n%q\nwant\n%q", got, want)
	}
}

func TestPropertiesSectionEmpty(t *testing.T) {
	if got := propertiesSection(nil, nil); got != "content properties: (none)" {
		t.Errorf("propertiesSection(empty) = %q", got)
	}
}

func TestPropertiesSectionFetchError(t *testing.T) {
	got := propertiesSection(nil, errors.New("boom"))
	if got != "content properties: (could not fetch: boom)" {
		t.Errorf("propertiesSection(error) = %q", got)
	}
}
