# Plan: `create` subcommand (+ `md_to_confluence` extraction)

Add a `create` subcommand that creates a new Confluence page in a specified space
from a markdown FILE (mirroring confluence-cli's `create`, but file-driven like
`update`). As part of this, extract the shared markdown→storage conversion out of
`update.py` into a new `libmarkdown.py`.

## Scope

**Build now:**
1. Extract `md_to_confluence(...)` into `libmarkdown.py`; rewire `update` to use it.
2. Implement the `create` subcommand.

**Design on paper only (documented below, NOT built):** `update`'s space/parent
enforcement + move logic, and a new `fix` command.

## Decisions locked (from the interview)

- `create FILE... --space KEY [--parent PAGE_ID]` — multiple files, like `update`.
- Space is specified as a **key**; resolved to a numeric `spaceId` via
  `GET /wiki/api/v2/spaces?keys=KEY`.
- **`--space` precedence (mirrors `--parent`):** space may come from `--space` or
  frontmatter `space`. Both set and differ → error. Only one set → use it. Neither →
  error (a page must have a space).
- Frontmatter model written by `create`: `page_id`, `space` (key), `parent`
  (`null` for top-level, else the parent page id).
- **Parent has three forms:** `null` (top-level), a **`.md` file path** (a parent
  page authored in the doc set), or a **numeric page_id** (an existing external
  page). See "Hierarchy publishing" below.
- **Parent precedence:** `--parent` and a non-null frontmatter `parent` both set →
  error. Otherwise use whichever is set. Neither set → `parent: null`. The resolved
  parent must live in the target space → else error.
- Title comes from frontmatter `title:` only (required; no `--title`).
- No `--message` / `--force` / mtime logic (those are `update`-only).
- Conversion is the full shared pipeline via `md_to_confluence`.

## `create` behavior — two phases (transactional pre-check)

