# markfluence

A CLI for publishing and manipulating Confluence pages and attachments.

## Install

### From PyPI

TBD

### From GitHub

TBD

### From local git repository

Uses [uv](https://docs.astral.sh/uv/).

```sh
uv sync
```

## Configure

Copy `.env.example` to `.env` and fill in:

```
CONFLUENCE_URL=https://your-org.atlassian.net
CONFLUENCE_USERNAME=you@example.com
CONFLUENCE_TOKEN=your-api-token
```

(Optional): `alias mf=markfluence`

## Usage

```sh
markfluence --help
markfluence create --help
markfluence update --help
```

### `create`

```
Usage: markfluence create [OPTIONS] FILENAMES...
```

Create new Confluence pages from Markdown files.

The page title comes from frontmatter.

Confluence space can be specified on the command line (`--space SPACE`) or
in the frontmatter.

Optional parent can be specified on the command line (`--parent PAGE_ID`) or
in the frontmatter. In the frontmatter, you can specify the page id or
the Markdown file.

All files are validated first — if any would fail (a page already exists at its
`page_id`, a title clash in the space, an unresolvable parent), nothing is
created.

On success `page_id`, `space`, and `parent` are written back into each file.

A whole tree can be created in one pass: give each child a `parent:` that points at
its parent's `.md` file, and `create` orders creation parents-first and fills in the
real ids (see the `parent` field below).

```sh
markfluence create docs/new_page.md --space ENG
markfluence create docs/child.md --space ENG --parent 123456
markfluence create docs/*.md --space ENG   # hierarchy via parent: paths
```

### `update`

```
Usage: markfluence update [OPTIONS] FILENAMES...
```

Update one or more Markdown files in Confluence.

Space, parent, page id, and title are all read from frontmatter.

Each file is processed independently; the command exits non-zero if any file fails.

```sh
markfluence update docs/managing_an_incident.md
markfluence update docs/*.md --message "Bulk update"
markfluence update docs/foo.md --force     # ignore the mtime check
```

## Markdown page structure

Each Markdown file is one Confluence page: an optional YAML **frontmatter** block
followed by the Markdown **body**.

```
---
title: My Page Title
space: ENG
parent: null
page_id: 1234567890
page_width: max
---

# Body starts here
...
```

### Frontmatter

Frontmatter is a block delimited by `---` lines containing flat `key: value` pairs
(no nesting, lists, or multi-line values). Full-line `#` comments and trailing
inline ` # ...` comments (whitespace, then `#`) are ignored.

To include a literal ` #` (or leading/trailing whitespace, or a leading quote) in a
value, quote it with single or double quotes — e.g. `title: "Detect # Verify"`.
Single quotes are literal (`''` escapes a quote); double quotes honor `\"` and `\\`.
markfluence adds quotes automatically when it writes a value back if they're needed
to round-trip.

| Field | Value domain | Notes |
| --- | --- | --- |
| `space` | a space key (e.g. `ENG`, or a personal space like `~1234abcd`) | Target space for `create` (or pass `--space`); written back by `create`. Always a key, never a numeric space id. |
| `parent` | `null`, a numeric page id, or a relative `.md` path | `null` = top-level page; a page id = an existing parent page; a `.md` path = a parent authored in the same run (`create` resolves it in dependency order, then rewrites the value to `<page_id>  # <original.md>`). Used by `create` (or `--parent`). |
| `page_id` | a numeric page id, or `null` | The target page. `update` looks it up by `title` and writes it back when missing; `create` writes it after creating the page. `null`/absent means "no page yet." |
| `title` | text (**required**) | The Confluence page title. |
| `page_width` | `narrow`, `wide`, or `max` | The published page width (the UI's "Adjust width" options; `narrow`/`wide`/`max` map to the `default`/`full-width`/`max` appearance properties). Absent or blank defaults to `max`. `create`/`update` assert it on every publish (so a width set in the Confluence UI is overwritten unless the frontmatter matches); `fix` writes back the live page's width. |

To create a page, you only need to specify the `title` in the frontmatter.

### Body

The body is [GitHub-Flavored Markdown](https://github.github.com/gfm/), converted
to Confluence storage format. Supported constructs:

**Tables** and **fenced code blocks** (rendered as Confluence code macros, with
language).

**GitHub alerts** — `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`,
`[!CAUTION]` — become info/tip/note/warning panels.

Example:

```markdown
> [!NOTE]
> This is a note.
```

**Images** — `![alt](./path.png)` uploads a local file as an attachment (or
references a remote URL); a missing/unsupported image becomes `IMAGE BROKEN: …`
text.

Extra properties ride in the title as JSON:

```markdown
![alt](x.png '{"title":"…","width":"100","align":"center"}')
```

* `align` is left/center/right;
* `width`/`height` are pixels

A plain title (`![alt](x.png "tooltip")`) becomes the image tooltip.

Examples:

```
![alt text](./path.png)

![alt text](https://example.com/image.png)

![alt text](./path.png "title")

![alt text](./path.png '{"title":"sometitle","width":100}')
```

**Links to sibling `.md` files** are rewritten to the target page's Confluence
URL; **heading anchors** are rewritten to Confluence's anchor scheme.

**Comment directives:**
- `<!-- confluence-toc -->` — table-of-contents macro.
- `<!-- confluence-note --> … <!-- /confluence-note -->` — note panel.
- `<!-- chart:pie|bar [stacked] -->` immediately before a table — chart macro.
- `<!-- ac:layout --> … <!-- ac:layout-section type:… --> …` — multi-column
  layouts (mirrors [mark](https://github.com/kovetskiy/mark)'s directives).

## Development

```sh
uv run pytest
uv run ruff check
uv run ruff format
```

## Inspriations

[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli) -- command
line interface

[kovetskiy/mark](https://github.com/kovetskiy/mark) -- Markdown support for
Confluence and how things are represented
