# Plan: `fix` subcommand

Add a `fix` subcommand that reconciles a markdown file's frontmatter *coordinates*
(`page_id`, `space`, `parent`, and a missing `title`) to match its live Confluence
page. This is the frontmatter-inference path deliberately kept out of `update` (see
`_plans/002_create-subcommand.md` → "On paper only" and `todo.md`).

`fix` is **read-only on the server** -- it never creates, updates, or moves pages.
It only rewrites local frontmatter to reflect what the live page already says.

## Scope

**Build now:**
1. Implement `fix FILE...` in a new `src/markfluence/fix.py`.
2. Register it in `cli.py`.

**Deferred (follow-up, NOT this change; tracked in `todo.md`):** dropping
`update`'s title-search/write-back and its `--resolve` flag, and the README updates
for that. Until then `update` keeps its own inference; the duplication is
short-lived and intentional.

## Decisions locked (from the interview)

- `fix FILE...` with a `--dry-run` flag. **No** `--space`, `--parent`, or
  `--page-id` options (space/parent are outputs, not inputs).
- `fix` reconciles four frontmatter fields: `page_id`, `space` (key), `parent`
  (`null` or a numeric page id), and `title` (fill-only; see below).

### Locating the live page (per file)

1. **`page_id` present** → fetch directly via `get_page_or_none`.
   - **404 / trashed / wrong id → error, no fallback.** An explicit `page_id` is a
     hard identity assertion; never silently rebind to a same-titled page. Message
     directs the user to remove the id (to search by title) or correct it.
2. **no `page_id`** → search by `title` (global, all spaces), reusing `update`'s
   behavior:
   - 0 matches → error;
   - 2+ matches → error listing each `id`/`title`/URL (remedy: add a `page_id`);
   - exactly 1 → use it.
3. **neither `page_id` nor `title`** → error: *"add a page_id or title so I can
   locate the page."* (This is the "fill in at least one" case. Note `space`/
   `parent` cannot locate a page and never satisfy this check.)

### Deriving values from the live page

A single `get_page`/`get_page_or_none` (v2 `GET /pages/{id}`) returns everything;
**no new client method is needed**:

- `space` **key**: from `_links.webui` via the existing `extract_space_key`
  (same source `update` uses).
- `parent`: from `parentId` (falsy → top-level → `null`).

### Writing back — reconcile (compare *parsed* frontmatter vs. live)

For `page_id`, `space`, `parent`:

- **absent → fill** with the live value (including `parent: null` for a top-level
  page, matching what `create` writes -- keeps frontmatter complete for the future
  `update` space/parent enforcement).
- **present and == live value → leave the line untouched** (preserves any inline
  comment, e.g. `parent: 12345  # architecture.md`, and avoids churn). A present
  `parent: null` and a live top-level page count as equal.
- **present but wrong → write the live value** via `update_frontmatter_field`.
- `parent` is always written as a **numeric id or `null`** -- never a `.md` path
  (the server only knows the id; the `.md` form is a `create`-input convenience).

For `title`:

- **present → never touch** (a present-but-different title is a pending rename the
  author will push via `update`; overwriting it would silently undo that).
- **absent → fill** from the live page (completes an otherwise-unpublishable file).

A present value comparison normalizes the string `"null"` to "no value", and
compares ids/keys as strings (frontmatter values and v2 ids are both strings).

### File writes, mtime, and idempotency

- **The file is written only if at least one field actually changed.** A no-op
  `fix` must not bump mtime, or the next `update` would think the file was edited
  and needlessly re-publish (`update` skips when `mtime <= page version time`).
- `--dry-run`: report intended changes but write nothing.

### Multi-file semantics

Flat, independent per-file loop (like `update`, **not** `create`'s two-phase
transactional flow -- `fix` has no server writes and no cross-file dependencies). A
failure on one file doesn't stop the others; the command exits non-zero if any file
failed.

### Reporting

Per file, one line per changed field:

```
[file] page_id: (none) -> 1234567890
[file] space: OLD -> NEW
[file] parent: 111 -> null
[file] title: (none) -> Live Page Title
```

`[file] already consistent` when nothing changed. Under `--dry-run`, the change
lines are phrased as intent, e.g. `[file] would set space: OLD -> NEW`.
