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

### Shell completions

markfluence generates its own completion scripts for bash, zsh, fish, and
PowerShell. The release archives ship them under `completions/`, and a Homebrew
install puts them where each shell looks, so a `brew install` needs nothing
further.

Otherwise, to load them into the current shell:

```sh
source <(markfluence completion bash)   # bash
source <(markfluence completion zsh)    # zsh
markfluence completion fish | source    # fish
```

To install them permanently, `markfluence completion <shell> --help` prints the
path your shell reads on your platform. For example, on Linux with bash:

```sh
markfluence completion bash > /etc/bash_completion.d/markfluence
```

Completion offers Markdown files wherever a `FILE` or `PAGE` argument goes (the
page-id and URL forms of `PAGE` you type out), the values of flags like
`--page-width` and `--format`, and directories for `--dest`. Attachment names
aren't completed: they live on the server, and completion never makes a network
call.

## Configure

markfluence needs a Confluence site URL, a username, and an API token. Each is
resolved with the precedence **flag > environment variable > `.env` file**:

| Setting | Flag | Environment / `.env` |
| --- | --- | --- |
| Site URL | `--url` | `CONFLUENCE_URL` |
| Username | `--username` | `CONFLUENCE_USERNAME` |
| API token | *(none — never a flag)* | `CONFLUENCE_TOKEN` |
| Cloud ID *(optional)* | `--cloud-id` | `CONFLUENCE_CLOUD_ID` |

markfluence reads a `.env` file from the current directory automatically (no need
to `source` it), or from an explicit path via `--env-file PATH`.

Copy `.env.example` to `.env` and fill in:

```
CONFLUENCE_URL=https://your-org.atlassian.net
CONFLUENCE_USERNAME=you@example.com
CONFLUENCE_TOKEN=your-api-token
# Optional. Set this only when using *scoped* API tokens.
# CONFLUENCE_CLOUD_ID=
```

> [!NOTE]
> The API token is deliberately not accepted as a command-line flag; it comes
> only from the environment or `.env`.

(Optional): `alias mf=markfluence`

### Scoped tokens and service accounts

For a normal personal API token, you can leave `CONFLUENCE_CLOUD_ID` unset.

For a **scoped** API token for an Atlassian [service account][svcacct], you
need to set `CONFLUENCE_CLOUD_ID`. You would use a scoped API token for a
service account to publish from CI or other automated system. Scoped tokens are
rejected with a **401** against your site domain--markfluence must use
Atlassian's `api.atlassian.com` gateway, and the cloud ID is required there.
`CONFLUENCE_URL` still holds the site URL: markfluence uses it to write correct
links into the pages it publishes.

To find your cloud ID, you can do this:

```console
$ curl -s https://your-org.atlassian.net/_edge/tenant_info
{"cloudId":"d8febd08-5555-5555-5555-db37c2369ce5"}
```

The cloud ID is not a secret.

The scopes markfluence needs:

| Used for | Classic scope |
| --- | --- |
| Reading, creating, and updating pages | `read:confluence-content.all`, `write:confluence-content` |
| Resolving space keys | `read:confluence-space.summary` |
| Page width (content properties) | `read:confluence-props`, `write:confluence-props` |
| Image attachments | `write:confluence-file` |
| Author names in `info` | `read:confluence-user` |

> [!NOTE]
> Currently, markfluence doesn't support deleting anything, so it doesn't need
> delete scopes. This might change in the future.

[svcacct]: https://support.atlassian.com/user-management/docs/understand-service-accounts/

## Usage

General:

```sh
markfluence --help
markfluence schema --help
```

Manipulating Confluence pages:

```sh
markfluence create --help
markfluence update --help
markfluence fix --help
markfluence info --help
markfluence read --help
markfluence export --help
```

Manipulating Confluence page attachments:

```sh
markfluence attachment-list --help
markfluence attachment-upload --help
markfluence attachment-download --help
```

