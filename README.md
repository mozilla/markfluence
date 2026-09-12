# markfluence

Markdown-centric Confluence cli tool. Works with Claude, works with GitHub
actions, works with you.

## Location of documentation

This file covers installing, configuring, and running each command. The rest
lives beside it, because it is reference material rather than a read-through:

| | |
|---|---|
| [README.md](README.md) (this file) | installation, configuration, usage |
| [docs/markdown_file.md](docs/markdown_file.md) | the page format: every frontmatter field, and what the converter does with each body construct |
| [docs/github-actions.md](docs/github-actions.md) | running markfluence in CI: a working workflow, credentials, and why a service account |
| [docs/root-model.md](docs/root-model.md) | the documentation root: how a tree of files maps to a tree of pages |
| [CONTRIBUTING.md](CONTRIBUTING.md) | how to contribute: development setup, what to run before opening a pr, commit conventions, how to file an issue, etc |
| [docs/confluence/](docs/confluence/) | what we established about Confluence by experiment — the API, storage format, scopes, and the traps that produce confident wrong answers |
| [docs/guarantees.md](docs/guarantees.md) | the properties markfluence holds itself to, each with an honest status |
| [docs/json-output.md](docs/json-output.md) | `--json` in detail: status verbs, what counts as a result, why the shapes are what they are |
| [schema/json-output/v1.json](schema/json-output/v1.json) | the `--json` schema itself, also printed by `markfluence schema` |

## Which Confluence

Ways to run Confluence and markfluence support for it:

| Confluence | markfluence support |
|---|---|
| Cloud — Standard, Premium, Enterprise | **Supported.** |
| Cloud with a custom site domain | **Supported.** |
| Atlassian Government / isolated Cloud | **Untested.** Based on Atlassian documentation it has the same APIs and same identity model, so it is expected to work, but it's untested. |
| Data Center | **Unsupported.** |
| Server | **Unsupported.** Also end-of-life since February 2024. |

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
PowerShell. The release archives ship the bash/zsh/fish scripts under
`completions/`, and a Homebrew install puts them where each shell looks, so a
`brew install` needs nothing further; PowerShell isn't packaged and is
generated on demand instead (below).

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

