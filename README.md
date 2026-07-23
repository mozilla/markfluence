# markfluence

Markdown-centric Confluence cli tool. Works with Claude, works with GitHub
actions, works with you.

## Install

### From source

Requires Go 1.25+.

```sh
git clone https://github.com/mozilla/markfluence
cd markfluence
make install          # installs `markfluence` into your Go bin
# ...or, to build into ./bin without installing:
make build            # produces ./bin/markfluence
```

### Homebrew

TBD — published to a tap on the first release.

## Configure

markfluence needs a base URL, a username, and an API token. Each is resolved
with the precedence **flag > environment variable > `.env` file**:

| Setting | Flag | Environment / `.env` |
| --- | --- | --- |
| Base URL | `--url` | `CONFLUENCE_URL` |
| Username | `--username` | `CONFLUENCE_USERNAME` |
| API token | *(none — never a flag)* | `CONFLUENCE_TOKEN` |

markfluence reads a `.env` file from the current directory automatically (no need
to `source` it), or from an explicit path via `--env-file PATH` (a persistent flag
on every subcommand; a missing explicit path is an error). Copy `.env.example` to
`.env` and fill in:

```
CONFLUENCE_URL=https://your-org.atlassian.net
CONFLUENCE_USERNAME=you@example.com
CONFLUENCE_TOKEN=your-api-token
```

The API token is deliberately not accepted as a command-line flag; it comes only
from the environment or `.env`.

(Optional): `alias mf=markfluence`

## Usage

```sh
markfluence --help
markfluence create --help
markfluence update --help
markfluence fix --help
markfluence info --help
markfluence read --help
```

### `create`

```
Usage: markfluence create FILE... [flags]
```

Create new Confluence pages from Markdown files.

The page title comes from frontmatter, or from `--title` (which overrides the
frontmatter and requires a single `FILE`).

Confluence space can be specified on the command line (`--space SPACE`) or
in the frontmatter.

Optional parent can be specified on the command line (`--parent PAGE_ID`) or
in the frontmatter. In the frontmatter, you can specify the page id or
the Markdown file.

Page width defaults to `max`; set it with `--page-width narrow|wide|max` (which
overrides the frontmatter `page_width` and may apply across a batch).

All files are validated first — if any would fail (a page already exists at its
`page_id`, a title clash in the space, an unresolvable parent), nothing is
created.

On success `title`, `space`, `parent`, `page_id`, and `page_width` are written
back into each file — unless `--no-persist` is given, in which case nothing is
written back (and the file won't record its new `page_id`).

A whole tree can be created in one pass: give each child a `parent:` that points at
its parent's `.md` file, and `create` orders creation parents-first and fills in the
real ids (see the `parent` field below).

```sh
markfluence create docs/new_page.md --space ENG
markfluence create docs/child.md --space ENG --parent 123456
markfluence create docs/*.md --space ENG               # hierarchy via parent: paths
markfluence create note.md --space ENG --title "Ad-hoc note" --page-width wide
markfluence create note.md --space ENG --no-persist    # create without touching the file
```

### `update`

```
Usage: markfluence update FILE... [flags]
```

Update one or more Markdown files in Confluence.

Page id and title are read from frontmatter. A `page_id` is **required** (from
frontmatter or `--page-id`); `update` errors if none is set. `--title` and
`--page-id` override the frontmatter and require a single `FILE`; `--title`
renames the page, and a title otherwise falls back to the live page's title.
Page width is asserted only when `--page-width` is passed or a `page_width`
frontmatter line is present — otherwise the live page's width is left untouched.
`update` never writes back to the file.

Updates are skipped when a file hasn't changed since the page's last version
(compared by mtime) unless `--force` is given. Each file is processed
independently; the command exits non-zero if any file fails.

```sh
markfluence update docs/managing_an_incident.md
markfluence update docs/*.md --message "Bulk update"
markfluence update docs/foo.md --force              # ignore the mtime check
markfluence update page.md --page-id 123456         # override the target page
markfluence update page.md --title "New Title"      # override / rename
markfluence update docs/*.md --page-width wide      # set width across a batch
```

### `fix`

```
Usage: markfluence fix FILE... [flags]
```

Reconcile each file's frontmatter (`page_id`, `space`, `parent`, `page_width`, and
a missing `title`) to match its live Confluence page. The page is located by
`page_id`, or by searching for the `title` when `page_id` is absent. `fix` never
creates, updates, or moves pages — it's read-only on the server and writes a file
only when a field actually changed. `--dry-run` reports the changes without writing.

