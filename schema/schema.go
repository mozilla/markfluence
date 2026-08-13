// Package schema publishes markfluence's --json output contract: the JSON
// Schema documents under this directory, embedded in the binary.
//
// The Go file lives here, beside the schemas, because go:embed cannot reach
// outside its own package directory and the schema path is itself published --
// consumers link to schema/json-output/v1.json. Embedding rather than copying is
// what makes `markfluence schema` and the drift-guard tests
// (internal/schematest) read the same bytes: the shipped schema is the tested
// schema, with nothing to keep in sync.
package schema

import _ "embed"

// Version is the schema_version the embedded schema describes, matching the
// "schema_version" field of every --json document markfluence emits.
const Version = 1

// V1 is schema/json-output/v1.json verbatim, newline-terminated. It is the
// contract for schema_version 1.
//
//go:embed json-output/v1.json
var V1 string

// Latest is the schema for the current Version. When a v2 lands, this moves and
// the callers that mean "whatever markfluence emits today" follow along.
var Latest = V1
