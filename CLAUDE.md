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

This repo contains **two coexisting programs**:

1. **`confluence_publish.py`** (repo root) — the original standalone script, run via `uv run confluence_publish.py` with its own inline PEP-723 dependencies (uses `requests`). It is **left untouched** and is explicitly excluded from ruff and ty (see `pyproject.toml`). Don't lint, format, or typecheck it, and don't modify it unless asked.

2. **`markfluence`** (the `src/markfluence/` package) — the new click-based CLI that supersedes it. Its `update` subcommand is a **verbatim port** of the script's publishing logic.

### Package layers

- `cli.py` — the click group (`main`); loads dotenv, registers subcommands. Command taxonomy mirrors [pchuri/confluence-cli](https://github.com/pchuri/confluence-cli), but **only `update` and `create` are implemented**; other commands (read, delete, search, attachments, etc.) are intentionally not built yet.
- `libclient.py` — `ConfluenceClient`, an **`httpx2`** wrapper (note: `httpx2` is Pydantic's maintained successor to the dead `httpx`; import name is `httpx2`). Holds auth + the API calls (`get_page`/`get_page_or_none`/`page_exists`, `resolve_space_id`, `search_pages_by_title`, `update_page`, `create_page`, and attachment ops `list_attachments`/`create_attachment`/`update_attachment`/`sync_attachments`). Everything is Confluence **v2** except attachment writes, which are **v1** (`/wiki/rest/api/...`) since v2 attachments are read-only. URLs are built as absolute strings off `base_url` rather than relying on relative-URL joining.
- `libmarkdown.py` — all markdown/frontmatter logic: the frontmatter helpers (`extract_frontmatter` with inline-`#`-comment support, `update_frontmatter_field`) and the conversion pipeline, exposed as `md_to_confluence(md_body, filename, base_url, space_key)` which returns `(html, {"attachments", "broken"})`. **Both** `update` and `create` call it, then `sync_attachments` the returned local images. Carries the `E501` ruff ignore (verbatim long lines).
- `update.py` — the `update` command + `process_file` flow (frontmatter resolution, mtime skip, calls `md_to_confluence`).
- `create.py` — the `create` command: two-phase transactional create (validate all files → create parents-first in topological order), with hierarchy expressed via `parent:` (`null` | `.md` path | page_id). See `_plans/create-subcommand.md`.

### The conversion pipeline (the crux)

`libmarkdown.py`'s `md_to_confluence()` turns a markdown body into Confluence storage-format HTML through an **ordered sequence of regex transforms**, and the order encodes real dependencies. Preserve it when editing:

1. `gfm.convert()` (marko) → base HTML
2. `rewrite_anchor_links` — must run **before** `replace_internal_doc_links` so corrected fragments carry through the `.md`→URL rewrite
3. `replace_internal_doc_links` — needs the sibling-doc `page_id`/`title` map and the space key
4. TOC / note / chart / layout / callout comment-directive substitutions
5. `replace_images` — rewrites `<img>` to `<ac:image>`; local files → `ri:attachment` (with a stable path-based filename, e.g. `assets/x.png`→`assets_x.png`) collected for upload; remote URLs → `ri:url`; missing/unsupported → `IMAGE BROKEN:` text. Image properties `title`/`width`/`height`/`align` ride in the markdown title as a JSON object (`![alt](x.png '{"width":"100"}')`), falling back to a plain `ac:title` when the title isn't JSON (`alt` is always native). Uploading happens in the command (needs a page id) via `sync_attachments`, which stores a SHA-256 in the attachment comment to skip/update-in-place on re-runs (mark's scheme).
6. `collapse_paragraph_newlines` — must run **before** `replace_code_blocks`; it stashes `<pre>` blocks so code survives the newline flattening
7. `replace_code_blocks` — relies on that `<pre>` stash still being intact

The transforms map GitHub/Mark-style markdown constructs to Confluence macros (code, panels/callouts, charts, layouts, TOC, images) and rewrite cross-doc links + heading anchors to their published Confluence URLs/ids.

### Frontmatter-driven publishing

Each markdown file's YAML frontmatter carries `title`, `page_id`, and (written by `create`) `space` (a key) and `parent` (`null` for top-level, a `.md` path, or a page_id). `update` looks up a missing `page_id` by `title` and **writes it back into the file**; `create` writes `page_id`/`space`/`parent` back after creating. Updates are skipped when the file's mtime predates the page's last version (unless `--force`). Both commands process multiple files and exit non-zero if any fail.

## Project phasing

Work is planned in `_plans/`; deferred work is tracked in `todo.md`. The conversion pipeline has been extracted into `libmarkdown.py` (shared by `update`/`create`). Still on paper (see `_plans/create-subcommand.md` and `todo.md`): `update` gaining space/parent enforcement + moves (via the legacy `content/{id}/move` endpoint), a `fix` command that infers frontmatter from a live page, and frontmatter value quoting. Tests are currently smoke-only.