```sh
markfluence fix docs/*.md
markfluence fix docs/foo.md --dry-run
```

### `info`

```
Usage: markfluence info ARG [flags]
```

Print a page's metadata (id, title, status, space, parent, version, page width,
authors, dates, url). `ARG` is a numeric page id or a Markdown file whose
frontmatter has a `page_id`. `--properties` also lists all of the page's content
properties.

```sh
markfluence info 1234567890
markfluence info docs/foo.md --properties
```

### `read`

```
Usage: markfluence read ARG [flags]
```

Fetch a Confluence page and print its body to stdout. `ARG` is a numeric page id
or a Confluence page URL (the modern `/wiki/.../pages/<id>/...` form or a legacy
`?pageId=<id>` URL). It composes with shell redirection.

`--format` selects the output:

- `markdown` (**default**) — the page converted to GitHub-Flavored Markdown, with
  `title`/`page_id`/`space`/`page_width` frontmatter, i.e. a best-effort inverse of
  what `create`/`update` publish. The Confluence API has no markdown
  representation, so markfluence converts the storage body itself: constructs
  markfluence emits round-trip faithfully, while editor-authored content degrades
  gracefully — any macro markfluence doesn't map (panels, expand, status, …) and
  column layouts pass through as raw storage tags, with a macro/cell body kept as
  readable markdown, so they round-trip back through `create`/`update`. Some
  transforms are lossy (e.g. `CAUTION`
  alerts, internal links, and original image paths cannot be recovered), so this is
  a reading aid, not a guaranteed source round-trip.
- `storage` — the page's raw storage-format XHTML, exactly as stored.

```sh
markfluence read 1234567890                       # markdown, with frontmatter
markfluence read 1234567890 > page.md
markfluence read 1234567890 --format storage > page.storage.xml
markfluence read "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title"
```

### `--json` output

The persistent `--json` flag makes any command emit a single machine-readable
JSON document to stdout instead of the human output, for scripting and CI. It
pipes cleanly to `jq`:

```sh
markfluence info 1234567890 --json | jq '.results[0].page_width'
markfluence update docs/*.md --json | jq '.summary'
```

Output is a stable, versioned **envelope**. `results` always holds one object per
target (a single element for `info`/`read`); `summary` carries batch counts:

```json
{
  "schema_version": 1,
  "markfluence_version": "1.4.0",
  "command": "update",
  "results": [
    {
      "ok": true,
      "status": "published",
      "file": "docs/foo.md",
      "page_id": "123",
      "title": "Foo",
      "space": "ENG",
      "url": "https://wiki.example.net/wiki/spaces/ENG/pages/123/Foo",
      "version": { "previous": 3, "new": 4 },
      "page_width": { "value": "max", "default": false },
      "attachments": [ { "action": "updated", "filename": "diagram.png" } ],
      "warnings": [],
      "broken": [],
      "error": null,
      "code": null
    }
  ],
  "summary": { "total": 1, "succeeded": 1, "failed": 0, "skipped": 0 }
}
```

The full contract is published as a JSON Schema (draft 2020-12) at
[`schema/json-output/v1.json`](schema/json-output/v1.json) — the `results` item
and `summary` shapes are selected by `command`, and the stderr error object is
`#/$defs/errorObject`. A test validates markfluence's actual output against it, so
the schema cannot drift from the implementation.

Notes on the schema:

- **Per-command stable.** Each command always emits the same keys in the same
  shapes (empty values are `null` or `[]`); the key *set* differs per command.
  `schema_version` is bumped on any breaking change.
- **Status verbs** are per-command: `published`/`skipped` (`update`),
  `created`/`not_created` (`create`), `changed`/`consistent` (`fix`), plus
  `failed`. `info`/`read` results carry data only (no status verb).
- **Compound values are objects**, never display strings — `version`,
  `page_width`, and the `created`/`updated` author stamps on `info`.
- **`create`'s two-phase abort** (a validation failure means nothing is created)
  lists every input file — failed ones with an `error`, the rest as
  `not_created` — and sets `summary.aborted: true`.
- **Warnings and broken image/link notices** are data (`warnings`/`broken`
  arrays on each result), not stderr log lines.

Errors and exit codes:

