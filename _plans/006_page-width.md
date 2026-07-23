# Plan: `page_width` frontmatter field

Add a `page_width` frontmatter field that controls the published Confluence page's
width. This is distinct from the deferred `layout: article` todo (that concerns the
`ac:layout` article layout, not page width).

## Background — how Confluence stores width

Page width is **not** part of the page body or the v2 page fields; it's stored as
two **content properties**, set by a separate API call after the page write:

- `content-appearance-published` — the *viewed* page.
- `content-appearance-draft` — the *editor*.

Property values, and how the current UI ("Adjust width" dropdown) labels them:

| UI label | property value | our `page_width` |
|---|---|---|
| Narrow | `default` | `narrow` |
| Wide | `full-width` | `wide` |
| Max | `max` | `max` |

(`full-width` is the *middle* "Wide", not the widest — "Max" is `max`.) mark uses a
different, legacy vocabulary (`Content-Appearance: full-width|fixed|default`, no
`max`), so we deliberately diverge and use the UI vocabulary authors see today.

## Decisions locked (from the interview)

### Vocabulary

- Field name **`page_width`** (underscore, matching `page_id`).
- Values **`narrow` / `wide` / `max`**, normalized case-insensitively (lowercased,
  stripped).
- **Unset or blank ⇒ `max`.**

### Authoritative default (option A)

markfluence treats the markdown file as the source of truth for width. On **every**
`create`/`update` publish it asserts the width — including forcing `max` when
`page_width` is unset. Consequences, accepted deliberately:

- It **overwrites a width set manually in the Confluence UI** when the frontmatter
  doesn't say otherwise.
- It costs extra API calls per publish (mitigated by an idempotent skip; see below).

Both `content-appearance-published` and `content-appearance-draft` are set to the
same value so the viewed page and the editor agree.

### Setting the property — `ConfluenceClient.set_content_property`

No single upsert exists, so setting a property is get-then-create-or-update:

1. `GET /pages/{id}/properties?key={key}`.
2. Already equals target ⇒ **do nothing** (idempotent skip; return `"unchanged"`).
3. Exists but differs ⇒ `PUT /properties/{propId}` with `version.number + 1`.
4. Absent ⇒ `POST /properties`.

**Flakiness:** content-property writes around a page write sometimes return a
spurious 4xx/5xx even though they applied. On any HTTP error, **pause 1s and retry
once**; the retry re-reads first, so an actually-applied write resolves to
`"unchanged"`. If the retry also fails, the caller emits a **warning and keeps
going** — width failure is **non-fatal** and does **not** change the exit code (the
page write itself already succeeded).

### Validation

A **present but unrecognized** value (e.g. `page_width: mx`) is a **hard error**:
- `create` — a phase-1 validation error (nothing is created).
- `update` — fails that file (checked up front, so a typo is surfaced even on a file
  that would otherwise be mtime-skipped).

Unset/blank is not an error — it's the `max` default.

### Where width is applied

**On the publish path only:**
- `create` — always, in phase 2 after the page (and attachments) exist.
- `update` — only when it actually publishes (not on an mtime-skip). Editing
  `page_width` bumps the file mtime, so a width change always triggers a publish.
  Known gap: a width changed *in the UI* on an otherwise-unchanged file isn't
  reasserted until the next real edit or `--force`. Reasserting on every run would
  cost a property GET per file per run; not worth it.

### `fix` — reconcile `page_width` from the live page

`fix` makes the markdown match Confluence, so it reads the live width and writes it
back. Source of truth: **`content-appearance-published`**, reverse-mapped
(`max`→`max`, `full-width`→`wide`, `default`→`narrow`, legacy `fixed`→`narrow`).

Comparison is by **effective width** (unset/blank `page_width` counts as `max`):

- effective declared == live ⇒ no change (so an all-`max` page stays free of an
  explicit `page_width: max` line).
- differ ⇒ write the live value into `page_width`.

**Unset live property** (page renders at Confluence's site default, which we can't
cheaply read) ⇒ treated as **`narrow`**. Caveat: if a site admin changed the default
width, `fix` guesses wrong here.

Best-effort read: if the property GET fails, `fix` warns and skips the width field
rather than failing the file. `fix` never writes to the server.

### `info` — display width

`info` shows a `page_width:` line (labeled to match the frontmatter field) from
`content-appearance-published`, reverse-mapped. An unset property is shown as
`narrow (Confluence default)` (honest that it's inferred). A read failure shows
`unknown` rather than crashing.

## Code shape

- `pagewidth.py` (new): vocabulary constants + mapping, `declared_width`
  (validate/normalize, default `max`), `apply_page_width` (set both keys),
  `set_page_width` (apply + click reporting, non-fatal on HTTP error), and
  `read_page_width` → `(vocab, explicit)`.
- `libclient.py`: `get_content_property` and `set_content_property` (idempotent,
  retry-once).
- `create.py` / `update.py`: validate in the existing validation step; call
  `set_page_width` on the publish path.
- `fix.py`: read live width, add `page_width` to the reconcile diff.
- `info.py`: add the `width` row.

## Deferred / out of scope

- `--json` output (separate `todo.md` item).
- Reasserting width on mtime-skipped `update` runs (see gap above).
- Reading the space/site default width so `fix`/`info` don't have to assume
  `narrow` for an unset property.
