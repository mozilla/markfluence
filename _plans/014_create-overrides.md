# Plan: `create` title / page_width overrides + persist toggle

Add `--title`, `--page-width`, and `--persist`/`--no-persist` flags to `create`.
The value flags override frontmatter; the persist toggle controls the existing
frontmatter write-back.

## Decisions locked (from the interview)

### Override semantics (flag wins)

`--title` and `--page-width` override the matching frontmatter value with no
conflict error. This differs from create's existing `--space`/`--parent` (which
error on conflict); those are left unchanged, so create is internally mixed.
Accepted.

### `--title`

- Effective title = `--title` → frontmatter title; still **required** (error if
  neither). Feeds the duplicate-title check and `CreatePage`.
- **Requires exactly one FILE** (a shared title across a batch collides on the
  dup-check and is nonsensical).

### `--page-width`

- Width is **always applied** to the new page (unchanged), defaulting to `max`.
  Value precedence: `--page-width` → frontmatter `page_width` → `max`.
- **Batch-allowed** (a uniform width across files is sensible).
- The flag value is validated by reusing `pagewidth.Declared`.

### `--persist` (default) / `--no-persist`

- `--persist` (default): write back `page_id`, `space`, `parent` (as today) **plus
  the effective `title` and the effective `page_width` (always, even the default
  `max`)**, overwriting whatever is there. Writing the effective title reconciles a
  `--title` override so a later `update` won't rename the page back.
- `--no-persist`: write **nothing** to the file (skips `page_id` too).
- `--no-persist` wins if both are somehow given (`doPersist = persist && !noPersist`).

### `--title` single-FILE guard

Only `--title` triggers the single-FILE requirement; `--page-width` and the
persist toggle are batch-ok.

### Consequences

- `--no-persist` leaves the created file without a `page_id`; since `update` now
  requires a page id, such a file can't be `update`d later without `--page-id`.
  Fire-and-forget by design.
- In-set parent references still resolve under `--no-persist` (they use the
  runtime created-map, not the file); cross-run `.md`-path parent references rely
  on persisted `page_id`s.

## Implementation

- Flags `--title`, `--page-width`, `--persist` (bool, default true), `--no-persist`
  (bool, default false). `run` computes `doPersist := persist && !noPersist` and
  enforces the `--title` single-FILE guard up front.
- `resolveFile`: effective title via `resolveTitle`, effective width via
  `resolveWidth(pageWidthOpt, mf.Frontmatter)` (flag → frontmatter → default max).
  `record.title`/`record.width` carry the effective values, so `createOne`
  (`CreatePage`, `pagewidth.Apply`) uses them unchanged.
- `createOne(r, parentID, c, doPersist)`: gate the write-back on `doPersist` and
  add `title` + `page_width` to the fields written (canonical order via
  `UpdateField`).

## Testing

Pure helpers, unit-tested (the network flow stays untested, as today):

- `resolveTitle(cliTitle, mf)` and `resolveWidth(cli, fm)` (flag/frontmatter/
  default + invalid-value error).
- `overrideNeedsSingleFile(cliTitle, nFiles)`.
- `wantPersist(persist, noPersist)` (default true; `--no-persist` wins).

## Docs

- CLAUDE.md: note create's `--title`/`--page-width`/`--persist`, and that
  `--persist` now also writes `title` + `page_width` back.
- README `create` section: document the three flags.