- **Per-file operational failures** appear in `results` as
  `{ "ok": false, "error": "…", "code": "…" }`; the command exits `1` if any
  file failed.
- **Fatal/pre-flight failures** (bad flags, credential resolution) print a typed
  error object to **stderr** and exit `2`:

  ```json
  { "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG" }
  ```

- Error `code` values: `CONFIG`, `AUTH`, `NOT_FOUND`, `VALIDATION`, `CONVERT`,
  `IO`, `NETWORK`, `API`.

## GitHub Actions

markfluence can run in CI to keep Confluence pages in sync with markdown in
your repo: on a push to your default branch, publish the changed docs. You
will need to know the Confluence `page_id` for each page you want to update.

### Credentials

Store environment variables as [encrypted secret][secrets] (never commit them).
markfluence reads them straight from the environment — no `.env` in CI.

- `CONFLUENCE_TOKEN`
- `CONFLUENCE_URL`
- `CONFLUENCE_USERNAME`

[secrets]: https://docs.github.com/en/actions/security-guides/using-secrets-in-github-actions

### Workflow

```yaml
name: Publish docs to Confluence

on:
  push:
    branches: [main]
    paths: ['docs/**.md']        # only when docs change

# Avoid overlapping publishes racing on the same pages.
concurrency:
  group: confluence-publish
  cancel-in-progress: false

jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'

      # No release binaries are published yet, so install from source. Pin a tag
      # (…@v1.2.3) once releases exist, rather than @latest, for reproducibility.
      - name: Install markfluence
        run: go install github.com/mozilla/markfluence@latest

      - name: Publish
        env:
          CONFLUENCE_URL: ${{ secrets.CONFLUENCE_URL }}
          CONFLUENCE_USERNAME: ${{ secrets.CONFLUENCE_USERNAME }}
          CONFLUENCE_TOKEN: ${{ secrets.CONFLUENCE_TOKEN }}
        run:
          markfluence update --page-id=12345 --force docs/some_doc.md
```

Notes:

- **Exit codes.** `update` exits non-zero if any file fails, so the job fails
  loudly. Add `--json` to get machine-readable per-file results on stdout (see
  [`--json` output](#--json-output)) if a later step needs to parse them.

A reusable composite/Docker action wrapping this is tracked in
[#29](https://github.com/mozilla/markfluence/issues/29).

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
- `<!-- markfluence-version -->` — replaced with the build stamp,
  `markfluence vVERSION COMMITDATE`.

**Raw Confluence storage format.** You can paste Confluence
[storage format](https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html)
markup (`<ac:…>` / `<ri:…>` elements — any macro, layout, etc.) straight from a
page's **⋯ → View storage format** into your markdown, and it's emitted verbatim.
Two conventions:

- **Leave a blank line** between an `ac:`/`ri:` tag and any markdown you want
  converted (e.g. a macro or layout-cell body). With a blank line the content is
  parsed as markdown; tight against the tags it passes through literally.
- **Put the opening tag on its own line** (or self-close it) so it isn't wrapped in
  a paragraph.

For example, a two-column layout with markdown in each cell:

```
<ac:layout>
<ac:layout-section ac:type="two_equal">
<ac:layout-cell>

Left column with **markdown**.

</ac:layout-cell>
<ac:layout-cell>

Right column.

</ac:layout-cell>
</ac:layout-section>
</ac:layout>
```

Storage markup shown inside a fenced code block stays literal (it isn't activated).

## Development

Requires Go 1.25+. Common tasks (run `make` with no target for the list):

```sh
make build              # build ./bin/markfluence
make test               # go test ./...
make lint               # golangci-lint (installs the pinned version into ./bin)
make vet                # go vet ./...
make fmt                # go fmt ./...
make regen-regressions  # regenerate the converter's golden test outputs
```

The converter's behavior is pinned by a golden-file regression suite under
`internal/convert/testdata/regression/`. Run the built binary against Confluence
by putting a `.env` in the working directory (see [Configure](#configure)).

## Inspirations

[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli) -- command
line interface. markfluence tries to match subcommands and arguments from
confluence-cli, but focuses on Markdown document publishing and less on
providing a CLI access to the full Confluence v1/v2 API.

[kovetskiy/mark](https://github.com/kovetskiy/mark) -- Markdown support for
Confluence and how things are represented. markfluence tries to match key
design decisions, but has defaults I like better and works in different
scenarios better.
