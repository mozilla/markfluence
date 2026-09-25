# 054: adding a key is a compatible schema change

Answers #200. v0.1.0 is released and `--json` has consumers:
`mozilla/markfluence-action` exposes the envelope to later workflow steps as
its `results-json` output. `docs/json-output.md` says a change that breaks
compatibility increases `schema_version`, but not what breaks it. And the
published schema sets `additionalProperties: false` on every object, so a
consumer validating against a copy of the v0.1.0 schema rejects any new key:
adding a field is breaking in practice. #10 needs one (`update`'s `moved`), so
this comes first.

## What the survey established

- `additionalProperties: false` appears 48 times, always on a node with
  `"type": "object"` and `properties`, and every such node has it. So "close
  every typed object that lists properties" reproduces the current schema
  exactly. The `if`/`then` branches carry no `type`, and neither does the
  envelope's per-command `then`, so neither is closed; they constrain
  `results` and `summary`, which the envelope's own `properties` declare.
- **Opening the objects breaks three unions.** Eight `results.items` are a
  `oneOf` of a command's result and `singleOpFailure`. Five tell them apart by
  `ok: const true` against `ok: const false`. The other three
  (`exportResult`, `attachmentUploadResult`, `attachmentDownloadResult`)
  carry `ok: boolean` and rely on closed objects to be exclusive.
  `exportResult` declares every key `singleOpFailure` requires, so with open
  objects a failed export row matches both, and `oneOf` fails. The two
  attachment results lack `page_id` only, so they would become ambiguous the
  day one gains it.

## Decisions

**D1. The compatibility rule, in `docs/json-output.md`.**

- *Compatible*, and `schema_version` stays: a new key on the envelope, a
  result, a summary, or the error object; a new value in an enum the schema
  documents as open (none today); loosening a constraint.
- *Breaking*, and `schema_version` goes up: removing or renaming a key;
  changing a key's type, or what it means; making a key nullable that was
  not; a new value in a closed enum (`code`, `status`, `command`...).
- Consumers must ignore keys they do not know, and should validate against
  the schema that `markfluence schema` prints, which is the running binary's.

A new `command` enum value counts as breaking for the rule's sake, since a
consumer switching on `command` meets a value it has no case for. In practice
a new command is new output nobody consumed before; that is judged when it
happens, not here.

**D2. The published schema is open.** Remove every `additionalProperties:
false` from `schema/json-output/v1.json`. Every document valid before stays
valid, so this is itself compatible.

**D3. The unions become `anyOf`.** All eight result-or-failure unions change
from `oneOf` to `anyOf`. `anyOf` is looser than `oneOf`, so this is
compatible too, and it is what an open schema needs: "matches at least one
shape" is the claim a consumer can rely on. The scalar unions
(`stringOrNull`, `codeOrNull`) stay `oneOf`; their branches cannot overlap.

**D4. The tests close it again.** `internal/schematest` compiles a *closed*
copy: it walks the embedded schema and sets `additionalProperties: false` on
every node with `"type": "object"` and `properties` that does not already say
otherwise. `ValidateEnvelope`/`ValidateError` validate against that, so the
drift guard is unchanged: a key the code emits and the schema does not list
still fails. The closing lives in one exported function (`Closed`) so a test
can check it directly.

**D5. The embedded schema is still what ships.** `markfluence schema` prints
the open document, byte for byte the file. The closing happens in memory, in
test code only.

## Implementation

- Before editing the schema, a one-off check: strip every
  `additionalProperties: false` from the current file, run `Closed` on the
  result, and compare with the original as parsed JSON. They must be equal.
  That is the proof that D4 restores exactly what D2 removes; the
  permanent tests below keep it true.
- Then the schema edit, done by a script (strip the key; `oneOf` → `anyOf`
  in the eight unions), with the formatting kept.

## Files

| file | change |
|---|---|
| `schema/json-output/v1.json` | D2, D3; the top-level `description` says the schema is open and why |
| `internal/schematest/schematest.go` | D4: `Closed`, compile the closed copy; package doc |
| `internal/schematest/document_test.go` (or a new test file) | tests below |
| `docs/json-output.md` | D1 |
| `CLAUDE.md` | the `schematest` and `schema/` bullets |
| code comments naming `additionalProperties:false` (`cmd/diff`, `cmd/search`, `cmd/userfind`, `cmd/spaceinfo`, `internal/jsonout`) | say "the closed schema the tests validate against" |

## Tests

- The published schema contains no `additionalProperties: false`.
- `Closed` closes every typed object that lists properties, and nothing else
  (the `if`/`then` nodes stay open).
- A document with an extra key on a result fails `ValidateEnvelope` and passes
  the published schema. That pins both halves: the drift guard, and the
  consumer promise.
- A failed export row validates against the published schema (the union
  ambiguity D3 fixes). It fails today with objects opened and `oneOf` kept.
- Every existing conformance test passes unchanged.

## Not in scope

- **A test comparing against the released schema** (fetching v0.1.0's file
  and checking no key was removed or retyped). `required` already catches a
  removed key from our side; a check across releases waits until a breaking
  change slips through.
- **A `schema_version` bump.** Nothing here breaks a consumer.
