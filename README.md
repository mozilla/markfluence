# markfluence

Markdown-centric Confluence cli tool. Works with Claude, works with GitHub
actions, works with you.

## Location of documentation

This file covers installing, configuring, and running each command. The rest
lives beside it, because it is reference material rather than a read-through:

| | |
|---|---|
| [README.md](README.md) (this file) | installation, configuration, usage |
| [docs/commands/](docs/commands/) | every command's `--help`, rendered — the same text `markfluence CMD --help` prints, generated from the binary |
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

Each command explains itself: **`markfluence COMMAND --help`** is the reference
for what it does, why, and how to invoke it. What follows is which command to
reach for.

**Publishing markdown to Confluence:**

| | |
|---|---|
| [`create`](docs/commands/markfluence_create.md) | make new pages from files that have no `page_id` yet. Checks every file first, and creates nothing if any would fail |
| [`update`](docs/commands/markfluence_update.md) | republish files that already have a `page_id`. Skips a file that has not changed |
| [`check`](docs/commands/markfluence_check.md) | validate files with no network and no credentials — dead links, broken images, bad frontmatter |
| [`fix`](docs/commands/markfluence_fix.md) | reconcile a file's frontmatter *from* its live page, when the two have drifted |

**Getting things out of Confluence:**

| | |
|---|---|
| [`read`](docs/commands/markfluence_read.md) | one page as markdown on stdout, or as raw storage |
| [`export`](docs/commands/markfluence_export.md) | a page, a subtree, or a whole space to files, attachments included |
| [`info`](docs/commands/markfluence_info.md) | one page's metadata: space, parent, version, width, labels, authors |

**Finding pages:**

| | |
|---|---|
| [`find`](docs/commands/markfluence_find.md) | resolve an exact title to ids. Sees archived pages and folders, which `search` cannot |
| [`search`](docs/commands/markfluence_search.md) | full text, for when you do not know the title. Takes raw CQL with `--cql` |
| [`children`](docs/commands/markfluence_children.md) | list what is under a page, a folder, or a space |

**Attachments** — `create`/`update` handle a page's images for you; these are for
everything else:

| | |
|---|---|
| [`attachment-list`](docs/commands/markfluence_attachment-list.md) | what is attached to a page |
| [`attachment-upload`](docs/commands/markfluence_attachment-upload.md) | attach a file, skipping one whose checksum already matches |
| [`attachment-download`](docs/commands/markfluence_attachment-download.md) | fetch attachments back to the paths they were published from |

And [`schema`](docs/commands/markfluence_schema.md) prints the `--json` schema.

Every command takes `--json`; `create`, `update` and `fix` take `--dry-run`.

### Common workflows

Edit a page that already exists:

```sh
# what is its page id?
markfluence find --space SRE "Deploy runbook"
# download it and all attachments
markfluence export 1234567890

# ...edit the file it wrote...

# is this valid markdown?
markfluence check deploy-runbook.md
# publish changes to Confluence
markfluence update deploy-runbook.md
```

Create a new page:

```sh
vi deploy_runbook.md

# ...create the file...

# verify markdown is correct
markfluence check deploy-runbook.md
# create the page in the ENG space at the top level
markfluence create --space ENG deploy-runbook.md

# ...make some edits...

# verify markdown is correct
markfluence check deploy-runbook.md
# publish edits
markfluence update deploy-runbook.md
```

Export an entire tree of pages and edit them:

```sh
# export an entire tree of pages and referenced attachments
markfluence export --depth all --dest docs 1234567890

# ...make edits...

# verify markdown is correct
markfluence check docs/*.md docs/**/*.md
# update any pages that changed
markfluence update docs/*.md docs/**/*.md
```

Pick up changes somebody made in Confluence. This is the one command that
writes *to* your files *from* Confluence — every other one goes the other way:

```sh
# what disagrees? nothing is written
markfluence fix docs/*.md --dry-run

# reconcile page_id, space, parent, page_width, labels and a missing title
markfluence fix docs/*.md
```

`fix` never creates, updates or moves pages — it is read-only on the server.
Note the asymmetry it settles: `update` leaves a field alone when your file does
not mention it, while `fix` fills that field in from the page. It is how you
adopt a page somebody labeled in the UI, or one you published by hand and want
a file for.

One thing it does *not* do: a `title` you have already set is left alone, so a
page renamed in Confluence does not rename your frontmatter. Only a missing or
blank `title` is filled in.

### What the output looks like

Three commands whose shape is worth seeing before you run them. `children`
indents by depth, keeping `TYPE` and `ID` aligned so the output stays greppable:

```
TYPE    ID          TITLE
folder  2876047392  Articles
page    1675427879  MozCloud planning
page    1671692338    MozCloud observability focus and issues
```

`find` reports current pages, archived pages and folders together, because all
three can hold the title you asked about but only one is a page you can publish
to:

```
TYPE    ID          SPACE          STATUS    TITLE           URL
page    1675427879  ENG            current   Deploy runbook  https://…
page    1293156436  CLOUDSERVICES  archived  Deploy runbook  https://…
```

`search` prints a block per hit rather than a row, because the excerpt is what
tells you *why* it matched — with the terms Confluence marked shown in reverse
video:

```
Deployment runbook
  page 2064154670  PXI
  https://org.atlassian.net/wiki/spaces/PXI/pages/2064154670/Deployment+runbook
  Keep an eye on the deploy-packages job...to be deployed)

    Showing 2 matches; more exist (use --limit all).
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

It can also carry **project-wide defaults**, which is what saves a hundred
files from each repeating `space: ENG`:

```yaml
space: ENG
page_width: max
```

Each is a default a file overrides: the chain is **flag > frontmatter >
project file**, so the answer closest to the content wins. A key markfluence
does not recognise is an error rather than something ignored — a typo in a
project-wide default is wrong for every file at once. Credentials are
deliberately not settings here; see
[docs/root-model.md](docs/root-model.md#what-it-deliberately-does-not-hold).

It can also hold a **`pages:` block**, giving each file its page metadata so the
markdown itself stays pristine — no frontmatter at all:

```yaml
pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
```

That is what makes `markfluence update docs/**/*.md` work from CI with no
per-file inputs, and it is why `update` has no `--page-id`/`--title` flags:
those would each have to name one file. Both locations are legal and agreement
is silent, so you can move metadata in a file at a time; a file neither place
mentions is skipped rather than failed. Details:
[docs/root-model.md](docs/root-model.md#pages--page-metadata-for-a-pristine-file).

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

For the reasoning behind this model — what it fixes, what it costs, and every
project-wide setting — see [docs/root-model.md](docs/root-model.md) and
[_plans/025_file-organization.md](_plans/025_file-organization.md).

### Moving files and assets

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

## Inspirations

[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli) -- command
line interface. markfluence tries to match subcommands and arguments from
confluence-cli, but focuses on Markdown document publishing and less on
providing a CLI access to the full Confluence v1/v2 API.

[kovetskiy/mark](https://github.com/kovetskiy/mark) -- Markdown support for
Confluence and how things are represented. markfluence tries to match key
design decisions, but has defaults I like better and works in different
scenarios better.
