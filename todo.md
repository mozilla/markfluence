# TODO

Backlog of deferred work. Design detail for these lives in `_plans/`.

- [ ] **Quoting support for frontmatter values.** The simplified frontmatter parser
  has no quoting, so a value containing whitespace-then-`#` gets truncated by inline
  comment stripping (e.g. `title: Detect # Verify` → `Detect`). Add single/double
  quoted values that suppress inline-comment parsing so such values round-trip.
  (See `_plans/create-subcommand.md` → "Frontmatter parser change".)

- [ ] **`update` space/parent enforcement + moves.** Require `space` and `parent`
  in frontmatter; reconcile against the live page via the legacy move endpoint
  (`PUT /wiki/rest/api/content/{id}/move/{position}/{targetId}`).
  (See `_plans/create-subcommand.md` → "On paper only".)

- [ ] **`fix` command.** Given a `page_id` (or resolvable title), read the live page
  and populate/refresh `space` + `parent` + `page_id` in frontmatter.
  (See `_plans/create-subcommand.md` → "On paper only".)

- [ ] **Support `layout: article`.** Honor a `layout: article` directive (likely a
  frontmatter field) to control the published page's layout/appearance. Exact scope
  TBD.

- [ ] **Raw Confluence storage escape hatch.** Give authors a way to emit arbitrary
  Confluence storage markup for macros/elements we don't explicitly support. Raw
  `ac:`/`ri:` tags can't be written directly in markdown -- marko HTML-escapes them
  because their tag names contain colons -- so use the established comment-directive
  pattern: e.g. `<!-- confluence-raw -->` ... `<!-- /confluence-raw -->`, with a
  transform in `libmarkdown.py` that replaces the block with its inner content
  verbatim (emitted as-is into the storage body, not escaped). Mirrors how
  `confluence-note` / `ac:layout` / `chart` directives already work.

- [ ] **URL-decode image `src` before the filesystem lookup.** marko URL-encodes
  image destinations (e.g. `![alt](<my file.png>)` → `src="my%20file.png"`), so a
  local image whose filename contains a space or other special character is looked
  up literally and reported `IMAGE BROKEN`. In `replace_images`, `urllib.parse.
  unquote` the `src` before resolving/checking the local path (leave remote `ri:url`
  values as-is).

- [ ] **Clean up and test `libclient.py`.** Refactor `ConfluenceClient` for clarity
  (e.g. reduce URL/`raise_for_status` repetition, tidy the v1/v2 split) and add
  tests for its logic that don't need a live Confluence -- e.g. `sync_attachments`
  create/update/skip decisions and error handling, using a mocked HTTP layer.

- [ ] **Clean up and test `libmarkdown.py`.** The conversion pipeline was ported
  verbatim from `confluence_publish.py`; refactor it for clarity (and drop the
  `E501` per-file ignore once the long verbatim lines are gone) and add real unit
  tests for the transforms (frontmatter parsing, macros, anchor/link rewriting,
  code blocks) beyond the image tests that already exist.

- [ ] **Improve `create` handling of a frontmatter `page_id`.** Today a file with a
  `page_id` either errors `Page exists.` (if the page exists) or silently creates a
  new page and overwrites the id (if it doesn't). Instead:
  - page exists → error should print the **URL** to the existing page, not just
    "Page exists.";
  - page id is bad / doesn't exist → say the page id is bad and should be removed
    (rather than silently creating a new page).

- [ ] **Drop title-based page_id resolution from `update`.** `update` currently
  searches Confluence by `title` when a file has no `page_id`, then writes the id
  back. Remove that: `update` should require `page_id` in frontmatter and error if
  it's missing (inference moves to the future `fix` command). Update the README,
  which documents the lookup-by-title behavior.

- [ ] **Remove the `--resolve` flag from `update`.** It's built on the title search
  above; drop it (its find/write-back role belongs to the future `fix` command).

- [ ] **Better error for a stale `page_id` on `update`.** When a markdown file's
  frontmatter has a `page_id` but that page no longer exists in Confluence, `update`
  currently fails with a raw HTTP 404 (surfacing from `get_page`, or from
  `list_attachments` when the page is merely *trashed* — v2 still returns trashed
  pages while the v1 attachment API 404s). Detect the missing/trashed page up front
  in `process_file` and emit a clear message (e.g. "page <id> from frontmatter no
  longer exists in Confluence; remove the page_id to recreate it with `create`, or
  fix the id") instead of a bare 404.
