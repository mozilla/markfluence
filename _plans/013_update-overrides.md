# Plan: `update` title / page_id / page_width overrides

Add `--title`, `--page-id`, and `--page-width` flags to `update`. CLI flags
override the file's frontmatter for that run. Also drop `update`'s title-based
page lookup (and its write-back), and stop forcing a default width.

## Decisions locked (from the interview)

### Flags (not positional)

`--title <string>`, `--page-id <string>`, `--page-width <narrow|wide|max>`.
Positional args stay the `FILE...` list; kebab-case matches `create`'s
`--space`/`--parent`. The guard treats a flag as "set" when its value is
non-empty (so `--title ""` counts as unset).

### page_id is now required; title search removed

- Resolution: `--page-id` → frontmatter `page_id`. If neither, **error**
  (`no page id: set page_id in frontmatter or pass --page-id`).
- The old behavior — look up a missing `page_id` by title and **write it back**
  into the file — is removed. Consequently `update` no longer mutates files at
  all. `findByTitle` is removed from `update`.
- `SearchPagesByTitle` on the client stays (the `fix` command still uses it).

### title is now optional

- Resolution: `--title` → frontmatter title → **live page title** (from the
  `GetPage` call `update` already makes). A page always has a title, so title is
  never truly missing once the page id resolves.
- `--title` renames the live page (it flows into `UpdatePage`).

### width: assert only when explicitly set

- Apply width iff `--page-width` is given **or** the frontmatter has a
  `page_width` line; precedence `--page-width` → frontmatter. Otherwise **skip the
  width call entirely**, leaving the live page's width untouched.
- **Behavior change:** today `update` always asserts width, defaulting to `max`
  when absent. After this, a file with frontmatter but no `page_width` no longer
  forces `max`.
- The `--page-width` value is validated by reusing `pagewidth.Declared` (feed it a
  synthetic `{"page_width": value}` map).

### single-FILE guard

- `--title` or `--page-id` requires exactly one `FILE` (they're per-page; one
  page_id across a batch is nonsensical).
- `--page-width` may apply across a batch (a uniform width change is sensible), so
  it does **not** trigger the guard on its own.

### Unchanged

mtime skip (`--force` to bypass), attachment sync, markdown→storage conversion,
and deriving the space from the live page (`SpaceKeyFromWebUI`).

## Revised `processFile` flow

1. Parse the file.
2. `pageID` = `--page-id` or frontmatter `page_id`; error if empty.
3. Resolve width intent: `(width, applyWidth, err)` from `--page-width` /
   frontmatter / none.
4. `GetPage(pageID)`; derive `spaceKey` from the live page.
5. `title` = `--title` or frontmatter title or `page.Title`.
6. mtime skip unless `--force`.
7. Convert, sync attachments, `UpdatePage(pageID, title, …)`.
8. If `applyWidth`, `pagewidth.Apply`; else leave width as-is.

## Out of scope

`create` is unchanged: it still asserts width (default `max`) and has its own
`--space`/`--parent`. This leaves a create/update width inconsistency, noted but
not addressed here.

## Testing

Pure, unit-tested helpers (the network flow stays untested, as today):

- `resolveTitlePageID(cliTitle, cliPageID string, mf *frontmatter.MarkdownFile)
  (title, pageID string)` — precedence (live-title fallback applied later, in the
  flow).
- width resolution — `--page-width`/frontmatter/none, plus invalid-value error.
- the single-FILE guard (set flag + >1 file → error; +1 file → ok).

## Docs

- CLAUDE.md: revise the frontmatter-publishing note ("`update` looks up a missing
  `page_id` by title and writes it back") and the width "asserts on every publish"
  wording for `update`.
- README `update` section: drop the title-lookup/write-back sentence; document the
  three flags and the new page_id-required / width-only-when-set behavior.
