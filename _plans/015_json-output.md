# Plan: `--json` machine-readable output

Add a global `--json` mode across all five subcommands (`info`, `read`, `update`,
`create`, `fix`) so markfluence output can be consumed by scripts and CI. Closes
issue #12 ("Add --json output for commands").

`--json` suppresses the human `internal/ui` output and writes a single structured
JSON document to stdout. Warnings and broken-image/link notices — currently
`ui.Warn` lines on stderr — become **data** in the payload (issue #12 lists them
as schema fields). Fatal/pre-flight errors stay on stderr, but as a typed JSON
error object when `--json` is set.

Reference: pchuri/confluence-cli's `--json` (global flag, stdout-stays-valid-JSON,
typed error codes on stderr). We diverge deliberately in two places: we wrap
output in an **envelope** (it has no envelope), and we fold warnings/broken into
the **payload** (it logs them to stderr) — because markfluence's warnings are
per-page conversion results, not incidental log chatter.

## Decisions locked (from the interview)

- **Scope:** all five subcommands, including `read`.
- **Flag:** one persistent `--json` bool on `rootCmd`, inherited by every
  subcommand (like `--debug`/`--no-color`). Bare `markfluence --json` (help) is
  unaffected — `--json` only shapes subcommand output.
- **Envelope:** uniform for every command, including single-target `info`/`read`
  (their `results` is a 1-element array).
- **Field presence:** stable schema means **per-command stable** — each command
  always emits the same keys with the same shapes (empty → `null`/`[]`); the key
  *set* differs between commands.
- **Compound values are nested objects**, never human display strings.
- **Status verbs are per-command** (`published`/`created`/`changed`, etc.).
- **create phase-1 abort:** every input file appears in `results`; summary carries
  `aborted:true`.
- **Refactor depth:** full — `processFile`/`createOne` build a typed result
  struct; a human renderer and the JSON collector both consume it (single source
  of truth).
- **Errors:** per-file operational failures live in `results`; fatal/pre-flight
  errors go to stderr as a typed error object. Typed error codes, markfluence-
  tailored set.
- **Formatting:** pretty-printed, 2-space indent, newline-terminated.
- **Exit codes:** `0` ok, `1` operational failure, `2` config/usage/pre-flight.

## Architecture

### `internal/jsonout` (new package)

Holds the shared machinery so no command re-implements it:

- `Envelope` — `{schema_version, markfluence_version, command, results, summary}`.
  `results` is `[]any`; `summary` is `any` (command-specific).
- `ErrorObject` — `{schema_version, command, error, code}` for the fatal path.
- `Code` constants: `CONFIG`, `AUTH`, `NOT_FOUND`, `VALIDATION`, `CONVERT`, `IO`,
  `NETWORK`, `API`.
- `Emit(w io.Writer, env Envelope) error` — marshal indented (2-space), trailing
  newline, to stdout.
- `EmitError(w io.Writer, command, msg string, code Code) error` — the stderr
  error object.
- A helper to derive a `Code` from a `client.HTTPError` (401/403→`AUTH`,
  404→`NOT_FOUND`, other→`API`) with `NETWORK` for transport errors.

`schema_version` is the integer `1` (bump on breaking change).
`markfluence_version` is `buildinfo.Stamp` (aids bug reports).

### `internal/ui`

- `SetJSON(bool)` — when set, the stdout helpers (`Header`/`Success`/`Info`/`Dim`)
  become no-ops, a belt-and-suspenders guard so a stray call can't corrupt the
  JSON on stdout. `ui.Warn`/`ui.Error` also stop writing in JSON mode (their
  content is carried in the payload / error object instead). `ui.Debug` is
  unaffected — still stderr, still `--debug`-gated.
- Wired from `rootCmd.PersistentPreRunE` next to `SetDebug`.

### Command refactor (the bulk of the diff)

Each command's per-file worker builds a typed result struct instead of printing
inline and returning `bool`:

- `update.processFile` / `create.createOne` / `fix.processFile` → return a result
  struct (and an in-band error/status).
- A **human renderer** re-derives today's `ui.*` lines from that struct, so
  human-mode output is byte-identical to now.
- A **JSON collector** appends structs to `Envelope.results` and prints once at
  the end.

Human-mode output must not regress; the renderer is exercised by the existing
command behavior.

## Envelope

```json
{
  "schema_version": 1,
  "markfluence_version": "1.4.0",
  "command": "update",
  "results": [ /* per-command result objects */ ],
  "summary": { "total": 2, "succeeded": 1, "failed": 1 }
}
```

Summary core is `{total, succeeded, failed}`; commands add extras (below).

