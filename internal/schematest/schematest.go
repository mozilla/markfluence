// Package schematest is a test-only helper that validates markfluence's --json
// output against the published JSON Schema (schema/json-output/v1.json). It is
// the drift guard: because the schema uses additionalProperties:false
// throughout, any new field on a result struct fails validation until the schema
// is updated to match.
//
// It validates against the copy embedded in package schema -- the same bytes
// `markfluence schema` prints -- so what ships is what these tests checked.
package schematest

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/mozilla/markfluence/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const schemaID = "https://github.com/mozilla/markfluence/schema/json-output/v1.json"

var (
	once       sync.Once
	envelope   *jsonschema.Schema
	errObject  *jsonschema.Schema
	compileErr error
)

func compile() {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
	if err != nil {
		compileErr = err
		return
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaID, doc); err != nil {
		compileErr = err
		return
	}
	if envelope, err = c.Compile(schemaID); err != nil {
		compileErr = err
		return
	}
	errObject, err = c.Compile(schemaID + "#/$defs/errorObject")
	compileErr = err
}

func load(t *testing.T) {
	t.Helper()
	once.Do(compile)
	if compileErr != nil {
		t.Fatalf("compiling JSON Schema: %v", compileErr)
	}
}

// ValidateEnvelope fails the test if instance (a marshaled --json stdout
// document) does not conform to the envelope schema.
func ValidateEnvelope(t *testing.T, instance []byte) {
	t.Helper()
	load(t)
	validate(t, envelope, instance)
}

// ValidateError fails the test if instance (a marshaled stderr error object)
// does not conform to #/$defs/errorObject.
func ValidateError(t *testing.T, instance []byte) {
	t.Helper()
	load(t)
	validate(t, errObject, instance)
}

func validate(t *testing.T, sch *jsonschema.Schema, instance []byte) {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(instance))
	if err != nil {
		t.Fatalf("instance is not valid JSON: %v\n%s", err, instance)
	}
	if err := sch.Validate(v); err != nil {
		t.Errorf("instance does not conform to schema:\n%v\n--- instance ---\n%s", err, instance)
	}
}
