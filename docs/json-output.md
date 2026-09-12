# `--json` output, in detail

The envelope, an example, and the exit codes are in the
[README](../README.md#--json-output); the authoritative field-by-field contract
is the JSON Schema at
[`schema/json-output/v1.json`](../schema/json-output/v1.json), which
`markfluence schema` also prints.

This file is the part that is neither: why the shapes are what they are, and the
per-command details a script author hits once and then needs to look up.


## Notes on the schema

- **Per-command stable.** Each command always emits the same keys in the same
  shapes (empty values are `null` or `[]`); the key *set* differs per command.
  `schema_version` is bumped on any breaking change.
- **`roots`** lists every distinct [documentation root](../README.md#the-documentation-root)
  the command resolved, sorted — `[]` for a command with no per-file root
  concept (`find`, `search`, ...) or a pre-flight failure that never reached
  root resolution. `schema` emits no envelope at all, so it has no `roots` key
  to speak of.
- **`warnings`** carries warnings about the *invocation* rather than about any
  page or file — currently only the `.env` permission warning below. A result's
  own warnings live on the result; this is for something that belongs to no
  result. `[]` when there is nothing to report. It appears on the stderr error
  object too, since a fatal failure emits no envelope and a credential failure
  is exactly the run where a warning about your `.env` matters.
- **Status verbs** are per-command: `published`/`skipped` (`update`),
  `created`/`not_created` (`create`), `changed`/`consistent` (`fix`),
  `clean`/`warnings`/`broken` (`check`),
  `created`/`updated`/`skipped` (`attachment-upload`),
  `downloaded`/`skipped` (`attachment-download`), plus `failed`. `info`, `read`,
  and `attachment-list` results carry data only (no status verb).
- **One result per target**, and the target is per-command: the page for
  `info`/`read`/`export` (always one), the file for `update`/`create`/`fix`/`check`,
  and the attachment for the three `attachment-*` commands — so
  `.results[] | .filename` works and `summary.total` is the attachment count.
  `export` nests the files it wrote in an `attachments` array on its page
  result, the way `update`/`create` do.
- **`check`'s `broken` status is `ok: false` with no `error`/`code`** — unlike
  every other failure, its `broken`/`warnings` arrays already say everything
  there is to say, so there's no separate operational error to attach. Only
  its `failed` status (a file that never reached the converter at all) sets
  them, the same as every other command's failure. `check --show-html` adds a
  `debug: { html, attachments } | null` field, populated only for a file that
  reached the converter; `html` stays exactly what the converter produced
  (unindented), since it's meant to match what `update`/`create` would
  literally publish.
- **Compound values are objects**, never display strings — `version`,
  `page_width`, and the `created`/`updated` author stamps on `info`.
- **`create`'s preflight abort** (any file failing means nothing is created)
  lists every input file — failed ones with an `error`, the rest as
  `not_created` — and sets `summary.aborted: true`.
- **Warnings and broken image/link notices** are data (`warnings`/`broken`
  arrays on each result), not stderr log lines.
- **The discovery commands list what they found**, so `results` is one object per
  match (`find`, `search`) or per node (`children`), and `summary.total` is that
  count. `search`'s summary carries two extra fields: `truncated`, meaning
  `--limit` was reached with matches left over, and `skipped`, counting index rows
  that had no page id to report (reachable only via `--cql` or `--type all`).
  Neither is a count of matches you could get by asking again for more.

Errors and exit codes:

- **Per-file operational failures** appear in `results` as
  `{ "ok": false, "error": "…", "code": "…" }`; the command exits `1` if any
  file failed.
- **`find` and `search` have no failed-result variant.** They name no page, so
  there is no id to attach a failure to: an operational failure prints the same
  typed error object to **stderr** and exits `1`, with no envelope on stdout.
  Emitting an empty `results` array would be worse than emitting nothing, since
  "no matches" is a meaningful answer that a caller acts on.
- **Fatal/pre-flight failures** (bad flags, credential resolution) print a typed
  error object to **stderr** and exit `2`:

  ```json
  { "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG", "warnings": [] }
  ```

- Error `code` values: `CONFIG`, `AUTH`, `NOT_FOUND`, `VALIDATION`, `CONVERT`,
  `IO`, `NETWORK`, `API`.