Every command that takes a page accepts three forms: a numeric page id, a
Confluence page URL, or a Markdown file whose frontmatter has a `page_id`.

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
the Markdown file. The parent may also be a **folder** — the Confluence Cloud
content type — in which case give its id the same way you would a page's.

Page width defaults to `max`; set it with `--page-width narrow|wide|max` (which
overrides the frontmatter `page_width` and may apply across a batch).

All files are validated first — if any would fail (a problem with its `page_id`, a
title clash in the space, an unresolvable parent), nothing is created. Both kinds
of clash name the page in the way, so you can go look at it:

```console
$ markfluence create docs/runbook.md
[docs/runbook.md] a page already exists at page_id 123 ("Deploy Runbook"): https://wiki.example.net/wiki/spaces/ENG/pages/123/Deploy+Runbook
Aborting: 1 file(s) failed validation; nothing was created.
```

A file whose `page_id` doesn't resolve is also a failure, not a fresh page:
`create` will not publish a second copy and overwrite the id it can't explain.
Remove the `page_id` to create a new page, or correct it. A `page_id` that isn't a
numeric id at all (a pasted URL, a leftover placeholder) is reported as such
without asking Confluence about it.

On success `title`, `space`, `parent`, `page_id`, and `page_width` are written
back into each file — unless `--no-persist` is given, in which case nothing is
written back (and the file won't record its new `page_id`).

A whole tree can be created in one pass: give each child a `parent:` that points at
its parent's `.md` file, and `create` orders creation parents-first and fills in the
real ids (see the `parent` field below).

`--dry-run` validates every file (the same checks a real run makes, so it exits
non-zero on the same failures) and previews what would be created — pages,
attachment uploads, page widths, and frontmatter write-backs — without writing to
Confluence or to any file. Because nothing is created, a previewed page has no id
or URL yet; an in-set child's `parent` is unresolved, but its source file is
reported in the `parent_file` output field (present in every run, in `--json`).

```sh
markfluence create docs/new_page.md --space ENG
markfluence create docs/child.md --space ENG --parent 123456
markfluence create docs/*.md --space ENG               # hierarchy via parent: paths
markfluence create note.md --space ENG --title "Ad-hoc note" --page-width wide
markfluence create note.md --space ENG --no-persist    # create without touching the file
markfluence create docs/*.md --space ENG --dry-run     # preview; write nothing
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

A `page_id` that no longer resolves fails that file with what to do about it
(`page_id 999 not found (deleted, trashed, or wrong); correct it, or remove it and
use create instead`), and one that isn't a numeric id at all is reported without
asking Confluence. Since `update` writes nothing back, fixing the id is always
safe: the file is exactly as you left it.

Updates are skipped when a file hasn't changed since the page's last version
(compared by mtime) unless `--force` is given. Each file is processed
independently; the command exits non-zero if any file fails.

`--dry-run` previews what would be published — the version bump, attachment
uploads, and any page-width change — without writing to Confluence. It honors the
mtime skip and `--force` just like a real run, so its forecast matches what a real
run would do.

```sh
markfluence update docs/managing_an_incident.md
markfluence update docs/*.md --message "Bulk update"
markfluence update docs/foo.md --force              # ignore the mtime check
markfluence update page.md --page-id 123456         # override the target page
markfluence update page.md --title "New Title"      # override / rename
markfluence update docs/*.md --page-width wide      # set width across a batch
markfluence update docs/*.md --dry-run              # preview; write nothing
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
Usage: markfluence info PAGE [flags]
```

Print a page's metadata (id, title, status, space, parent, version, page width,
authors, dates, url). `PAGE` is a numeric page id, a Confluence page URL, or a
Markdown file whose frontmatter has a `page_id`. `--properties` also lists all of
the page's content properties.

```sh
markfluence info 1234567890
markfluence info docs/foo.md --properties
```

### `read`

```
Usage: markfluence read PAGE [flags]
```

Fetch a Confluence page and print its body to stdout. `PAGE` is a numeric page id,
a Confluence page URL (the modern `/wiki/.../pages/<id>/...` form or a legacy
`?pageId=<id>` URL), or a Markdown file whose frontmatter has a `page_id`. It
composes with shell redirection.

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
  alerts, internal links, original image paths, and table cell background colors
  cannot be recovered), so this is
  a reading aid, not a guaranteed source round-trip.
- `storage` — the page's raw storage-format XHTML, exactly as stored.

```sh
markfluence read 1234567890                       # markdown, with frontmatter
markfluence read 1234567890 > page.md
markfluence read 1234567890 --format storage > page.storage.xml
markfluence read "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title"
```

### `children`

```
Usage: markfluence children PAGE [flags]
```

List the pages and folders under a page or folder. `PAGE` is a numeric id, a
Confluence page **or folder** URL, or a markdown file whose frontmatter has a
`page_id`.

```sh
markfluence children 1234567890                  # direct children
markfluence children 1234567890 --depth 3
markfluence children 1234567890 --depth all
markfluence children "https://org.atlassian.net/wiki/spaces/ENG/folder/1234567890"
markfluence children docs/index.md               # children of the page index.md publishes to
markfluence children 1234567890 --json | jq -r '.results[] | select(.type=="page") | .id'
```

```
TYPE    ID          TITLE
folder  2876047392  Articles
page    1675427879  MozCloud planning
page    1671692338    MozCloud observability focus and issues
```

Titles indent by depth; `TYPE` and `ID` stay aligned so the output is still
greppable. Siblings appear in the order Confluence displays them, which takes a
merge — pages and folders come from separate requests.

**Folders are listed, not just traversed.** A folder can hold the only pages in a
subtree, so listing pages alone would show nothing for a folder that contains
folders. A folder also counts as a level: at the default `--depth 1` a child
folder appears as a row, and `--depth 2` shows what is inside it.

`--depth` takes a positive number or `all`, defaulting to `1`. `0` is rejected
rather than treated as "unlimited" — a common convention elsewhere, and silently
walking an entire space for someone who meant "none" is worse than an error.
`--depth all` is genuinely unbounded: it costs two requests per node, and the
default of `1` is what keeps the casual case cheap.

Trashed pages and folders are not listed. Finding nothing is a success, not a
failure: the command prints `No children.` and exits 0, so `--json` reporting an
empty `results` array is how a script tests for an empty subtree.

### `export`

```
Usage: markfluence export PAGE [flags]
```

Write a page and the attachments it uses to a directory — the one-command form
of `read` plus `attachment-download`.

```console
$ markfluence export 1234567890 --dest ./out
wrote      out/markfluence-test-page.md
downloaded out/assets/diagram.png
           (skipped 2 unreferenced attachment(s); --all-attachments to include)
```

The page is written as Markdown with `title`/`space`/`parent`/`page_id`/
`page_width` frontmatter — byte-identical to what `read` prints — so an exported
file can be edited and published straight back with `update`.

Attachments are written to the paths their images were published from, so the
exported tree matches the layout of the repo the page came from and previews
locally in GitHub or VSCode. There is deliberately no `--attachments-dir`:
collecting attachments into one directory would mean rewriting the image `src`s,
and a later `update` would then publish them under different attachment names,
orphaning the originals.

Only attachments the page actually references are exported. That includes images,
attachment links, and references inside macros markfluence passes through
untouched. `--all-attachments` takes everything on the page instead;
`--skip-attachments` writes the page file only.

`--file` names the page file, defaulting to a slug of the title
(`markfluence-test-page.md`), or the page id when the title slugs to nothing.
`--dest` defaults to the current directory and is created if missing. Existing
files are skipped unless `--force`, and `--dry-run` previews without writing.

If the page references an attachment that isn't attached — already broken in
Confluence — the export still succeeds and reports it as a warning.

Markdown is the only output format. Use `read --format storage` to inspect the
raw storage Confluence holds.

### `attachment-list`

```
Usage: markfluence attachment-list PAGE [flags]
```

List a page's attachments.

```console
$ markfluence attachment-list 1234567890
NAME                            SIZE  VER  TYPE       SOURCE
assets%2Fdiagram.png           24.1 KB   3  image/png  assets/diagram.png
notes.pdf                       1.2 MB   1  application/pdf  -
```

`NAME` is the name Confluence stores. For an image markfluence published that is
the percent-encoded source path (see [Body](#body)), and `SOURCE` is the Markdown
image path it came from — so the table shows at a glance which attachments a
publish manages and which it will leave alone.

`SOURCE` is a dash when no source path is recorded: either the attachment was
uploaded by hand, or it was published before markfluence recorded source paths.
Those two look the same here; `--json` has a `managed` field that tells them
apart. Attachments left behind by the encoding change show up this way, which is
how you find them.

### `attachment-upload`

```
Usage: markfluence attachment-upload PAGE FILE... [flags]
```

Upload or replace attachments on a page, complementing the automatic sync that
`create` and `update` perform for a page's images.

Each file is attached under its base name. A file whose contents already match
what's on the page is skipped, using the same checksum bookkeeping
`create`/`update` use, so uploading by hand and publishing agree on what's
current. `--force` uploads anyway (bumping the attachment's version), which is
how you repair an attachment whose stored bytes drifted while its checksum still
matches. `--dry-run` previews without writing.

`--name` sets the attachment name for a single file and takes a **path**, which
markfluence encodes for you — so `--name assets/x.png` produces the attachment
that an image written as `![](assets/x.png)` resolves to. The recorded source
path always matches the stored name, so a later publish won't create a duplicate
under a different one.

```sh
markfluence attachment-upload 1234567890 diagram.png
markfluence attachment-upload 1234567890 report.pdf notes.txt
markfluence attachment-upload 1234567890 img.png --name assets/diagram.png
markfluence attachment-upload 1234567890 diagram.png --force
```

### `attachment-download`

```
Usage: markfluence attachment-download PAGE [NAME...] [flags]
```

Download a page's attachments. Each `NAME` is an attachment name as
`attachment-list` reports it; with no `NAME`, every attachment is downloaded.

An attachment markfluence published is written back to the Markdown image path
recorded in its comment, so the downloaded tree matches what the page's Markdown
references and previews locally:

```console
$ markfluence attachment-download 1234567890 --dest ./out
downloaded /out/assets/diagram.png
downloaded /out/notes.pdf
```

An attachment with no recorded path — hand-uploaded, or published before
markfluence recorded them — is written under its stored name. `--flat` writes
everything under stored names. `--dest` defaults to the current directory and is
created if missing. An existing file is skipped unless `--force`, and
`--dry-run` previews without writing.

A recorded path that would resolve outside `--dest` is refused for that
attachment: the path comes from an attachment comment, which anyone who can edit
the page controls.

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

The binary carries that same schema, so a consumer can fetch the contract
without knowing anything about this repository (see [`schema`](#schema)).

Notes on the schema:

- **Per-command stable.** Each command always emits the same keys in the same
  shapes (empty values are `null` or `[]`); the key *set* differs per command.
  `schema_version` is bumped on any breaking change.
- **Status verbs** are per-command: `published`/`skipped` (`update`),
  `created`/`not_created` (`create`), `changed`/`consistent` (`fix`),
  `created`/`updated`/`skipped` (`attachment-upload`),
  `downloaded`/`skipped` (`attachment-download`), plus `failed`. `info`, `read`,
  and `attachment-list` results carry data only (no status verb).
- **One result per target**, and the target is per-command: the page for
  `info`/`read`/`export` (always one), the file for `update`/`create`/`fix`, and
  the attachment for the three `attachment-*` commands — so
  `.results[] | .filename` works and `summary.total` is the attachment count.
  `export` nests the files it wrote in an `attachments` array on its page
  result, the way `update`/`create` do.
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

### `schema`

```
Usage: markfluence schema
```

Print the JSON Schema for [`--json` output](#--json-output) to stdout. The schema
is embedded in the binary, so a script, a CI job, or an agent can fetch the
contract at runtime — validating markfluence's own output, or generating types
from it — without reading it out of this repository:

```console
$ markfluence schema | jq -r '.properties.command.enum | join(" ")'
info read update create fix attachment-list attachment-upload attachment-download export

$ markfluence update docs/*.md --json > out.json
$ markfluence schema > schema.json
$ check-jsonschema --schemafile schema.json out.json
```

The printed document is byte-identical to
[`schema/json-output/v1.json`](schema/json-output/v1.json) at the revision the
binary was built from, and describes the `schema_version` that binary emits.
Because the tests validate real output against the same embedded copy, the
schema you get from a binary is the one its output was checked against.

Nothing here talks to Confluence, so no credentials are needed. The output is
already JSON; `--json` changes nothing.

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

Prefer a [service account][svcacct] over a personal token here, so published pages
aren't authored by an individual and publishing doesn't break when that person
rotates their token or moves on. That means a **scoped** token, which also needs
`CONFLUENCE_CLOUD_ID` (see [Scoped tokens and service
accounts](#scoped-tokens-and-service-accounts)). The cloud ID is not sensitive, so
make it a repository **variable** rather than a secret.

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
          # A variable, not a secret: the cloud ID is public. Omit it if you're
          # using an unscoped personal token.
          CONFLUENCE_CLOUD_ID: ${{ vars.CONFLUENCE_CLOUD_ID }}
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
| `parent` | `null`, a numeric page **or folder** id, or a relative `.md` path | `null` = top-level page; an id = an existing parent, which may be a page or a Cloud folder (the value is just an id either way — nothing records which kind it is); a `.md` path = a parent authored in the same run (`create` resolves it in dependency order, then rewrites the value to `<page_id>  # <original.md>`). Used by `create` (or `--parent`). |
| `page_id` | a numeric page id, or `null` | The target page. `update` looks it up by `title` and writes it back when missing; `create` writes it after creating the page. `null`/absent means "no page yet." |
| `title` | text (**required**) | The Confluence page title. |
| `page_width` | `narrow`, `wide`, or `max` | The published page width (the UI's "Adjust width" options; `narrow`/`wide`/`max` map to the `default`/`full-width`/`max` appearance properties). Absent or blank defaults to `max`. `create`/`update` assert it on every publish (so a width set in the Confluence UI is overwritten unless the frontmatter matches); `fix` writes back the live page's width. |

To create a page, you only need to specify the `title` in the frontmatter.

### Body

The body is [GitHub-Flavored Markdown](https://github.github.com/gfm/), converted
to Confluence storage format. Supported constructs:

**Fenced code blocks** are rendered as Confluence code macros and support the
syntax highlighting, but only the languages Confluence supports.
[GFM fenced code](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-and-highlighting-code-blocks)

**Tables** use GFM syntax and are rendered as Confluence tables.
[GFM tables](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/organizing-information-with-tables)

**Table cell background colors** can be specified using an HTML comment at the
start of the cell. They will be invisible in Markdown preview, but will have
the specified background color in Confluence.

```markdown
| Service | Status                     |
| ------- | -------------------------- |
| auth    | <!-- bg:light-green --> ok |
| billing | <!-- bg:light-red --> down |
```

The color is a swatch name from the Confluence editor's cell background palette,
or a literal `#rrggbb` hex for anything else. The 21 swatches, one row here per
column of the editor's picker:

| Light | Medium | Bold |
| --- | --- | --- |
| `white` `#ffffff` | `light-grey` `light-gray` `#f4f5f7` | `grey` `gray` `#b3bac5` |
| `light-blue` `#deebff` | `blue` `#b3d4ff` | `bold-blue` `#4c9aff` |
| `light-teal` `#e6fcff` | `teal` `#b3f5ff` | `bold-teal` `#79e2f2` |
| `light-green` `#e3fcef` | `green` `#abf5d1` | `bold-green` `#57d9a3` |
| `light-yellow` `#fffae6` | `yellow` `#fff0b3` | `bold-yellow` `#ffc400` |
| `light-red` `#ffebe6` | `red` `#ffbdad` | `bold-red` `#ff8f73` |
| `light-purple` `#eae6ff` | `purple` `#c0b6f2` | `bold-purple` `#998dd9` |

Details:

- Confluence colors **cells**, not rows or columns; a colored column is
  implemented with a marker per cell in the column and a colored row is
  implemented with a marker per cell in the row.
- The marker works in header cells too.
- A cell holding nothing but a marker is an empty colored cell.
- The color marker has to be the first thing in the cell. Anywhere else it's
  ignored with a warning, since a stray comment would otherwise do nothing
  visible.
- An unknown color name is dropped with a warning and the cell publishes
  uncolored.

**GitHub alerts** — `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`,
`[!CAUTION]` — become info/tip/note/warning panels.
[GFM alerts](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts)

Example:

```markdown
> [!NOTE]
> This is a note.
```

**Images** — `![alt](./path.png)` uploads a local file as an attachment (or
references a remote URL); a missing/unsupported image becomes `IMAGE BROKEN: …`
text.

Image paths resolve relative to the Markdown file, the same way they do when you
view the file on GitHub, so a page in a subdirectory can share an asset
directory above it:

```
docs/                      ← run markfluence from here
  assets/logo.png
  guide/page.md            → ![logo](../assets/logo.png)
```

> [!NOTE]
> An image path is a URL, not a filename, so a space or other special character
> has to be percent-encoded — `![shot](assets/my%20image.png)` for a file named
> `my image.png`. This is the same rule GitHub and your editor's preview follow,
> and it is what they produce when they write a link for you.
> 
> The angle-bracket form `![shot](<assets/my image.png>)` is an equivalent
> spelling of the same image. A bare space (`![shot](assets/my image.png)`) is
> not a valid path, so it is not an image at all and stays on the page as
> literal text — again matching what GitHub and your preview show.
> 
> `markfluence read` and `markfluence export` write the encoded form, so a page
> round-trips back to Markdown that still renders.

**Run markfluence from the root of your documentation tree.** That root bounds
which images may be published: an image resolving outside it (`../../secrets/x.png`)
is reported as `IMAGE BROKEN: … (outside the documentation root)` rather than
uploaded.

Confluence attachment names cannot contain `/`, so the path is percent-encoded
into the attachment name — `assets/logo.png` is attached as `assets%2Flogo.png`,
and `../assets/logo.png` as `..%2Fassets%2Flogo.png`. The encoding is reversible,
so `markfluence read` restores an image's original path instead of a flattened
one. markfluence also records the source path in the attachment's comment, which
it prefers over decoding the name.

> [!NOTE]
> Pages published before this encoding existed used `/` → `_`. Republishing such
> a page uploads the image under its new name and updates the page to match, but
> the old attachment stays behind, unreferenced — markfluence never deletes.
> Remove those manually if the clutter bothers you.

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

As with image paths, a link destination is a URL: a sibling whose filename has a
space is written `[see](my%20doc.md)` (or `[see](<my doc.md>)`), and a bare
`[see](my doc.md)` is not a link at all. The same applies to the fragment, so a
non-ASCII heading anchor may arrive as `#caf%C3%A9-section`. Both are decoded
before markfluence matches them against files and headings on disk, so either
spelling resolves. A link it cannot resolve — a target with no `page_id`, or a
file that isn't there — is left exactly as written and published as-is, which on
Confluence is a dead relative link. There is no warning for this, so check the
targets when a link matters.

**Comment directives:**
- `<!-- confluence-toc -->` — replaced with Confluence table-of-contents macro.
- `<!-- markfluence-version -->` — replaced with the build stamp,
  `markfluence VERSION (SHA, DATE)` (the same string `markfluence --version`
  prints).

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
make completions        # write the shell completion scripts to ./completions
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
