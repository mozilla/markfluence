# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

Uses [uv](https://docs.astral.sh/uv/) for dependency management and [just](https://github.com/casey/just) as the task runner.

- `uv sync` — install deps into `.venv/`
- `just check` — the full gate: `ruff check` + `ruff format --check`, `pytest`, `ty check`. Run before considering work done.
- `just lint` / `just test` / `just typecheck` — the individual pieces
- `uv run pytest tests/test_smoke.py::test_group_help` — run a single test
- `uv run ruff format` — apply formatting
- `uv run mzcld-confluence-cli update FILE...` — run the CLI (needs `.env`; see below)

## Configuration

The CLI reads `CONFLUENCE_URL`, `CONFLUENCE_USERNAME`, `CONFLUENCE_TOKEN` from `.env` (via python-dotenv) or the environment. `.env.example` is the template. `ConfluenceClient.from_env()` is the single place these are read and validated.

## Commit conventions

Commits must follow [Conventional Commits 1.0.0](https://www.conventionalcommits.org/en/v1.0.0/): `type(optional-scope): description`, e.g. `feat(update): add --dry-run flag`, `fix(libclient): handle 404 on missing page`. Common types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `build`.

Do not add AI attribution to commits or pull requests — no `Co-Authored-By: Claude` trailers, no "Generated with Claude Code" lines, no similar attribution in commit messages or PR descriptions.

## Architecture

This repo contains **two coexisting programs**:

1. **`confluence_publish.py`** (repo root) — the original standalone script, run via `uv run confluence_publish.py` with its own inline PEP-723 dependencies (uses `requests`). It is **left untouched** and is explicitly excluded from ruff and ty (see `pyproject.toml`). Don't lint, format, or typecheck it, and don't modify it unless asked.

2. **`mzcld-confluence-cli`** (the `src/mzcld_confluence_cli/` package) — the new click-based CLI that supersedes it. Its `update` subcommand is a **verbatim port** of the script's publishing logic.

### Package layers

- `cli.py` — the click group (`main`); loads dotenv, registers subcommands. Command taxonomy mirrors [pchuri/confluence-cli](https://github.com/pchuri/confluence-cli), but **only `update` is implemented**; other commands (read, create, delete, search, attachments, etc.) are intentionally not built yet.
- `libclient.py` — `ConfluenceClient`, an **`httpx2`** wrapper (note: `httpx2` is Pydantic's maintained successor to the dead `httpx`; import name is `httpx2`). Holds auth + the API calls (`get_page`, `search_pages_by_title`, `update_page`). URLs are built as absolute strings off `base_url` rather than relying on relative-URL joining.
- `update.py` — the `update` command plus the entire markdown→Confluence-storage conversion pipeline. This is where nearly all the logic lives.

### The conversion pipeline (the crux)

`update.py`'s `process_file()` turns a markdown file into Confluence storage-format HTML through an **ordered sequence of regex transforms**, and the order encodes real dependencies. Preserve it when editing:

1. `gfm.convert()` (marko) → base HTML
2. `rewrite_anchor_links` — must run **before** `replace_internal_doc_links` so corrected fragments carry through the `.md`→URL rewrite
3. `replace_internal_doc_links` — needs the sibling-doc `page_id`/`title` map and the space key
4. TOC / note / chart / layout / callout comment-directive substitutions
5. `collapse_paragraph_newlines` — must run **before** `replace_code_blocks`; it stashes `<pre>` blocks so code survives the newline flattening
6. `replace_code_blocks` — relies on that `<pre>` stash still being intact

The transforms map GitHub/Mark-style markdown constructs to Confluence macros (code, panels/callouts, charts, layouts, TOC) and rewrite cross-doc links + heading anchors to their published Confluence URLs/ids.

### Frontmatter-driven publishing

Each markdown file's YAML frontmatter carries `title` and `page_id`. If `page_id` is absent, the page is looked up by `title` and the resolved id is **written back into the file**. Updates are skipped when the file's mtime predates the page's last version (unless `--force`). Multiple files are processed independently; the command exits non-zero if any fail.

## Project phasing

Work is planned in `_plans/`. The current state is Phase 1 (scaffold + verbatim port). Phase 2 will refactor the conversion pipeline out of `update.py` and add real conversion tests (currently only a smoke test exists). The `E501` per-file ignore on `update.py` in `pyproject.toml` is a deliberate Phase-2 cleanup marker for the verbatim-ported long lines.