markfluence reads a `.env` file automatically (no need to `source` it) from the
[documentation root](#the-documentation-root) — the directory holding
`markfluence.yaml`, found by walking up from the working directory, or the
working directory itself with no `markfluence.yaml` above it — or from an
explicit path via `--env-file PATH`. For `create`, `update`, and
`attachment-upload`, `--root PATH` redirects this too, the same as it does
the per-file root those commands otherwise resolve independently.

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

Then restrict it, since it holds your API token:

```
chmod 600 .env
```

markfluence warns when the `.env` it read is reachable by anyone but you *and*
contains `CONFLUENCE_TOKEN`. The warning names the file, what is wrong with its
mode, and the `chmod` that fixes it. When markfluence is run with `--json`, the
warning is not printed but carried in the output document's `warnings` array
(and on the stderr error object), because stderr in that mode is itself a JSON
document.

(Optional): `alias mf=markfluence`

### Scoped tokens and service accounts

For a normal personal API token, leave `CONFLUENCE_CLOUD_ID` unset.

For a **scoped** API token — the kind an Atlassian [service account][svcacct] gets,
for publishing from CI — you must set `CONFLUENCE_CLOUD_ID`. Scoped tokens are
rejected with a **401** against your site domain, so markfluence must use
Atlassian's `api.atlassian.com` gateway, and the cloud ID is required there.

To find your cloud ID (it is not a secret):

```console
$ curl -s https://your-org.atlassian.net/_edge/tenant_info
{"cloudId":"d8febd08-5555-5555-5555-db37c2369ce5"}
```

The scopes markfluence needs, copy-pasteable:

```
read:page:confluence
write:page:confluence
read:space:confluence
read:folder:confluence
search:confluence
read:confluence-user
write:confluence-file
readonly:content.attachment:confluence
read:confluence-content.summary
```

> [!NOTE]
> Scopes are fixed when a token is issued. A missing scope requires a **new**
> token to be created.

> [!NOTE]
> **The mixture of naming styles is correct, not a copy-paste error.** Atlassian
> has two scope vocabularies — *classic* (`read:confluence-user`) and *granular*
> (`read:page:confluence`) — granted independently, so holding one does **not**
> imply the other. markfluence talks to both API versions and each accepts only
> one vocabulary.

Diagnosing a failure:

| symptom | meaning |
|---|---|
| `401 Unauthorized; scope does not match` | a scope is **missing** — issue a new token |
| **403** | the token is scoped for the call, but the account lacks Confluence permission on that space or page — grant access; a new token will not help |

**[docs/confluence/api.md](docs/confluence/api.md#scopes) is the reference**: which
scope each API call needs and how that was established, why the list is mixed,
the three calls Atlassian no longer documents, and how to probe a token for the
scopes it actually holds (there is no introspection endpoint, but the scope gate
runs before routing, so one request per scope answers it).

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
markfluence find --help
markfluence search --help
markfluence children --help
markfluence export --help
```

Validating markdown locally (no network, no credentials):

```sh
markfluence check --help
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

All files are checked first — if any would fail (a problem with its `page_id`, a
title clash in the space, an unresolvable parent, or markdown the converter
refuses), nothing is created. Both kinds of clash name the page in the way, so
you can go look at it:

```console
$ markfluence create docs/runbook.md
  ✗ [docs/runbook.md] a page already exists at page_id 123 ("Deploy Runbook"): https://wiki.example.net/wiki/spaces/ENG/pages/123/Deploy+Runbook
  ✗ Aborting: 1 file(s) failed preflight; nothing was created.
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
real ids (see the `parent` field below). Creation is three-phase — every file is
validated (above), then a content-less stub is reserved for each, parents-first,
before any of them is converted — so a link from one file in the batch to another
resolves regardless of which direction it points, or whether the two link to each
other. A run interrupted after this point leaves a permanent, empty page version
behind rather than no page at all; every id is already persisted (unless
`--no-persist`), so a plain `update` finishes the job.

The preflight phase converts each file too, keeping only the answer to "can this
convert at all?" — so markdown the converter refuses (two images in one document
whose file names match, say) aborts the batch instead of leaving an empty page
and a `page_id` behind. A stub can still be left by a failure while publishing:
a server or network error, an attachment that turns out to be unreadable, or a
frontmatter file that can't be written. See S7 in
[docs/guarantees.md](docs/guarantees.md), which names all three.

`--dry-run` checks every file (the same checks a real run makes, so it exits
non-zero on the same failures) and previews what would be created — pages,
attachment uploads, page widths, and frontmatter write-backs — without writing to
Confluence or to any file. Because it makes the same checks, one unpublishable
file aborts the preview for the whole batch rather than previewing the rest; to
lint several files independently, use [`check`](#check) instead. Because nothing is created, a previewed page has no id
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
Labels work the same way: a `labels:` line is asserted exactly (anything on the
page that the file does not list is removed), and no `labels:` line means the
page's labels are left alone — not even read. `update` never writes back to the
file.

A `page_id` that no longer resolves fails that file with what to do about it
(`page_id 999 not found (deleted or wrong); correct it, or remove it and
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

Reconcile each file's frontmatter (`page_id`, `space`, `parent`, `page_width`,
`labels`, and a missing `title`) to match its live Confluence page. Labels are
reconciled even for a file with no `labels:` line, which is how you adopt a page
somebody labeled in the UI — the one place `fix` fills in a field `update` would
have left alone, because `fix` reconciles the file to the page rather than the
page to the file. The page is located by
`page_id`, or by searching for the `title` when `page_id` is absent. `fix` never
creates, updates, or moves pages — it's read-only on the server. It writes a file
when a field changed, and also when the frontmatter fields are out of canonical
order (`title`, `space`, `parent`, `page_id`, then the rest alphabetically),
which is reported separately as `reordered`. `--dry-run` reports both without
writing.

```sh
markfluence fix docs/*.md
markfluence fix docs/foo.md --dry-run
```

### `check`

```
Usage: markfluence check FILE... [flags]
```

Validate one or more Markdown files against the converter and frontmatter
rules — offline: no network access, no credentials, and no writes to
Confluence or to disk. This is the primitive a CI job, a pre-commit hook, or
an agent editing docs wants: validate every change instantly, with no risk of
publishing anything. Each file is processed independently; the command exits
non-zero if any file is broken or fails outright.

It reports the same `Broken`/`Warnings` a real `update`/`create` would
produce — a missing or escaping image/link, an unpublished sibling link, a
`#fragment` matching no heading — each prefixed with the source line it came
from, plus four frontmatter checks: an unparseable/unterminated frontmatter
block, an invalid `page_width`, an invalid `labels` entry, and a
present-but-non-numeric `page_id`.
Deliberately not checked: whether `page_id`/`space`/`parent` are set at all —
`check` can't know whether you're about to `create` or `update`, and a false
positive there would be worse than a miss. A **Broken** result fails
(`update`/`create` would publish literal `LINK BROKEN: …`/`IMAGE BROKEN: …`
text); a **Warning** alone does not — an unpublished sibling link is the
normal state of a tree that hasn't been created yet, not a defect.

`link not resolved: TARGET` — the most common warning — means `TARGET` is a
sibling `.md` file that exists under the documentation root but has no
`page_id` yet. A same-page anchor (`#heading`) hits this same warning when
the *current* file itself has no `page_id` yet, since it's internally
treated as a link to itself — which can read as though the file names
itself as missing; it doesn't, that's just this file before its first
publish (see [links to sibling `.md` files](docs/markdown_file.md)). Other message shapes:
[`IMAGE BROKEN`/`LINK BROKEN`](docs/markdown_file.md), and `anchor not found: TARGET` for a
`#fragment` that matches no heading.

```console
$ markfluence check docs/*.md
  ✗ [docs/broken-links.md] line 12: LINK BROKEN: typo-target.md (not found)
    [docs/guide.md] clean
  ✗ 1 of 2 file(s) failed.
```

`--show-html` additionally prints the converted storage HTML (indented by
nesting depth) and the attachment list, for debugging what a file would
actually publish without publishing it:

```sh
markfluence check docs/*.md
markfluence check --show-html docs/one-page.md
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
  `title`/`page_id`/`space`/`page_width`/`labels` frontmatter, i.e. a best-effort inverse of
  what `create`/`update` publish. The Confluence API has no markdown
  representation, so markfluence converts the storage body itself: constructs
  markfluence emits round-trip faithfully, while editor-authored content degrades
  gracefully — any macro markfluence doesn't map (panels, expand, status, …) and
  column layouts pass through as raw storage tags, with a macro/cell body kept as
  readable markdown, so they round-trip back through `create`/`update`. A page or
  space link converts back to a markdown link, and so does a **mention** (see
  below); an attachment link and a blog-post link stay as raw storage, since a
  markdown link would republish to something else or nothing at all. Some other transforms are lossy (e.g. a table
  cell background color outside the named swatches comes back as a literal hex),
  so this is a reading aid, not a guaranteed source round-trip.
- `storage` — the page's raw storage-format XHTML, exactly as stored.

```sh
markfluence read 1234567890                       # markdown, with frontmatter
markfluence read 1234567890 > page.md
markfluence read 1234567890 --format storage > page.storage.xml
markfluence read "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title"
```

### `children`

```
Usage: markfluence children [PAGE] [flags]
```

List the pages and folders under a page or folder, or under a whole space with
`--space KEY`. `markfluence children --help` explains why folders get their own
rows and why a folder counts as a level.

```sh
markfluence children 1234567890                  # direct children
markfluence children 1234567890 --depth 3
markfluence children 1234567890 --depth all
markfluence children "https://org.atlassian.net/wiki/spaces/ENG/folder/1234567890"
markfluence children docs/index.md               # children of the page index.md publishes to
markfluence children 1234567890 --json | jq -r '.results[] | select(.type=="page") | .id'
markfluence children --space ENG                 # the space's top level
markfluence children --space ENG --depth all     # every page and folder in the space
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

`--depth` takes a positive number or `all`. `0` is rejected rather than treated
as "unlimited", since silently walking an entire space for someone who meant
"none" is worse than an error.

Trashed pages and folders are not listed. Finding nothing is a success: the
command prints `No children.` and exits 0, so an empty `results` array is how a
script tests for an empty subtree.

With `--space`, human output adds a `--depth` reminder when `--depth` was left at
its default — on stderr, so the table still pipes:

```
$ markfluence children --space AIM
TYPE  ID       TITLE
page  2097154  Africa Innovation Mradi Home
page  2097185  What is Africa Mradi?

    Showing the space's top level. Use --depth 2, or --depth all for the whole tree.
```

A space's top level is its root **pages**, which is not the same as the
homepage's children: more than one root page is normal (a page published with
`parent: null` is one), and a folder created with no parent lands under the
homepage rather than at the root, so there is no root-level folder to miss
([docs/confluence/spaces.md](docs/confluence/spaces.md)). Such a row reports
`"parent_id": null` in `--json`, hanging off no node. An unknown space key is a
usage error (exit 2), not an empty result, as it is for `find` and `search`.

