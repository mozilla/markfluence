// Package schema publishes markfluence's --json output contract: the JSON
// Schema documents under this directory, embedded in the binary.
//
// The Go file lives here, beside the schemas, because go:embed cannot reach
// outside its own package directory, and this is where the contract is
// published -- a top-level directory a non-Go consumer can browse or curl, with
// a path that mirrors the schema's own $id. Embedding rather than copying is
// what makes `markfluence schema` and the drift-guard tests
// (internal/schematest) read the same bytes: the shipped schema is the tested
// schema, with nothing to keep in sync.
//
// The version these documents describe is jsonout.SchemaVersion. It is not
// restated here: the number already lives in the schema document and in the code
// that emits it, and a third copy would be one more place to forget.
package schema

import _ "embed"

// V1 is schema/json-output/v1.json verbatim, newline-terminated -- the contract
// for schema_version 1, the only version so far. A v2 becomes a sibling var and
// a caller that chooses between them; until one exists there is nothing to
// choose.
//
//go:embed json-output/v1.json
var V1 string
