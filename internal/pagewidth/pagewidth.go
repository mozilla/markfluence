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
// The vocabulary logic is pure; Apply and Read orchestrate the underlying
// content-property calls against the client.
package pagewidth

import (
	"fmt"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
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

// Vocabulary lists the accepted widths, narrowest first. It is what
// --page-width completes to; Declared is what validates against it.
func Vocabulary() []string {
	return []string{string(Narrow), string(Wide), string(Max)}
}

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

// WidthFromProperties reads the effective Width from a page's content properties.
// It returns (width, explicit); explicit is false when the published appearance
// property isn't present (the page renders at Confluence's site default, which we
// surface as Narrow).
func WidthFromProperties(properties []client.Property) (width Width, explicit bool) {
	for _, p := range properties {
		if p.Key == PublishedKey {
			value, _ := p.Value.(string)
			return VocabFromPropertyValue(value), true
		}
	}
	return Narrow, false
}

// Action reports the outcome of setting one appearance property.
type Action struct {
	Key    string
	Action string // "set" or "unchanged"
}

// Apply sets both appearance properties for a page to match w. It returns one
// Action per property (in published, draft order).
func Apply(c *client.ConfluenceClient, pageID string, w Width) ([]Action, error) {
	value := PropertyValue(w)
	var actions []Action
	for _, key := range []string{PublishedKey, DraftKey} {
		result, err := c.SetContentProperty(pageID, key, value)
		if err != nil {
			return nil, err
		}
		actions = append(actions, Action{Key: key, Action: result})
	}
	return actions, nil
}

// Read returns a live page's Width from its published appearance property, plus
// whether it was explicitly set (false means Confluence's site default, surfaced
// as Narrow).
func Read(c *client.ConfluenceClient, pageID string) (width Width, explicit bool, err error) {
	prop, err := c.GetContentProperty(pageID, PublishedKey)
	if err != nil {
		return "", false, err
	}
	if prop == nil {
		return Narrow, false, nil
	}
	value, _ := prop.Value.(string)
	return VocabFromPropertyValue(value), true, nil
}
