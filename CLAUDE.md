# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Uses [uv](https://docs.astral.sh/uv/) for dependency management and [just](https://github.com/casey/just) as the task runner.

- `uv sync` — install deps into `.venv/`
- `just check` — the full gate: `ruff check` + `ruff format --check`, `pytest`, `ty check`. Run before considering work done.
- `just lint` / `just test` / `just typecheck` — the individual pieces
- `uv run pytest tests/test_smoke.py::test_group_help` — run a single test
- `uv run ruff format` — apply formatting
- `uv run markfluence update FILE...` — run the CLI (needs `.env`; see below)

## Configuration

The CLI reads `CONFLUENCE_URL`, `CONFLUENCE_USERNAME`, `CONFLUENCE_TOKEN` from `.env` (via python-dotenv) or the environment. `.env.example` is the template. `ConfluenceClient.from_env()` is the single place these are read and validated.

## Commit conventions

Commits must follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/): `type(optional-scope): description`, e.g. `feat(update): add --dry-run flag`, `fix(libclient): handle 404 on missing page`. Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`.

Do not add AI attribution to commits or pull requests — no `Co-Authored-By: Claude` trailers, no "Generated with Claude Code" lines, no similar attribution in commit messages or PR descriptions.

## Architecture

**`markfluence`** (the `src/markfluence/` package) is a click-based CLI for publishing markdown to Confluence. Its `update` subcommand is a **verbatim port** of the publishing logic from an earlier standalone script (since removed; `requests`-based, PEP-723 deps).

### Package layers

- `cli.py` — the click group (`main`); loads dotenv, registers subcommands. Command taxonomy mirrors [pchuri/confluence-cli](https://github.com/pchuri/confluence-cli). **Implemented: `update`, `create`, `fix`, `info`.** Other commands (read, delete, search, attachment subcommands, `property-*`, etc.) are intentionally not built yet.
- `libclient.py` — `ConfluenceClient`, an **`httpx2`** wrapper (note: `httpx2` is Pydantic's maintained successor to the dead `httpx`; import name is `httpx2`). Holds auth + the API calls: pages (`get_page`/`get_page_or_none`/`page_exists`, `resolve_space_id`, `search_pages_by_title`, `update_page`, `create_page`), users (`get_user`), attachments (`list_attachments`/`create_attachment`/`update_attachment`/`sync_attachments`), and content properties (`get_content_property`/`list_content_properties`/`set_content_property`). Everything is Confluence **v2** except attachment writes and the `get_user` lookup, which are **v1** (`/wiki/rest/api/...`) since v2 doesn't cover them. URLs are built as absolute strings off `base_url` rather than relying on relative-URL joining.
- `libmarkdown.py` — all markdown/frontmatter logic: the frontmatter helpers (`extract_frontmatter`, `parse_value` with inline-`#`-comment + single/double-quote support, and `update_frontmatter_field(key, value, comment=None)` which auto-quotes values that wouldn't round-trip bare) and the conversion pipeline, exposed as `md_to_confluence(md_body, filename, base_url, space_key)` which returns `(html, {"attachments", "broken", "warnings"})`. **Both** `update` and `create` call it, then `sync_attachments` the returned local images. Carries the `E501` ruff ignore (verbatim long lines).
- `pagewidth.py` — the `page_width` frontmatter field (`narrow`/`wide`/`max`, default `max`) mapped to Confluence's `content-appearance-published`/`-draft` content properties. `declared_width` validates/normalizes; `set_page_width` asserts both properties on publish (best-effort, non-fatal); `read_page_width`/`width_from_properties` reverse-map for `fix`/`info`. See `_plans/page-width.md`.
- `update.py` — the `update` command + `process_file` flow (frontmatter resolution, mtime skip, calls `md_to_confluence`, asserts `page_width` on publish).
- `create.py` — the `create` command: two-phase transactional create (validate all files → create parents-first in topological order), with hierarchy expressed via `parent:` (`null` | `.md` path | page_id). See `_plans/create-subcommand.md`.
- `fix.py` — the `fix` command: reconciles a file's frontmatter (`page_id`, `space`, `parent`, `page_width`; fills a missing `title`) to match the live page. Read-only on the server; writes a file only when a field changed. See `_plans/fix-subcommand.md`.
- `info.py` — the `info` command: prints a single page's metadata (id/title/status/space/parent/version/`page_width`/authors/dates/url); `--properties` also lists all content properties. Read-only. See `_plans/info-subcommand.md`.

