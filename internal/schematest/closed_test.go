package schematest

import (
	"strings"
	"testing"

	"github.com/mozilla/markfluence/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

func decoded(t *testing.T) any {
	t.Helper()
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(schema.V1))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// TestPublishedSchemaIsOpen pins the consumer half of #200: an object the
// published file closes is one no later release may add a key to without
// breaking whoever validates against the copy they have. Any other
// additionalProperties (a schema for a map's values, say) is fine.
func TestPublishedSchemaIsOpen(t *testing.T) {
	eachSchema(decoded(t), "#", func(path string, node map[string]any) {
		if node["additionalProperties"] == false {
			t.Errorf("%s sets additionalProperties: false; the published schema must stay open "+
				"(the tests close it: see closeObjects)", path)
		}
	})
}

// TestEveryPropertiesNodeIsTyped keeps the closing complete. closeObjects
// closes only a node declaring "type": "object", so a nested object written
// without a type would be left open, and a stray key on it would pass the
// drift guard with every test green. The one legitimate untyped node listing
// properties is an envelope if/then branch.
func TestEveryPropertiesNodeIsTyped(t *testing.T) {
	eachSchema(decoded(t), "#", func(path string, node map[string]any) {
		if _, ok := node["properties"]; !ok || isObjectSchema(node) {
			return
		}
		if strings.HasPrefix(path, "#/allOf/") &&
			(strings.HasSuffix(path, "/if") || strings.HasSuffix(path, "/then")) {
			return
		}
		t.Errorf(`%s lists properties without "type": "object", so the drift guard leaves it open`, path)
	})
}

// TestCloseObjectsClosesObjectSchemasOnly: every object schema is closed, and
// nothing else is -- the if/then branches least of all.
func TestCloseObjectsClosesObjectSchemasOnly(t *testing.T) {
	doc := decoded(t)
	closeObjects(doc)
	closed := 0
	eachSchema(doc, "#", func(path string, node map[string]any) {
		ap, said := node["additionalProperties"]
		switch {
		case isObjectSchema(node):
			if ap != false {
				t.Errorf("%s: object schema not closed", path)
			}
			closed++
		case said:
			t.Errorf("%s: closed, but it is not an object schema", path)
		}
	})
	if closed == 0 {
		t.Fatal("closed nothing")
	}
}

// TestCloseObjectsLeavesInstanceDataAlone: a const is a value, and closing an
// object-shaped one would change what it requires.
func TestCloseObjectsLeavesInstanceDataAlone(t *testing.T) {
	doc := map[string]any{
		"type":       "object",
		"properties": map[string]any{"x": map[string]any{}},
		"const":      map[string]any{"type": "object", "properties": map[string]any{}},
	}
	closeObjects(doc)
	if doc["additionalProperties"] != false {
		t.Error("the schema itself was not closed")
	}
	if _, said := doc["const"].(map[string]any)["additionalProperties"]; said {
		t.Error("closeObjects wrote into a const")
	}
}

// exportEnvelope is a valid export envelope whose one row is a failed export,
// with extra added to that row.
func exportEnvelope(extra string) []byte {
	return []byte(`{"schema_version": 1, "markfluence_version": "dev", "command": "export",
		"roots": [], "warnings": [],
		"results": [{"ok": false, "page_id": "123", "title": "", "space": "", "parent": null,
			"parent_type": null, "parent_file": null, "dry_run": false, "status": "",
			"dest_path": null, "attachments": [], "warnings": [],
			"error": "boom", "code": "NETWORK"` + extra + `}],
		"summary": {"total": 1, "succeeded": 0, "failed": 1, "skipped": 0, "project_file": null}}`)
}

// TestAnUnlistedKeyIsCompatible pins both halves at once: the drift guard
// refuses a key the schema does not list, and the published schema accepts
// it, which is what makes adding a key a compatible change.
func TestAnUnlistedKeyIsCompatible(t *testing.T) {
	doc := exportEnvelope(`, "brand_new": 1`)
	if err := check(t, compile(t, true).envelope, doc); err == nil {
		t.Error("the closed schema accepted an unlisted key; the drift guard is off")
	}
	if err := check(t, compile(t, false).envelope, doc); err != nil {
		t.Errorf("the published schema refused an unlisted key: %v", err)
	}
}

// TestFailedExportRowIsValid is the union #200 found. A failed export row has
// every key singleOpFailure requires, so once objects are open it matches
// both shapes, and a oneOf refuses a row that matches two.
func TestFailedExportRowIsValid(t *testing.T) {
	doc := exportEnvelope("")
	if err := check(t, compile(t, false).envelope, doc); err != nil {
		t.Errorf("published schema: %v", err)
	}
	ValidateEnvelope(t, doc)
}
