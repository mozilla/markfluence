// Package schematest is a test-only helper that validates markfluence's --json
// output against the published JSON Schema (schema/json-output/v1.json). It is
// the drift guard: any new field on a result struct fails validation until the
// schema is updated to match.
//
// It validates against the copy embedded in package schema -- the same bytes
// `markfluence schema` prints -- so what ships is what these tests checked. But
// not as published: the published schema is open, because a new key is a
// compatible change and a consumer holding an older copy must not reject one
// (#200, docs/json-output.md). The drift guard needs the opposite, so the
// tests validate against a closed copy (closeObjects), which forbids every key
// the schema does not list.
package schematest

import (
	"bytes"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/mozilla/markfluence/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaID = "https://github.com/mozilla/markfluence/schema/json-output/v1.json"

// compiled is the schema compiled one way: open (as published) or closed (as
// the drift guard validates). Each is compiled once per test binary.
type compiled struct {
	once      sync.Once
	envelope  *jsonschema.Schema
	errObject *jsonschema.Schema
	err       error
}

var openSchema, closedSchema compiled

// compile builds the envelope and error-object schemas from the embedded
// document, closed first when closed is set. The one path both variants take,
// so the test that checks the published schema and the drift guard cannot come
// to disagree about anything but the closing.
func compile(t *testing.T, closed bool) *compiled {
	t.Helper()
	c := &openSchema
	if closed {
		c = &closedSchema
	}
	c.once.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
		if err != nil {
			c.err = err
			return
		}
		if closed {
			closeObjects(doc)
		}
		comp := jsonschema.NewCompiler()
		if err := comp.AddResource(schemaID, doc); err != nil {
			c.err = err
			return
		}
		if c.envelope, err = comp.Compile(schemaID); err != nil {
			c.err = err
			return
		}
		c.errObject, c.err = comp.Compile(schemaID + "#/$defs/errorObject")
	})
	if c.err != nil {
		t.Fatalf("compiling JSON Schema: %v", c.err)
	}
	return c
}

// ValidateEnvelope fails the test if instance (a marshaled --json stdout
// document) does not conform to the envelope schema, closed.
func ValidateEnvelope(t *testing.T, instance []byte) {
	t.Helper()
	validate(t, compile(t, true).envelope, instance)
}

// ValidateError fails the test if instance (a marshaled stderr error object)
// does not conform to #/$defs/errorObject, closed.
func ValidateError(t *testing.T, instance []byte) {
	t.Helper()
	validate(t, compile(t, true).errObject, instance)
}

func validate(t *testing.T, sch *jsonschema.Schema, instance []byte) {
	t.Helper()
	if err := check(t, sch, instance); err != nil {
		t.Errorf("instance does not conform to schema:\n%v\n--- instance ---\n%s", err, instance)
	}
}

// check validates instance against sch and returns the result.
func check(t *testing.T, sch *jsonschema.Schema, instance []byte) error {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(instance))
	if err != nil {
		t.Fatalf("instance is not valid JSON: %v\n%s", err, instance)
	}
	return sch.Validate(v)
}

// instanceKeywords hold instance data rather than subschemas. A walk looking
// for schemas must not descend into them: a const that happened to be an
// object with "type" and "properties" keys is data, and closing it would
// change the value it requires.
var instanceKeywords = map[string]bool{"const": true, "enum": true, "default": true, "examples": true}

// namedSchemas hold a map from a name to a schema rather than a schema. The
// map itself is not visited as one: infoResult has a property called
// "properties", and read as a schema its properties map lists properties.
var namedSchemas = map[string]bool{"properties": true, "patternProperties": true, "$defs": true}

// eachSchema calls f on every subschema of doc -- a schema decoded by
// jsonschema.UnmarshalJSON -- with its JSON pointer, skipping instance data.
func eachSchema(doc any, path string, f func(path string, node map[string]any)) {
	switch n := doc.(type) {
	case map[string]any:
		f(path, n)
		for k, v := range n {
			switch {
			case instanceKeywords[k]:
			case namedSchemas[k]:
				if m, ok := v.(map[string]any); ok {
					for name, sub := range m {
						eachSchema(sub, path+"/"+k+"/"+name, f)
					}
				}
			default:
				eachSchema(v, path+"/"+k, f)
			}
		}
	case []any:
		for i, v := range n {
			eachSchema(v, path+"/"+strconv.Itoa(i), f)
		}
	}
}

// isObjectSchema reports whether node is a schema the drift guard closes: one
// declaring "type": "object" and listing its properties.
//
// The type is what keeps the closing off the envelope's if/then branches: an
// if closed this way would stop matching any real envelope, and a then closed
// this way would forbid every envelope key but results and summary. Neither
// declares a type. TestEveryPropertiesNodeIsTyped keeps every other node that
// lists properties typed, so nothing is left open by omission.
func isObjectSchema(node map[string]any) bool {
	_, hasProps := node["properties"]
	return hasProps && node["type"] == "object"
}

// closeObjects sets additionalProperties to false, in place, on every object
// schema in doc that does not already say something about it.
func closeObjects(doc any) {
	eachSchema(doc, "#", func(_ string, node map[string]any) {
		if _, said := node["additionalProperties"]; !said && isObjectSchema(node) {
			node["additionalProperties"] = false
		}
	})
}
