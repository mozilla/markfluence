// Package pagewidth models the page_width frontmatter field and its mapping to
// Confluence's content-appearance properties.
//
// Confluence stores a page's width as two content properties rather than on the
// page body. Authors express the width with the UI vocabulary -- narrow, wide,
// max -- which maps to the underlying property values:
//
//	page_width   content-property value
//	----------   ----------------------
//	narrow       default
//	wide         full-width
//	max          max
//
// An unset or blank page_width defaults to max: the markdown file is the source
// of truth for width, so publishing asserts it on both the published and draft
// appearance properties (so the viewed page and the editor agree).
//
// This package holds only the pure vocabulary logic. Applying and reading the
// properties over the network lives with the client and command layers.
package pagewidth

import (
	"fmt"
	"strings"
)

// The two content properties Confluence uses for page appearance/width. Both are
// written so the published view and the editor render the same width.
const (
	PublishedKey = "content-appearance-published"
	DraftKey     = "content-appearance-draft"
)

// Width is the page_width vocabulary an author writes in frontmatter.
type Width string

const (
	Narrow Width = "narrow"
	Wide   Width = "wide"
	Max    Width = "max"
)

// DefaultWidth is the width used when page_width is unset or blank.
const DefaultWidth = Max

// vocabToProperty maps a Width to its content-property value.
var vocabToProperty = map[Width]string{
	Narrow: "default",
	Wide:   "full-width",
	Max:    "max",
}

// propertyToVocab maps a content-property value back to a Width. It includes the
// legacy "fixed" value, surfaced as narrow.
var propertyToVocab = map[string]Width{
	"max":        Max,
	"full-width": Wide,
	"default":    Narrow,
	"fixed":      Narrow,
}

// Declared returns the Width a file declares, defaulting to Max. Unset or blank
// yields Max; a present but unrecognized value is an error.
func Declared(frontmatter map[string]string) (Width, error) {
	raw, ok := frontmatter["page_width"]
	if !ok || strings.TrimSpace(raw) == "" {
		return DefaultWidth, nil
	}
	value := Width(strings.ToLower(strings.TrimSpace(raw)))
	if _, valid := vocabToProperty[value]; !valid {
		return "", fmt.Errorf("invalid page_width %q; expected narrow, wide, or max", raw)
	}
	return value, nil
}

// PropertyValue returns the content-property value for w. It assumes w is a valid
// Width (e.g. the result of Declared); an unknown Width yields "".
func PropertyValue(w Width) string {
	return vocabToProperty[w]
}

// VocabFromPropertyValue reverse-maps a content-property value to a Width,
// falling back to Narrow for an unrecognized or empty value (Confluence's site
// default renders narrow).
func VocabFromPropertyValue(value string) Width {
	if w, ok := propertyToVocab[value]; ok {
		return w
	}
	return Narrow
}

// Property is a Confluence content property reduced to the fields width logic
// needs: its key and value.
type Property struct {
	Key   string
	Value string
}

// WidthFromProperties reads the effective Width from a page's content properties.
// It returns (width, explicit); explicit is false when the published appearance
// property isn't present (the page renders at Confluence's site default, which we
// surface as Narrow).
func WidthFromProperties(properties []Property) (width Width, explicit bool) {
	for _, p := range properties {
		if p.Key == PublishedKey {
			return VocabFromPropertyValue(p.Value), true
		}
	}
	return Narrow, false
}
