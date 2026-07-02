# mzcld-confluence-cli

A CLI for publishing and manipulating Confluence pages and attachments.

It grows out of the standalone `confluence_publish.py` script (still present in
this repo, untouched). That script's markdown-publishing behavior is now the
`update` subcommand. The command taxonomy mirrors
[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli); only `update`
is implemented so far.

## Install

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

## Usage

```sh
uv run mzcld-confluence-cli --help
uv run mzcld-confluence-cli update --help
```

### `update`

Publish one or more markdown files to Confluence pages. The page title and page
ID are read from each file's YAML frontmatter:

```
---
title: My Page Title
page_id: 1234567890
---
```

If `page_id` is missing, the page is looked up by `title` and the resolved id is
written back to the file. Each file is processed independently; the command
exits non-zero if any file fails.

```sh
uv run mzcld-confluence-cli update docs/managing_an_incident.md
uv run mzcld-confluence-cli update docs/*.md --message "Bulk update"
uv run mzcld-confluence-cli update docs/foo.md --resolve   # resolve/write page_id, don't publish
uv run mzcld-confluence-cli update docs/foo.md --force     # ignore the mtime check
```

## Development

```sh
uv run pytest
uv run ruff check
uv run ruff format
```