## Per-command result shapes

### update — `status`: `published` | `skipped` | `failed`

```json
{ "ok": true, "status": "published", "file": "docs/foo.md",
  "page_id": "123", "title": "Foo", "space": "ENG",
  "url": "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
  "version": { "previous": 3, "new": 4 },
  "page_width": { "value": "max", "default": false },
  "attachments": [ { "action": "updated", "filename": "diagram.png" } ],
  "warnings": [], "broken": [], "error": null, "code": null }
```

- `skipped` (mtime unchanged): `ok:true`, `version.new == version.previous`.
- `failed`: `ok:false`, `error` set, `code` set.
- Summary extra: `skipped`.

### create — `status`: `created` | `not_created` | `failed`

```json
{ "ok": true, "status": "created", "file": "docs/foo.md",
  "page_id": "456", "title": "Foo", "space": "ENG", "parent": "123",
  "url": "...", "page_width": { "value": "max", "default": false },
  "persisted": true,
  "attachments": [ ... ], "warnings": [], "broken": [],
  "error": null, "code": null }
```

- Phase-1 abort: validation-failed files → `{ok:false,status:"failed",code:"VALIDATION",error}`;
  the rest → `{ok:false,status:"not_created",error:null}`.
- Summary: `{total, succeeded, failed, aborted}`.

### fix — `status`: `changed` | `consistent` | `failed`

```json
{ "ok": true, "status": "changed", "file": "docs/foo.md", "page_id": "123",
  "dry_run": false,
  "changes": [ { "field": "space", "old": "OLD", "new": "ENG" } ],
  "warnings": [], "error": null, "code": null }
```

- `consistent`: `changes` empty.
- `--dry-run`: `dry_run:true`, status still `changed` when changes exist, no file
  written.
- Summary extras: `changed`, `consistent`.

### info — data only (no operational verb)

```json
{ "ok": true, "file": null, "page_id": "123", "title": "Foo",
  "page_status": "current", "space": "ENG", "parent": "456",
  "version": { "number": 7 },
  "page_width": { "value": "max", "default": true },
  "created": { "at": "…", "by": { "account_id": "…", "name": "Will" } },
  "updated": { "at": "…", "by": { "account_id": "…", "name": "Will" } },
  "message": "Updated via markfluence", "url": "…",
  "properties": null }
```

- Confluence's `status` field is renamed `page_status` to avoid colliding with the
  result-status concept used by the action commands.
- `properties` stays gated on `--properties`: `null` when the flag is absent, an
  array of `{key, value}` (sorted by key) when present. No extra API call unless
  asked.
- **Page not found** is a `results[0]` entry `{ok:false,code:"NOT_FOUND"}` with
  exit `1` — *not* a fatal stderr error (that path is reserved for config/usage).
  Keeps "operational failures live in the payload" consistent.

### read — structured fields + body string

```json
{ "ok": true, "page_id": "123", "title": "X", "space": "ENG",
  "parent": null, "page_width": { "value": "max", "default": true },
  "format": "markdown", "body": "# X\n\nhello" }
```

- `page_width` is the same nested object as `info` (null when the width read
  fails). `format` echoes `--format` (`markdown` | `storage`). `parent` is `null`
  for top-level, else the parent page id.

## Errors & exit codes

- **Fatal / pre-flight** (bad flags, `client.Resolve`/auth) → JSON error object on
  **stderr**, no stdout payload, **exit 2**:

  ```json
  { "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG" }
  ```

- **Per-file operational** failures → `{ok:false, error, code}` in `results`;
  **exit 1** if any file failed.
- Codes derived from `client.HTTPError` status + failure site (see
  `internal/jsonout`).

## Testing

- `internal/jsonout`: unit-test `Emit`/`EmitError` output (golden strings) and the
  `HTTPError`→`Code` mapping.
- Each `cmd/*`: golden JSON tests built from hand-constructed `client.Page` /
  result-struct values (like `info_test.go` builds `client.Property` values) — no
  network mock needed. Cover: a success, a per-file failure, warnings/broken
  populated; for create, the phase-1 abort envelope; for fix, `--dry-run`.
- `cmd`: extend `root_test.go` to assert the `--json` persistent flag is
  registered.
- `make test && make lint && make vet` before done.

## Docs

- Document `--json` in the README (envelope shape, the per-command result keys, the
  error object, exit-code table) and note the schema is versioned via
  `schema_version`.

## Open follow-ups (not in this PR)

- A published JSON Schema file for `schema_version: 1` (nice-to-have for external
  consumers); the golden tests pin the shape in the meantime.
