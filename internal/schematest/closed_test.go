package schematest

import (
	"strconv"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// published compiles the schema exactly as it ships, open, which is what a
// consumer validates against.
func published(t *testing.T) *jsonschema.Schema {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(schemaID, doc); err != nil {
		t.Fatal(err)
	}
	sch, err := c.Compile(schemaID)
	if err != nil {
		t.Fatal(err)
	}
	return sch
}

func mustValidate(t *testing.T, sch *jsonschema.Schema, instance string) error {
	t.Helper()
	v, err := jsonschema.UnmarshalJSON(strings.NewReader(instance))
	if err != nil {
		t.Fatal(err)
	}
	return sch.Validate(v)
}

// walk calls f on every object node of a decoded schema, with its JSON pointer.
func walk(n any, path string, f func(path string, node map[string]any)) {
	switch m := n.(type) {
	case map[string]any:
		f(path, m)
		for k, v := range m {
			walk(v, path+"/"+k, f)
		}
	case []any:
		for i, v := range m {
			walk(v, path+"/"+strconv.Itoa(i), f)
		}
	}
}

// TestPublishedSchemaIsOpen pins the consumer half of #200: a key closed in
// the published file is a key no later release may add without breaking
// whoever validates against the copy they have.
func TestPublishedSchemaIsOpen(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
	if err != nil {
		t.Fatal(err)
	}
	walk(doc, "#", func(path string, node map[string]any) {
		if _, ok := node["additionalProperties"]; ok {
			t.Errorf("%s sets additionalProperties; the published schema must stay open "+
				"(the tests close it: see Closed)", path)
		}
	})
}

// TestClosedClosesObjectSchemasOnly: every object schema listing properties
// is closed, and the envelope's if/then branches -- which declare no type --
// are not. Closing a then would forbid every envelope key but results and
// summary; closing an if would stop it matching anything.
func TestClosedClosesObjectSchemasOnly(t *testing.T) {
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
	if err != nil {
		t.Fatal(err)
	}
	closed := 0
	walk(Closed(doc), "#", func(path string, node map[string]any) {
		_, hasProps := node["properties"]
		ap, said := node["additionalProperties"]
		switch {
		case hasProps && node["type"] == "object":
			if ap != false {
				t.Errorf("%s: object schema not closed", path)
			}
			closed++
		case said:
			t.Errorf("%s: closed, but it is not an object schema that lists properties", path)
		}
	})
	if closed == 0 {
		t.Fatal("closed nothing")
	}
}

// exportEnvelope is a valid export envelope whose one row is a failed export,
// with extra added to that row.
func exportEnvelope(extra string) string {
	return `{"schema_version": 1, "markfluence_version": "dev", "command": "export",
		"roots": [], "warnings": [],
		"results": [{"ok": false, "page_id": "123", "title": "", "space": "", "parent": null,
			"parent_type": null, "parent_file": null, "dry_run": false, "status": "",
			"dest_path": null, "attachments": [], "warnings": [],
			"error": "boom", "code": "NETWORK"` + extra + `}],
		"summary": {"total": 1, "succeeded": 0, "failed": 1, "skipped": 0, "project_file": null}}`
}

// TestAnUnlistedKeyIsCompatible pins both halves at once: the drift guard
// refuses a key the schema does not list, and the published schema accepts
// it, which is what makes adding a key a compatible change.
func TestAnUnlistedKeyIsCompatible(t *testing.T) {
	doc := exportEnvelope(`, "brand_new": 1`)
	load(t)
	if err := mustValidate(t, envelope, doc); err == nil {
		t.Error("the closed schema accepted an unlisted key; the drift guard is off")
	}
	if err := mustValidate(t, published(t), doc); err != nil {
		t.Errorf("the published schema refused an unlisted key: %v", err)
	}
}

// TestFailedExportRowIsValid is the union #200 found. A failed export row has
// every key singleOpFailure requires, so once objects are open it matches
// both shapes, and a oneOf refuses a row that matches two.
func TestFailedExportRowIsValid(t *testing.T) {
	doc := exportEnvelope("")
	if err := mustValidate(t, published(t), doc); err != nil {
		t.Errorf("published schema: %v", err)
	}
	ValidateEnvelope(t, []byte(doc))
}
