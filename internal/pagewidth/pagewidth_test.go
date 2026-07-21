package pagewidth_test

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/pagewidth"
)

func TestDeclaredDefaultsToMaxWhenUnsetOrBlank(t *testing.T) {
	for _, fm := range []map[string]string{
		{},
		{"page_width": ""},
		{"page_width": "   "},
	} {
		got, err := pagewidth.Declared(fm)
		if err != nil {
			t.Fatalf("Declared(%v) error: %v", fm, err)
		}
		if got != pagewidth.Max {
			t.Errorf("Declared(%v) = %q, want %q", fm, got, pagewidth.Max)
		}
	}
}

func TestDeclaredNormalizesCaseAndWhitespace(t *testing.T) {
	tests := map[string]pagewidth.Width{
		"Wide":   pagewidth.Wide,
		"  MAX ": pagewidth.Max,
	}
	for raw, want := range tests {
		got, err := pagewidth.Declared(map[string]string{"page_width": raw})
		if err != nil {
			t.Fatalf("Declared(%q) error: %v", raw, err)
		}
		if got != want {
			t.Errorf("Declared(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestDeclaredRejectsUnknownValue(t *testing.T) {
	_, err := pagewidth.Declared(map[string]string{"page_width": "mx"})
	if err == nil {
		t.Fatal("Declared(mx) = nil error, want an error")
	}
	if got := err.Error(); !strings.Contains(got, "invalid page_width") {
		t.Errorf("error = %q, want it to mention %q", got, "invalid page_width")
	}
}

func TestPropertyValueRoundTrip(t *testing.T) {
	tests := map[pagewidth.Width]string{
		pagewidth.Narrow: "default",
		pagewidth.Wide:   "full-width",
		pagewidth.Max:    "max",
	}
	for w, want := range tests {
		if got := pagewidth.PropertyValue(w); got != want {
			t.Errorf("PropertyValue(%q) = %q, want %q", w, got, want)
		}
	}
}

func TestVocabFromPropertyValue(t *testing.T) {
	tests := map[string]pagewidth.Width{
		"max":        pagewidth.Max,
		"full-width": pagewidth.Wide,
		"default":    pagewidth.Narrow,
		"fixed":      pagewidth.Narrow, // legacy value surfaces as narrow
		"nonsense":   pagewidth.Narrow, // unknown falls back to narrow
		"":           pagewidth.Narrow,
	}
	for value, want := range tests {
		if got := pagewidth.VocabFromPropertyValue(value); got != want {
			t.Errorf("VocabFromPropertyValue(%q) = %q, want %q", value, got, want)
		}
	}
}

func TestWidthFromProperties(t *testing.T) {
	present := []pagewidth.Property{
		{Key: pagewidth.PublishedKey, Value: "full-width"},
		{Key: "editor", Value: "v2"},
	}
	if w, explicit := pagewidth.WidthFromProperties(present); w != pagewidth.Wide || !explicit {
		t.Errorf("WidthFromProperties(present) = (%q, %v), want (wide, true)", w, explicit)
	}

	absent := []pagewidth.Property{{Key: "editor", Value: "v2"}}
	if w, explicit := pagewidth.WidthFromProperties(absent); w != pagewidth.Narrow || explicit {
		t.Errorf("WidthFromProperties(absent) = (%q, %v), want (narrow, false)", w, explicit)
	}
}
