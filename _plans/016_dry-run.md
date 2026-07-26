# Plan: `--dry-run` for `update` and `create`

Add `--dry-run` to `update` and `create` so they preview what a real run would do
— pages created/updated (and version bumps), attachments that would upload,
page-width changes, and (for `create`) frontmatter write-backs — **without writing
to Confluence or to files**. `fix` already has `--dry-run`; this brings the two
publishing commands to parity (issue #11).

Exit-non-zero semantics must match a real run's validation failures.

## Decisions locked (from the interview)

### Status model — reuse the verb, add `dry_run`

- **No new status verbs.** A dry-run keeps the existing per-file status
  (`published`/`skipped`/`created`/`failed`); the machine-readable signal is a
  required, always-emitted `dry_run` boolean on `updateResult`/`createResult`
  (mirroring the `dry_run` already on `fixResult`). Consumers key off `dry_run` to
  know nothing was written. `summarize()` is untouched.

### Human output — identical per-file lines + one banner

- **Per-file lines are byte-identical between dry-run and a real run.** We do *not*
  reword them (no "would publish" per line). The only human cue is a single leading
  `ui.Warn` banner emitted once per invocation:

  ```
  DRY RUN — no changes will be written.
  ```

  The banner is **suppressed under `--json`** (it would corrupt the JSON document;
  `dry_run: true` is the signal there).
- **`fix` is converted to this same rule**: drop its `renderHuman` "would set" verb
  swap (verb is always "set") and add the banner. Update `fix`'s affected tests.
- The one **unavoidable exception** is `create`'s final success line. A real run
  prints `Created page <id>: <url>`, but a dry-run has created nothing, so no id and
  no URL exist. `create` dry-run prints a distinct line instead:

  ```
  Would create page 'Title' in SPACE
  ```

  (`update` has no such problem: its URL comes from the read-only `GetPage` and its
  new version is `prev+1`, so `Published v4: <url>` prints identically and truly.)

### `update --dry-run`

All of `update`'s pre-write work is already read-only, so the flow is unchanged
through conversion; only the writes are swapped for previews.

1. Parse, resolve title/page_id/width, `GetPage` — unchanged (reads).
2. mtime skip (unless `--force`) — **honored**. A file that would be skipped reports
   `status: "skipped"` with the identical `Skipping -- no changes` line and short-
   circuits before any attachment/width preview, exactly as a real run.
3. `convert` — unchanged; surfaces `broken`/`warnings` and the attachment list.
4. Attachments: previewed via a **new read-only `client.PlanAttachments`** that
   shares the per-file decision logic with `SyncAttachments` (`ListAttachments` +
   checksum compare) but performs **no uploads** → accurate
   `created`/`updated`/`skipped`. Real run still calls `SyncAttachments`.
5. Version: reported as `{previous, new: previous+1}` (the same value a real run
   would send); no `UpdatePage` call.
6. Width: previewed via `pagewidth.Read`; reported **only when it differs** from the
   live width. No `pagewidth.Apply` call. (`fix` already reads live width in its
   dry-run — precedent.)

### `create --dry-run`

`create`'s **phase 1 (validation) runs unchanged** — it performs no writes, so all
validation errors, their codes, and the abort→exit-1 behavior (including
`topoSort` cycle detection) match a real run for free. Only **phase 2** is swapped
from create to preview:

- `convert` surfaces `broken`/`warnings` and the attachment list.
- Attachments: a not-yet-created page has nothing to read, so every attachment is
  synthesized as `created` (no `PlanAttachments` call, no server read).
- `page_id`/`url` stay `null`; `status: "created"`, `dry_run: true`.
- Width shown from the record (no `Apply`).
- `persisted` reflects **intent**: `true` when `--persist` is in effect; `dry_run:
  true` conveys nothing was actually written. No extra human line — a real run
  prints nothing about persistence either, and the identical-messages rule keeps it
  that way; the write-back intent lives in the JSON (`persisted` + `dry_run`).

### New `parent_file` field (create only, always present)

To give an automated consumer the parent→child relationship **without corrupting**
`parent` (whose contract stays "a page id or null"), add a separate
`parent_file` (`stringOrNull`) to `createResult`, **emitted in every run** (not
just dry-run):

| parent kind | `parent` (id) | `parent_file` |
|---|---|---|
| top-level | `null` | `null` |
| external (raw id) | `<id>` | `null` |
| published (`.md` w/ page_id) | `<id>` | `foo.md` |
| in-set (sibling being created) | real: `<new id>` · **dry-run: `null`** | `foo.md` |

Value = `parentInfo.display` (already populated for in-set and published parents).
In a dry-run an in-set child shows `parent: null` **and** `parent_file: "foo.md"`,
so the relationship is explicit; results are still emitted in topo order too.

**Rationale:** a synthesized/fake parent id is a footgun for an agent (it could feed
the fake id into a later real call); `null` is unambiguous and safe, and a
separately-named field beats overloading `parent` with a `.md` path.

### Flag

Each command gets its own `--dry-run` bool flag (as `fix` has), default false.

## Schema (`schema/json-output/v1.json`) — amend in place

- `updateResult`: add `dry_run` to properties + `required`.
- `createResult`: add `dry_run` **and** `parent_file` to properties + `required`.
- **`abortedResult()` must emit both new fields** (it builds `createResult`s; a
  dry-run that aborts in phase 1 would otherwise fail conformance): `dry_run` =
  the run's flag, `parent_file` = null.
- Amending `v1` (not minting `v2`) matches the precedent `fix` set when it added
  `dry_run` to `v1`.

## Testing

- `client.PlanAttachments`: unit test the created/updated/skipped decision (shared
  helper with `SyncAttachments`, no uploads).
- `update`/`create` dry-run: human + `--json` result-shape tests, including the
  stale-file skip (update) and an in-set-parent dry-run (create → `parent: null`,
  `parent_file` set).
- `fix`: fix up tests that asserted the "would set" wording.
- `TestSchemaConformance`: extend to cover the new required fields.

## Docs

- CLAUDE.md: note `--dry-run` on `update`/`create` (parity with `fix`) and the new
  `parent_file` output field.
- README: document `--dry-run` for both commands.
