package schematest

// This file introspects the schema *document*, as opposed to validating an
// instance against it. The document has a structural weakness worth guarding:
// at the top level, results is only {"type": "array"} and summary only
// {"type": "object"}. Every real constraint on a result item lives in an
// `if command == "X" then ...` branch of allOf.
//
// So a command name that is in the command enum but has no branch emits
// completely unvalidated output -- any keys, any shapes, and ValidateEnvelope
// passes. That is not a hypothetical mistake: a new command's conformance test
// fails first on the command enum, which makes "add the name to the enum" the
// obvious way to get to green, and stopping there buys no validation at all.

import (
	"encoding/json"
	"testing"

	"github.com/mozilla/markfluence/schema"
)

// document is the part of the schema this package reads. Fields absent from the
// JSON stay nil, which is what the checks below test for.
type document struct {
	Properties struct {
		Command struct {
			Enum []string `json:"enum"`
		} `json:"command"`
	} `json:"properties"`
	AllOf []struct {
		If struct {
			Properties struct {
				Command struct {
					Const string `json:"const"`
				} `json:"command"`
			} `json:"properties"`
		} `json:"if"`
		Then struct {
			Properties struct {
				Results *struct {
					Items json.RawMessage `json:"items"`
				} `json:"results"`
				Summary json.RawMessage `json:"summary"`
			} `json:"properties"`
		} `json:"then"`
	} `json:"allOf"`
}

func parseDocument(t *testing.T) document {
	t.Helper()
	var doc document
	if err := json.Unmarshal([]byte(schema.V1), &doc); err != nil {
		t.Fatalf("unmarshaling the schema document: %v", err)
	}
	return doc
}

// Commands returns the command names the schema's top-level command enum
// accepts. A command that emits a --json envelope must be one of these, so
// callers can check the CLI's own command list against the contract.
func Commands(t *testing.T) []string {
	t.Helper()
	doc := parseDocument(t)
	if len(doc.Properties.Command.Enum) == 0 {
		t.Fatal("schema has no command enum; the drift guards below rest on it")
	}
	return doc.Properties.Command.Enum
}