### `find`

```
Usage: markfluence find TITLE [flags]
```

Find the pages and folders whose title is `TITLE`. This is the one handle the
other commands cannot resolve: they take a page id, a page URL, or a Markdown
file with a `page_id`, and `find` is how you get from a title to one of those.
If you do not know the title, use [`search`](#search) instead.

```sh
markfluence find "Deploy runbook"
markfluence find "Deploy runbook" --space ENG
markfluence find "Deploy runbook" --json | jq -r '.results[] | select(.type=="page") | .id'
```

```
TYPE    ID          SPACE          STATUS    TITLE           URL
page    3277005     AVSE           archived  Deploy runbook  https://org.atlassian.net/wiki/spaces/AVSE/pages/3277005/Deploy+runbook
page    5144768     CEX            current   Deploy runbook  https://org.atlassian.net/wiki/spaces/CEX/pages/5144768/Deploy+runbook
folder  2950660103  CLOUDSERVICES  current   Deploy runbook  https://org.atlassian.net/wiki/spaces/CLOUDSERVICES/folder/2950660103
```

The match is **exact and case-insensitive** — not a substring search. Results are
ordered by space, then type, then id. `--space` takes a space **key**, never a
numeric space id, the same rule frontmatter follows; an unknown key is an error
rather than an empty result, since a typo would otherwise be indistinguishable
from "no such page".

**Archived pages are included, and marked.** An archived page does not appear in
the page tree, but it still holds its title — `create` in that space will be
refused until it is restored or renamed. That is the main reason to run `find`
before publishing, and why the `STATUS` column is not decoration.

**Folders are included too, but they never explain a conflict.** A folder id is
a legitimate `parent`, so being able to look one up by name is useful. A folder
does not reserve a title: a page can be created with a folder's exact name in
the same space. So a folder row is somewhere to publish, never a reason you
cannot.

Trashed pages are not listed. Finding nothing is a success: the command prints
`No matches found.` and exits 0, so `--json` reporting an empty `results` array
is how a script tests "does this title exist yet?" before deciding to create or
update.

Answering takes two requests, because no single Confluence API can see all
three: the v2 pages route covers current and archived pages but cannot see a
folder, while CQL covers folders but cannot see an archived page. If either
request fails the whole command fails — half an answer here reads as "nothing
found", which is the one wrong answer that causes a duplicate. The details are
in [docs/confluence/search.md](docs/confluence/search.md).

### `search`

```
Usage: markfluence search QUERY [flags]
```

Find pages by **full text**, for when you do not know the title. `find` answers
"does a page called *this* exist?"; `search` answers "where is the page about
deploys?". `markfluence search --help` covers how the query is matched and what
the index cannot see.

```sh
markfluence search "deploy runbook"
markfluence search "deploy runbook" --space ENG --limit 25
markfluence search deploy --limit all --json | jq -r '.results[].id'
markfluence search 'type = page and label = "runbook"' --cql
```

A label search only finds pages someone labeled. Since markfluence publishes
labels from frontmatter (`labels:` above), a tree it manages is searchable this
way without anyone tagging pages in the UI.

```
Deployment runbook
  page 2064154670  PXI
  https://org.atlassian.net/wiki/spaces/PXI/pages/2064154670/Deployment+runbook
  Keep an eye on the deploy-packages job...to be deployed)

Runbook: Prod deployment
  page 1293156436  CLOUDSERVICES
  https://org.atlassian.net/wiki/spaces/CLOUDSERVICES/pages/1293156436/Runbook+Prod+deployment
  "Deploy failed" message in #crash-ingestion-bots...look at logs in Github Actions

    Showing 2 matches; more exist (use --limit all).
```

Each hit is a block rather than a table row, because the excerpt is what tells
you *why* it matched, and an excerpt is too long for a column.

**Matched terms in the excerpt are shown in reverse video**, using the positions
Confluence reports rather than by matching your query text — so the highlight
follows the server's own stemming (searching `deploy` marks `deploys`) and works
under `--cql`, where there are no query words to match against. Some hits come
back without them; Confluence marked 40 of 50 sampled rows. The highlight
disappears under `--no-color`, under `NO_COLOR`, and whenever output is piped,
so a captured excerpt is plain text. `--json`'s `excerpt` is unaffected.

**markfluence never re-sorts results.** The API reports a relevance score of
`0.0` on every row, so the order it returns is the only ranking that exists —
which is also why a `--json` consumer should not sort `results`.

`--limit` takes a positive number or `all`, defaulting to `10` — a hit is 5–6
lines, so ten is about a screen. `0` is refused rather than read as "unlimited",
as with `children --depth`. When more matches exist the command reports *that*,
not how many, because the API's own total is an estimate that disagrees with
what it returns.

`--type` defaults to `page` and also accepts `blogpost` or `all`. The index
holds attachments, comments, databases and whiteboards, all of which match text,
but their ids are not something any other command accepts — hence `all` rather
than the default. `--type folder` is refused with a pointer to `find`.

`--cql` passes QUERY through as
[CQL](https://developer.atlassian.com/cloud/confluence/advanced-searching-using-cql/)
with no escaping and no clauses added. It cannot combine with `--space` or
`--type`: those would have to be ANDed onto your query, which regroups a query
containing `or` and silently answers something else. `--limit` still applies,
bounding paging rather than the query.

The index also lags by up to about a minute, so a page created moments ago may
not be there yet — anything that must be correct *now* should use `find`.

The evidence behind the query it builds — including why it uses `siteSearch` and
not the `text` field Atlassian documents — is in
[docs/confluence/search.md](docs/confluence/search.md).

### `export`

```
Usage: markfluence export PAGE [flags]
```

Write a page and the attachments it uses to a directory — the one-command form
of `read` plus `attachment-download`. `markfluence export --help` covers what
`--depth` and `--space` walk, and what the frontmatter it writes is for.

```console
$ markfluence export 1234567890 --dest ./out
wrote      out/markfluence-test-page.md
downloaded out/assets/diagram.png
           (skipped 2 unreferenced attachment(s); --all-attachments to include)
```

Attachments are written to the paths their images were published from, so the
exported tree matches the layout of the repo the page came from and previews
locally in GitHub or VSCode.

```console
$ markfluence export 1234567890 --depth all --dest out
wrote      out/markfluence.yaml
wrote      out/handbook.md
wrote      out/handbook/onboarding.md
downloaded out/handbook/onboarding/diagram.png
2 pages (2 exported, 0 skipped, 0 failed)
```

A page becomes `<slug>.md` with a `<slug>/` beside it holding its children and
its own Confluence-native attachments; a folder becomes a directory. Each
child's `parent:` points at its parent's file (`parent: ../handbook.md`), so the
tree publishes into fresh pages rather than only back into the ids it came from.
`--depth` takes `0` (the default, the page alone), a positive number, or `all`.

`markfluence.yaml` is written at `--dest` for a multi-page export, marking it as
a project root. Without it each exported file's root would be its own directory,
a shared asset above a page would resolve outside it, and the tree would not
publish back. An existing one is left alone.

A page whose file already exists is skipped, so a re-run resumes rather than
re-fetching; `--force` re-exports everything, which is also how you refresh a
tree whose pages changed upstream. `--file` names the page file, defaulting to a
slug of the title, or the page id when the title slugs to nothing. `--dest`
defaults to the current directory and is created if missing. `--dry-run`
previews without writing. `--skip-attachments` writes the page file only.

There is deliberately no `--attachments-dir`. It is no longer *unsafe* — an
attachment is named by its base name, so moving `assets/x.png` to
`attachments/x.png` keeps the name `x.png` and orphans nothing — but collecting
everything into one directory reintroduces exactly the collision the base name
already has to refuse: two pages' `diagram.png` cannot share a directory.

Referenced attachments include images, attachment links, and references inside
macros markfluence passes through untouched. If the page references an
attachment that is not attached — already broken in Confluence — the export
still succeeds and reports it as a warning.

Markdown is the only output format. Use `read --format storage` to inspect the
raw storage Confluence holds.

### `attachment-list`

```
Usage: markfluence attachment-list PAGE [flags]
```

List a page's attachments.

```console
$ markfluence attachment-list 1234567890
NAME             SIZE  VER  TYPE             SOURCE
diagram.png   24.1 KB    3  image/png        assets/diagram.png
notes.pdf      1.2 MB    1  application/pdf  -
```

`NAME` is the name Confluence stores — for an image markfluence published, the
file's base name (see [docs/markdown_file.md](docs/markdown_file.md)) — and `SOURCE` is the Markdown image path
it came from, recorded in the attachment's comment. The table shows at a glance
which attachments a publish manages and which it will leave alone.

`SOURCE` is a dash when no source path is recorded: either the attachment was
uploaded by hand, or it was published before markfluence recorded source paths.
Those two look the same here; `--json` has a `managed` field that tells them
apart. Attachments left behind by a naming change show up this way, which is how
you find them — including the percent-encoded names markfluence wrote before it
started naming attachments by their base name.

### `attachment-upload`

```
Usage: markfluence attachment-upload PAGE FILE... [flags]
```

Upload or replace attachments on a page, complementing the automatic sync that
`create` and `update` perform for a page's images.

Each file is attached under its path relative to the documentation root (its
base name, with no `markfluence.yaml` above it). A file whose contents already match
what's on the page is skipped, using the same checksum bookkeeping
`create`/`update` use, so uploading by hand and publishing agree on what's
current. `--force` uploads anyway (bumping the attachment's version), which is
how you repair an attachment whose stored bytes drifted while its checksum still
matches. `--dry-run` previews without writing.

`--name` takes a **path**, not a name, for a single file — so `--name
assets/x.png` produces the attachment that an image written as
`![](assets/x.png)` resolves to: stored as `x.png`, with `assets/x.png` recorded
as its source. The stored name is always the base name of the recorded path, so
a later publish won't create a duplicate under a different one.

Uploading several files at once refuses a collision the same way publishing
does: `arch/diagram.png` and `deploy/diagram.png` in one command both want the
attachment `diagram.png`, so neither is uploaded.

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

An attachment with no recorded path — one that originated in Confluence, or was
published before markfluence recorded them — is written under a directory named
after the page, since an attachment name is unique per page and not per space:
two pages' `diagram.png` would otherwise be one file. That is where `read` and
`export` point at it too. `--flat` writes everything directly under `--dest`,
under stored names. `--dest` defaults to the current directory and is
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
  "roots": ["/repo/docs"],
  "warnings": [],
  "results": [
    {
      "ok": true,
      "status": "published",
      "dry_run": false,
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

**[docs/json-output.md](docs/json-output.md)** covers the rest: the per-command
status verbs, what counts as one result for each command, `check`'s `broken`
status, `create`'s preflight abort, and why `find`/`search` report an
operational failure on stderr rather than as a result.

Exit codes:

| exit | meaning |
|---|---|
| `0` | success — including "no matches", which is an answer a caller acts on |
| `1` | a per-file or per-target failure; the envelope is still on stdout, with `ok: false` and an `error`/`code` on the failed results |
| `2` | a fatal pre-flight failure (bad flags, credential resolution). No envelope; a typed error object goes to **stderr** instead |

```json
{ "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG", "warnings": [] }
```

Error `code` values: `CONFIG`, `AUTH`, `NOT_FOUND`, `VALIDATION`, `CONVERT`,
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
info read update create fix children find search attachment-list attachment-upload attachment-download export

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

## Markdown page structure

Each Markdown file is one Confluence page: an optional YAML **frontmatter**
block followed by the Markdown **body**.

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

**[docs/markdown_file.md](docs/markdown_file.md) is the page-format reference**:
every frontmatter field and what each verb does with it, and every body
construct — tables and their cell conventions, GitHub alerts, images and how
their paths resolve, links between pages, mentions, and pasted Confluence
storage markup.

## The documentation root

**Do you need a `markfluence.yaml`?**

- If you export, edit, and publish files **one at a time**, no. Each file's
  root defaults to its own directory, and that's already the directory you
  want.
- If you're working on a **directory tree** of files — pages that link to
  each other, or that share an `assets/` directory — put a
  `markfluence.yaml` at the root of that tree. Without one, each file's root
  still defaults to its own directory, which means a page can't reach an
  image or another page sitting *above* itself; a shared-assets layout like
  the one in [docs/markdown_file.md](docs/markdown_file.md) needs a declared root to work at all.

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
```

The rest of this section is the precise version of the same idea. Every
markdown file has a **documentation root**: the directory holding
`markfluence.yaml`, found by walking up from the file's own directory, or —
with no `markfluence.yaml` anywhere above it — the file's own directory. It
bounds which images and `parent:` references a file may read, and it's what
an image's recorded attachment name and source are relative to. The root
actually used is reported once per distinct value in a run. `--root PATH`
overrides discovery for the whole invocation — and, for `create`, `update`,
and `attachment-upload`, also redirects where `.env` is read from (see
[Configure](#configure)).

For the reasoning behind this model — why a bare marker file, what it fixes,
what it costs — see [docs/root-model.md](docs/root-model.md) and
[_plans/025_file-organization.md](_plans/025_file-organization.md).

## Common tasks

**Moving or renaming a markdown file.** Just move it. Links to it resolve by
where it actually is, via the root-relative link index — nothing elsewhere
needs editing, and nothing needs republishing except the moved file itself
(to pick up its own new links, if any changed).

**Moving a page's own images along with it.** This churns: an attachment's
identity is relative to the *root*, not the page, so moving both together
changes the images' root-relative paths, and the next publish uploads them
under new names, leaving the originals behind unreferenced (markfluence never
deletes; [#99](https://github.com/mozilla/markfluence/issues/99) tracks a
future `attachment-prune`). Moving just the page and leaving its images in a
shared directory is the free move instead.

**Renaming or moving a shared asset**, independent of any page, churns the
same way: every page referencing it records a new attachment name on its next
publish. Identity follows the asset's location, not any particular page's
(this is L3 in [docs/guarantees.md](docs/guarantees.md) — `identity-from-asset-location`).

**Setting up a shared assets directory across many pages** needs a
`markfluence.yaml` at the directory that should be the shared root. Without
one, each page's root defaults to its own directory, and an asset above any
one of them is `IMAGE BROKEN` — the layout in [docs/markdown_file.md](docs/markdown_file.md) needs
this to work at all.

## Inspirations

[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli) -- command
line interface. markfluence tries to match subcommands and arguments from
confluence-cli, but focuses on Markdown document publishing and less on
providing a CLI access to the full Confluence v1/v2 API.

[kovetskiy/mark](https://github.com/kovetskiy/mark) -- Markdown support for
Confluence and how things are represented. markfluence tries to match key
design decisions, but has defaults I like better and works in different
scenarios better.