### The conversion pipeline (the crux)

`libmarkdown.py`'s `md_to_confluence()` turns a markdown body into Confluence storage-format HTML through an **ordered sequence of regex transforms**, and the order encodes real dependencies. Preserve it when editing:

1. `gfm.convert()` (marko) → base HTML, wrapped in `_shield_storage`: raw Confluence storage tags (`<ac:…>`/`<ri:…>`) are renamed to a colon-free per-document sentinel *before* marko (which would otherwise escape/linkify them) and restored *immediately after*, so authors can paste storage format directly and the rest of the pipeline sees real tags
2. `rewrite_anchor_links` — must run **before** `replace_internal_doc_links` so corrected fragments carry through the `.md`→URL rewrite
3. `replace_internal_doc_links` — needs the sibling-doc `page_id`/`title` map and the space key
4. TOC / callout comment-directive substitutions
5. `replace_images` — rewrites `<img>` to `<ac:image>`; local files → `ri:attachment` (with a stable path-based filename, e.g. `assets/x.png`→`assets_x.png`) collected for upload; remote URLs → `ri:url`; missing/unsupported → `IMAGE BROKEN:` text. Image properties `title`/`width`/`height`/`align` ride in the markdown title as a JSON object (`![alt](x.png '{"width":"100"}')`), falling back to a plain `ac:title` when the title isn't JSON (`alt` is always native). Uploading happens in the command (needs a page id) via `sync_attachments`, which stores a SHA-256 in the attachment comment to skip/update-in-place on re-runs (mark's scheme).
6. `collapse_paragraph_newlines` — must run **before** `replace_code_blocks`; it stashes `<pre>` blocks so code survives the newline flattening
7. `replace_code_blocks` — relies on that `<pre>` stash still being intact

The transforms map GitHub/Mark-style markdown constructs to Confluence macros (code, panels/callouts, TOC, images) and rewrite cross-doc links + heading anchors to their published Confluence URLs/ids. Arbitrary macros/layouts are authored as raw storage format directly (see the shield step above), not via bespoke directives.

### Frontmatter-driven publishing

Each markdown file's YAML frontmatter carries `title`, `page_id`, `space` (a key), `parent` (`null` for top-level, a `.md` path, or a page_id), and `page_width` (`narrow`/`wide`/`max`, default `max`). `update` looks up a missing `page_id` by `title` and **writes it back into the file**; `create` writes `page_id`/`space`/`parent` back after creating; `fix` reconciles all of these (plus `page_width`) from the live page. Updates are skipped when the file's mtime predates the page's last version (unless `--force`). Commands process multiple files (except `info`, single-arg) and exit non-zero if any fail.

## Project phasing

Work is planned in `_plans/`; deferred work is tracked in `todo.md`. The conversion pipeline lives in `libmarkdown.py` (shared by `update`/`create`). Done since the initial scaffold: the `create`, `fix`, and `info` commands; `page_width` support; and frontmatter value quoting. Still on paper (see `_plans/` and `todo.md`): `update` gaining space/parent enforcement + moves (via the legacy `content/{id}/move` endpoint), and a consistent `--json` output flag across commands. Tests cover the pure logic (frontmatter/quoting, images, page width, info rendering) plus smoke tests; the conversion transforms and `libclient` still want fuller coverage.
