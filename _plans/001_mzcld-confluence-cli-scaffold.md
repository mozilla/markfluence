# Plan: `mzcld-confluence-cli`

Turn the standalone `confluence_publish.py` script into a proper uv-installed Python
CLI project with click subcommands, laying scaffolding to mirror
[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli)'s flat command
taxonomy while implementing **only `update`** for now.

## Phasing

- **Phase 1 (this plan):** scaffold the project and reimplement `confluence_publish.py`
  verbatim as the `update` subcommand. `confluence_publish.py` is left untouched.
- **Phase 2 (later, separate work):** refactor the ported conversion logic and add
  real tests. Not in scope here.

## Decisions (from the grill-me interview)

| Decision | Choice |
| --- | --- |
| Distribution name | `mzcld-confluence-cli` |
| CLI command | `mzcld-confluence-cli` |
| Import package | `mzcld_confluence_cli` (src layout) |
| Build backend | hatchling (uv default) |
| Python floor | `>=3.11` (matches current script) |
| HTTP library | **`httpx2`** (Pydantic's httpx successor; PyPI + import name `httpx2`, v2.5.0) — sync client, no HTTP/2 extra needed |
| Runtime deps | `click`, `python-dotenv`, `httpx2`, `marko` |
| Dev deps | `pytest`, `ruff` |
| Subcommands | flat taxonomy like confluence-cli, but **only `update`** wired now |
| Auth/config | same 3 env vars via python-dotenv, unchanged |
| HTTP calls | shared `ConfluenceClient`; conversion stays **inline in `update.py`** |
| Conversion port | copied **verbatim**, no refactor (drift-proofing) |
| `update` interface | identical: `update FILE... [--message] [--resolve] [--force]` |
| Tests now | smoke test only (`--help`) |
| Old script | `confluence_publish.py` left untouched alongside |

## Target layout

```
pyproject.toml            # deps, entry point, ruff config
README.md                 # install + usage
.gitignore                # .env, dist/, __pycache__, .venv, .ruff_cache, *.egg-info
.env.example              # CONFLUENCE_URL / CONFLUENCE_USERNAME / CONFLUENCE_TOKEN
src/mzcld_confluence_cli/
  __init__.py
  cli.py                  # click group; console entry `main`; loads dotenv
  client.py               # ConfluenceClient (httpx2) + .from_env(); get_page,
                          #   search_pages_by_title, update_page
  update.py               # `update` subcommand + ~600 lines of conversion, ported verbatim
tests/
  test_smoke.py           # CliRunner invocation of `--help`
confluence_publish.py     # UNTOUCHED
```

## Module responsibilities

### `cli.py`
- `@click.group()` named `main`; bare invocation prints `--help`.
- Calls `load_dotenv()` once.
- Registers the `update` command. (Future commands: `read`, `info`, `create`,
  `delete`, `search`, `find`, `attachments`, `attachment-upload`,
  `attachment-delete`, `comments`, `init`, `profile`, `api`, `convert` — not built.)

### `client.py`
- `ConfluenceClient` wrapping `httpx2.Client(auth=(username, token), base_url=<url>/wiki, timeout=...)`.
- `.from_env()` classmethod: reads `CONFLUENCE_URL`, `CONFLUENCE_USERNAME`,
  `CONFLUENCE_TOKEN`; raises a clear error listing missing vars.
- Methods ported from the script: `get_page(page_id)`,
  `search_pages_by_title(title)`, `update_page(page_id, title, html_body, version, message)`.
- Preserve 30s (GET/search) and 60s (PUT) timeouts and `raise_for_status()`.
- Use absolute request paths (no reliance on cross-host redirect following, which
  httpx2 does not do automatically — matches how the script already builds URLs).

### `update.py`
- click command: `update FILE... [--message TEXT] [--resolve] [--force]`.
  - `FILE...` = one or more markdown paths (`nargs=-1`, required).
  - `--message` default `"Updated via mzcld-confluence-cli"`.
  - `--resolve` flag: resolve/write page_id and print info, no publish.
  - `--force` flag: skip mtime check.
- Contains the verbatim-ported transform + `process_file` logic:
  `extract_frontmatter`, `extract_title_from_markdown`, `update_frontmatter_field`,
  `replace_confluence_notes`, `collapse_paragraph_newlines`, `replace_code_blocks`,
  `replace_github_callouts`, `github_slug`, `confluence_slug`, `extract_headings`,
  `build_headings_anchor_map`, `rewrite_anchor_links`, `build_docs_page_map`,
  `extract_space_key`, `replace_internal_doc_links`, `replace_chart_directives`,
  `replace_layout_blocks`, `process_file`.
- HTTP calls rerouted through `ConfluenceClient` instead of module-level `requests` fns.
- Preserve behavior exactly: frontmatter title/page_id resolution, page_id
  write-back to file, mtime-skip, `--force`, `--resolve`, `[filename] …` output
  prefixes (via `click.echo`), and non-zero exit if any file fails.

## Build steps

1. `uv init` style scaffold: create `pyproject.toml` (name, version, `requires-python`,
   deps, `[project.scripts]` entry point, `[tool.ruff]`, hatchling build config for
   src layout).
2. Create `src/mzcld_confluence_cli/` package files (`__init__.py`, `cli.py`,
   `client.py`, `update.py`).
3. Port conversion + `process_file` verbatim into `update.py`; adapt HTTP calls to
   `ConfluenceClient`; wrap CLI entry in click.
4. Add `.env.example`, `.gitignore`, `README.md`, `tests/test_smoke.py`.
5. `uv sync`, then verify:
   - `uv run mzcld-confluence-cli --help`
   - `uv run mzcld-confluence-cli update --help`
   - `uv run pytest`
   - `uv run ruff check`

## Explicitly out of scope

- Any subcommand other than `update`.
- Profiles / `init` / `--profile` (stay on 3-env-var auth).
- Refactoring the conversion regex pile (Phase 2).
- Real conversion tests / golden files (Phase 2).
- Deleting or modifying `confluence_publish.py`.