### Phase 1: validate every file, create nothing
For each FILE, collect (don't stop on first) failures:
- `title` missing in frontmatter → fail.
- Space unresolvable (bad `--space`/frontmatter key, or the precedence rule above
  is violated) → fail.
- Frontmatter `page_id` present **and points to a live page** → fail with
  `Page exists.` (a missing/404 page_id is fine — it will be overwritten).
- Title already exists in the target space (search filtered to `spaceId`) → fail.
- Parent (resolved per precedence) not in the target space → fail.

If **any** file failed, print all failures and **abort** (exit non-zero). Nothing is
created.

### Phase 2: create each page
- Convert: `md_to_confluence(md_body, filename, base_url, space_key)`.
- `POST /wiki/api/v2/pages` with `spaceId`, `title`, `body` (storage), and
  `parentId` when non-null (omitted for top-level).
- Write back to the file's frontmatter: `page_id` (new id), `space` (key),
  `parent` (`null` or id).
- Print the new `page_id` and page URL per file.

Note: phase 2 is not a true transaction — if creation fails partway, earlier pages
stay created. Phase 1 makes the common misuse cases (existing page, title clash)
all-or-nothing.

## Hierarchy publishing (create a whole tree in one pass)

Motivating problem: publishing a hierarchy of pages for the first time is
chicken-and-egg — a child's parent page_id doesn't exist until the parent is
created. Solution: let `parent` reference the parent's **markdown file**, and have
`create` process the whole set in one invocation, topologically ordered.

**Parent forms and resolution:**
- `null` → top-level page.
- **`.md` path** (e.g. `parent: overview.md`) → resolved **relative to the child
  file's directory**, with this precedence:
  1. If the referenced file **does not exist on disk** → error ("parent file not
     found").
  2. Else if the parent file is among the files passed to this `create` run → create
     it first (topological order).
  3. Else if that file already has a `page_id` in its frontmatter → use it (no
     creation).
  4. Else (exists, not in the run, no `page_id`) → error ("parent not yet
     published").
- **numeric page_id** → an existing external page; used as-is.

**Topo-sort:** `create` builds a dependency graph from `.md`-path parents among the
passed files, detects cycles (error), and creates parents before children. It keeps
an in-memory file→new-page_id map so a child created in the same run gets its
parent's fresh id.

**Rewrite on success:** after resolving a `.md`-path parent to an id, `create`
rewrites the child's frontmatter to `parent: <page_id>  # <original.md>` — the
page_id is the live value, and the trailing `#` comment preserves the authoring
linkage. (`update` follows the same rewrite convention.)

**Phase-1 additions for hierarchy:** validate that every `.md`-path parent resolves
(per the precedence above), the graph is acyclic, and each resolved parent is in the
target space. All-or-nothing, before any creation.

## Frontmatter parser change (shared, affects `update` too)

The `parent: <id>  # <file>` form requires **inline `#`-comment support** in
`extract_frontmatter` (today it only skips full-line comments, so it would read the
value as `"<id>  # <file>"`). Add inline-comment stripping in `libmarkdown`, applied
to all fields, using the YAML convention: a comment starts at whitespace-then-`#`.

- **Quoting (now implemented):** single/double-quoted values suppress inline-comment
  parsing, so `title: "Detect # Verify"` round-trips; `update_frontmatter_field`
  auto-quotes on write-back when needed and takes a separate `comment=` for the
  `parent` annotation. (Was a known limitation — unquoted `title: Detect # Verify`
  still truncates to `Detect`, matching YAML.)

## `md_to_confluence` extraction (`libmarkdown.py`)

New module owns all markdown/frontmatter/conversion logic. Move from `update.py`
verbatim (behavior-preserving):
- Frontmatter helpers: `extract_frontmatter`, `extract_title_from_markdown`,
  `update_frontmatter_field`.
- Transforms: `replace_confluence_notes`, `collapse_paragraph_newlines`,
  `replace_code_blocks`, `replace_github_callouts`, `github_slug`,
  `confluence_slug`, `extract_headings`, `build_headings_anchor_map`,
  `rewrite_anchor_links`, `build_docs_page_map`, `extract_space_key`,
  `replace_internal_doc_links`, `replace_chart_directives`, `replace_layout_blocks`.
- New orchestrator:

  ```python
  def md_to_confluence(md_body, filename, base_url, space_key):
      """Run the full markdown -> Confluence storage pipeline (frontmatter
      already stripped). Returns storage-format HTML."""
  ```

  It contains the exact ordered sequence currently inlined in `process_file`
  (gfm.convert → anchor rewrite → internal doc links → TOC/note/chart/layout/
  callout → collapse newlines → code blocks).

`update.py` imports the helpers + `md_to_confluence` from `libmarkdown` and keeps
only its flow (`process_file`, the `update` click command). `create.py` imports the
same. Move the `E501` per-file ruff ignore from `update.py` to `libmarkdown.py`
(that's where the verbatim long lines now live).

## Client additions (`libclient.py`)

- `resolve_space_id(space_key)` → numeric id via `GET /api/v2/spaces?keys=KEY`
  (error if not found).
- `page_exists(page_id)` → bool; treats HTTP 404 as "does not exist", re-raises
  other statuses. (Split the raise-for-status handling so 404 is distinguishable.)
- `search_pages_in_space(title, space_id)` → title search filtered to a space
  (extend `search_pages_by_title` with an optional `space_id` param).
- `create_page(space_id, title, html_body, parent_id=None)` →
  `POST /api/v2/pages`.
- Parent-in-space check uses `get_page(parent_id)`'s `spaceId`.

## Frontmatter serialization notes

- `parent: null` is written/parsed as the literal string `null`, consistent with how
  the existing code already treats `page_id: null` (see `build_docs_page_map`).
- `create` calls `update_frontmatter_field` once per field (`page_id`, `space`,
  `parent`).

## Registration & tests

- Register `create` in `cli.py`.
- Extend smoke tests: `create --help` renders and lists `FILENAMES`, `--space`,
  `--parent`.
- `just check` must pass (ruff, format, pytest, ty).

## Verification

- `uv run mzcld-confluence-cli create --help`
- `uv run mzcld-confluence-cli update --help` (unchanged behavior)
- `uv run pytest`, `uv run ruff check`, `uv run ty check`
- Live create is not exercised here (needs real Confluence creds); the conversion is
  covered by the behavior-preserving extraction.

## On paper only — future work (not built now)

### `update` gains space/parent enforcement + moves
- Require **both** `space` and `parent` present in frontmatter; error if either is
  missing (directs the user to `create` or `fix`).
- Reconcile against the live page:
  - `space` (key) differs from the page's actual space → move.
  - `parent: <id>` differs from the page's actual parent → reparent.
  - `parent: null` but the page currently has a parent → move to top-level.
  - Resolved parent not in the declared space → error.
- Moves use the legacy endpoint
  `PUT /wiki/rest/api/content/{id}/move/{position}/{targetId}`:
  reparent → `targetId` = new parent, `position = append`; top-level / cross-space
  → `targetId` = destination space's **home page**, `position = append`.
  (Verify whether v2 `PUT /pages/{id}` accepts `parentId` for the same-space
  reparent case, which would avoid the legacy call there.)

### `fix` command
- `fix FILE...`: given a `page_id` (or a title resolvable via search), read the live
  page and populate/refresh `space` (key) + `parent` (`null`/id) + `page_id` in the
  frontmatter. This is the inference path deliberately removed from `update`.

Backlog items for the above (plus the frontmatter-quoting follow-up) are tracked in
`todo.md`.
