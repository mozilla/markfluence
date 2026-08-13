package schema

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	schemadoc "github.com/mozilla/markfluence/schema"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// run executes the command with a captured writer and returns what it printed.
func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	Cmd.SetOut(&out)
	Cmd.SetErr(&out)
	Cmd.SetArgs(args)
	t.Cleanup(func() {
		Cmd.SetOut(nil)
		Cmd.SetErr(nil)
		Cmd.SetArgs(nil)
	})
	if err := Cmd.Execute(); err != nil {
		t.Fatalf("schema %v: %v", args, err)
	}
	return out.String()
}

// TestPrintsSchemaVerbatim pins the whole point of the command: a consumer can
// diff or cache what it prints against the published file, so the bytes must
// match the embedded schema exactly.
func TestPrintsSchemaVerbatim(t *testing.T) {
	got := runCmd(t)
	if got != schemadoc.V1 {
		t.Errorf("output is not the embedded schema verbatim (%d bytes vs %d)",
			len(got), len(schemadoc.V1))
	}
	if !strings.HasSuffix(got, "}\n") {
		t.Errorf("output should be a newline-terminated JSON document, got tail %q",
			got[max(0, len(got)-10):])
	}
}

// TestOutputIsAUsableSchema checks the embedded document is a JSON Schema that
// compiles, not merely valid JSON: a truncated or mangled embed would still
// parse as an object.
func TestOutputIsAUsableSchema(t *testing.T) {
	out := runCmd(t)

	var doc struct {
		Schema string `json:"$schema"`
		ID     string `json:"$id"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if !strings.Contains(doc.Schema, "json-schema.org") {
		t.Errorf("$schema = %q, want a json-schema.org draft URI", doc.Schema)
	}
	if want := "https://github.com/mozilla/markfluence/schema/json-output/v1.json"; doc.ID != want {
		t.Errorf("$id = %q, want %q", doc.ID, want)
	}

	instance, err := jsonschema.UnmarshalJSON(strings.NewReader(out))
	if err != nil {
		t.Fatalf("unmarshaling for compile: %v", err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(doc.ID, instance); err != nil {
		t.Fatalf("adding schema resource: %v", err)
	}
	if _, err := c.Compile(doc.ID); err != nil {
		t.Fatalf("printed schema does not compile: %v", err)
	}
}

// TestSchemaVersionMatchesDocument keeps the constant the help text quotes tied
// to the schema_version the document itself pins.
func TestSchemaVersionMatchesDocument(t *testing.T) {
	var doc struct {
		Properties struct {
			SchemaVersion struct {
				Const int `json:"const"`
			} `json:"schema_version"`
		} `json:"properties"`
	}
	if err := json.Unmarshal([]byte(schemadoc.Latest), &doc); err != nil {
		t.Fatalf("unmarshaling schema: %v", err)
	}
	if got := doc.Properties.SchemaVersion.Const; got != schemadoc.Version {
		t.Errorf("schema pins schema_version %d, schema.Version = %d", got, schemadoc.Version)
	}
}

// TestRejectsArguments guards the completion promise: the command takes none.
func TestRejectsArguments(t *testing.T) {
	var out bytes.Buffer
	Cmd.SetOut(&out)
	Cmd.SetErr(&out)
	Cmd.SetArgs([]string{"v1"})
	t.Cleanup(func() {
		Cmd.SetOut(nil)
		Cmd.SetErr(nil)
		Cmd.SetArgs(nil)
	})
	if err := Cmd.Execute(); err == nil {
		t.Error("schema v1 should be a usage error")
	}
}
