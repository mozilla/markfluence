# Plan: `info` subcommand

Add an `info` subcommand that prints metadata about a single Confluence page,
identified by a page id or by a markdown file whose frontmatter carries a
`page_id`. Modeled on confluence-cli's `info` command
(https://github.com/pchuri/confluence-cli#get-page-information), which is
metadata-only (no page body).

`info` is **read-only** everywhere: it never touches Confluence or local files.

## Scope

**Build now:** `info ARG` in a new `src/markfluence/info.py`, registered in
`cli.py`, plus a `get_user` lookup on `ConfluenceClient`.

**Deferred (tracked in `todo.md`):** `--json` / machine-readable output. Dropped
for now because no other command has it; when added it should be a consistent flag
across commands, not a one-off here.

## Decisions locked (from the interview)

### Input — a single argument

`info ARG` takes exactly one argument (no multi-arg, no URL support):

- `os.path.isfile(ARG)` → treat as a markdown file; read `page_id` from its
  frontmatter. Missing/blank `page_id` (blank-aware, like `fix`) → error.
- else `ARG.isdigit()` → treat as a literal page id.
- else → error (`<ARG> is not a file or a numeric page id`).

### Output — human-readable text only

Aligned `label: value` lines; empty fields omitted; **dates shown as raw ISO 8601**
exactly as the API returns them (no timezone/format munging):

```
id:       123456
title:    Things
status:   current
space:    ENG
parent:   none (top-level)          # or the numeric parent id
version:  3
created:  2024-01-15T09:22:10Z by Alice Nguyen
updated:  2024-06-20T14:03:55Z by Bob Smith
message:  Reworded the intro        # omitted when the version has no message
url:      https://org.atlassian.net/wiki/spaces/ENG/pages/123456/Things
```

Field sources (all from one `get_page` v2 response, except author names):

- `id`, `title`, `status` — direct.
- `space` (key) — from `_links.webui` via `extract_space_key` (no extra call).
- `parent` — `parentId`, or `none (top-level)` when null.
- `version` — `version.number`.
- `created` — `page.createdAt` + creator name (`page.authorId`).
- `updated` — `version.createdAt` + last-editor name (`version.authorId`).
- `message` — `version.message` (omitted when empty).
- `url` — `_links.base` + `_links.webui` (same construction as the other commands).

`type` from confluence-cli is **dropped**: v2's pages endpoint doesn't return it and
markfluence only handles pages, so it would be a constant.

### Authors — resolve ids to display names

The v2 response only gives opaque `authorId`s. Resolve each to a display name via a
new `ConfluenceClient.get_user(account_id)` hitting the v1 endpoint
`GET /wiki/rest/api/user?accountId=` (v2 has no clean by-id user fetch):

- Show **both** the creator (`page.authorId`) and the last editor
  (`version.authorId`).
- **Dedupe:** when creator == last editor, look the id up once.
- **Fallback:** if the lookup fails for any reason, show the raw account id rather
  than failing the command (`get_user` returns `None` on any HTTP error).

### Errors

- File with missing/blank `page_id` → clear error.
- Arg neither a file nor numeric → clear error.
- Resolved page id not found → clean `page <id> not found` via `get_page_or_none`
  (not a raw HTTP 404).

Exit non-zero on failure.
