# markfluence

A command line tool for Markdown and Confluence: works with agents, works with
GitHub Actions, works with you.

## Features

- **Manage Confluence content outside of Confluence.** markfluence works with
  one file, or with a project of many files. It supports page trees, images,
  attachments, and labels.
- **GitHub-Flavored Markdown with Confluence additions.** markfluence supports
  base Markdown and more: tables, callouts, table cell background color, the
  table-of-contents macro, links, anchors, and Confluence user mentions. The
  files stay correct in GitHub and in Markdown preview programs.
- **Content round trip.** `export` downloads a Confluence page, a page tree, or
  a whole space to your machine. Those files publish back to Confluence. You
  can also write new files on your machine and publish them. The result is
  semantically equivalent, but not byte-for-byte the same. The content survives
  the round trip.
- **Batch publish.** Publish one page, or a whole tree at the same time. A page
  with no changes gets no new version: markfluence does not send its body
  again.
- **Confluence storage format.** You can write Confluence-specific markup
  with no Markdown equivalent.
- **Offline validation of Markdown files.** markfluence finds dead links,
  broken images, and bad frontmatter with no network and no credentials.
- **Confluence search.** `find` resolves an exact title to page ids and folder ids, and it
  sees archived pages and folders. `search` does a full-text search and shows
  an excerpt for each hit. It also takes raw Confluence Query Language (CQL).
  `user-find` resolves a person's name to the account id that a mention needs.
  It also prints the Markdown line that mentions them.
- **Diff.** `diff` shows what is different between your file and the page in
  Confluence. The output works with `patch -R`. The exit codes are the same as
  the exit codes of `diff`.
- **CI publish workflows.** Keep your documents in a repository and publish
  them to Confluence when a change merges. Use
  [markfluence-action](https://github.com/mozilla/markfluence-action). It is
  one step, and it publishes the files that changed and no other files.
- **Care with the edits of other persons.** markfluence keeps a local event
  log. It refuses to overwrite a page that somebody else changed after you made
  your copy. It skips a publish when the body did not change. It does not
  delete attachments. The `--force` flag is the escape hatch.
- **Personal access tokens and scoped service account tokens.** markfluence
  supports both token types correctly.
- **`--dry-run` on the publish and export commands.** You can see what will
  happen before it happens.
- **`--json`, correct exit codes, shell completions, terminal detection, and
  correct use of stdout and stderr.** Every command can write JSON for scripts
  and for AI agents. markfluence generates its own shell completions. It colors
  its output, but not when the output is not a terminal. It sends each message
  to stdout or to stderr as a shell script needs.

## Location of documentation

This file tells you how to install markfluence, how to configure it, and where
to find additional documentation.

| | |
|---|---|
| [README.md](README.md), this file | installation, configuration, and use |
| [docs/commands/](docs/commands/) | the `--help` text of every command, as Markdown. It is the same text that `markfluence CMD --help` prints, and it comes from the binary |
| [docs/markdown_file.md](docs/markdown_file.md) | the page format: every frontmatter field, and what the converter does with each body construct |
| [docs/root-model.md](docs/root-model.md) | the documentation root: how a tree of files maps to a tree of pages |
| [CONTRIBUTING.md](CONTRIBUTING.md) | how to contribute: the development setup, what to run before you open a pull request, the commit conventions, and how to file an issue |
| [docs/confluence/](docs/confluence/) | what we found out about Confluence by experiment: the API, the storage format, the scopes, and the traps that give you a confident wrong answer |
| [docs/guarantees.md](docs/guarantees.md) | the properties that markfluence holds itself to, each one with an honest status |
| [docs/json-output.md](docs/json-output.md) | `--json` in detail: the status verbs, what counts as a result, and why the shapes are what they are |
| [schema/json-output/v1.json](schema/json-output/v1.json) | the `--json` schema. The `markfluence schema` command also prints it |

## Which Confluence

These are the ways to run Confluence, and the support that markfluence gives to
each one:

| Confluence | markfluence support |
|---|---|
| Cloud: Standard, Premium, Enterprise | **Supported.** |
| Cloud with a custom site domain | **Supported.** |
| Atlassian Government Cloud, or isolated Cloud | **Not tested.** The Atlassian documents show the same APIs and the same identity model. We think it works, but we did not test it. |
| Data Center | **Not supported.** |
| Server | **Not supported.** Its end of life was in February 2024. |

## Install

### macOS with Homebrew

This repository is its own [tap](https://docs.brew.sh/Taps), so the tap needs
an explicit URL. The repository is not named `homebrew-markfluence`, and that
is the name that `brew tap` looks for.

```sh
brew tap mozilla/markfluence https://github.com/mozilla/markfluence
brew trust mozilla/markfluence
brew install markfluence
```

To upgrade, use `brew update && brew upgrade markfluence`. Homebrew installs
the shell completions where each shell looks for them, so you have no more
work to do.

markfluence has a build for Apple Silicon, and it has no build for Intel. macOS
26 Tahoe is the last release that Apple ships for Intel Macs, because macOS 27
needs Apple Silicon. Thus `brew install` finds no build on an Intel Mac. On an
Intel Mac, build markfluence from source. See below.

### Linux with a release archive

Get the archive for your architecture from the
[latest release](https://github.com/mozilla/markfluence/releases/latest). Make
sure that its checksum is correct, then put the binary on your `PATH`. Change these commands
as necessary for your machine:

```sh
VERSION=0.1.0                                   # no leading "v"
ARCH=amd64                                      # or: arm64
BASE="https://github.com/mozilla/markfluence/releases/download/v${VERSION}"
ARCHIVE="markfluence_${VERSION}_linux_${ARCH}.tar.gz"

curl -fsSLO "${BASE}/${ARCHIVE}" && \
    curl -fsSLO "${BASE}/checksums.txt" && \
    sha256sum --check --ignore-missing checksums.txt && \
    tar -xzf "${ARCHIVE}" markfluence && \
    install -D -m 0755 markfluence ~/.local/bin/markfluence
```

These commands tell `tar` to extract the `markfluence` file and no other file.
The archive is flat, and it also holds a `README.md` file and a `LICENSE` file.
A `tar -xzf` command with no file name writes over your own copies of those two
files.

### Any platform, from source

You need Go 1.25 or a later version.

```sh
go install github.com/mozilla/markfluence@latest
```

A binary from this command shows its version as `dev`. The release build
applies the version stamp with linker flags, and `go install` does not use
those flags. If you think that you will report a bug for a specific version,
use a release archive instead.

You can also build from a clone. This is also the development setup:

```sh
git clone https://github.com/mozilla/markfluence
cd markfluence
make install          # installs `markfluence` into your Go bin directory
# ...or build into ./bin and do not install:
make build            # makes ./bin/markfluence
```

### Shell completions

markfluence generates its own completion scripts for bash, zsh, fish, and
PowerShell. The release archives hold the bash, zsh, and fish scripts in the
`completions/` directory. A Homebrew install puts them where each shell looks
for them, so a `brew install` needs no more work. The archives do not hold the
PowerShell script. markfluence generates that script when you ask for it. See
below.

To load the completions into the current shell:

```sh
source <(markfluence completion bash)   # bash
source <(markfluence completion zsh)    # zsh
markfluence completion fish | source    # fish
```

To install them permanently, run `markfluence completion <shell> --help`. It
prints the path that your shell reads on your platform. This is an example for
Linux with bash:

```sh
markfluence completion bash > /etc/bash_completion.d/markfluence
```

Completion gives you Markdown file names for a `FILE` argument or a `PAGE`
argument. You type the page id form and the URL form of `PAGE` yourself.
Completion also gives you the values of flags such as `--page-width` and
`--format`, and directory names for `--dest`. Completion does not give you
attachment names. Attachment names are on the server, and completion never
makes a network request.

## Configure

markfluence needs a Confluence site URL, a username, and an API token. It
resolves each one in this sequence: **the flag first, then the environment
variable, then the `.env` file**.

| Setting | Flag | Environment variable or `.env` |
| --- | --- | --- |
| Site URL | `--url` | `CONFLUENCE_URL` |
| Username | `--username` | `CONFLUENCE_USERNAME` |
| API token | *none. It is never a flag* | `CONFLUENCE_TOKEN` |
| Cloud ID, optional | `--cloud-id` | `CONFLUENCE_CLOUD_ID` |

markfluence reads a `.env` file without help, so you do not need to `source`
it. It reads the file from the [documentation root](#the-documentation-root).
The documentation root is the directory that holds `markfluence.yaml`, and
markfluence finds it when it goes up from the working directory. If no
`markfluence.yaml` file is above the working directory, the root is the working
directory itself. You can also give an explicit path with `--env-file PATH`.
The `--root PATH` flag moves this path for the `create`, `update`, `diff`,
and `attachment-upload` commands. For those commands, and for `check`, `--root`
also moves the per-file root that they find by themselves.

Copy `.env.example` to `.env` and fill it in:

```
CONFLUENCE_URL=https://your-org.atlassian.net
CONFLUENCE_USERNAME=you@example.com
CONFLUENCE_TOKEN=your-api-token
# Optional. Set this only for a *scoped* API token.
# CONFLUENCE_CLOUD_ID=
```

> [!NOTE]
> markfluence does not accept the API token as a command line flag. The token
> comes from the environment or from the `.env` file.

Then restrict the file, because it holds your API token:

```
chmod 600 .env
```

markfluence gives a warning when two conditions are both true. The first
condition is that a person who is not you can read the `.env` file. The second
condition is that the file holds `CONFLUENCE_TOKEN`. The warning gives the name
of the file, the fault in its mode, and the `chmod` command that corrects
it. If you run markfluence with
`--json`, markfluence does not print the warning. It puts the warning in the
`warnings` array of the output document, and in the error object on stderr.
stderr is a JSON document in that mode.

Optional: `alias mf=markfluence`

### Scoped tokens and service accounts

For a normal personal API token, do not set `CONFLUENCE_CLOUD_ID`.

You must set `CONFLUENCE_CLOUD_ID` for a **scoped** API token. An Atlassian
[service account][svcacct] gets a scoped token, and you use it to publish from
CI. Atlassian refuses a scoped token with a **401** status against your site
domain. Thus markfluence must use the `api.atlassian.com` gateway of Atlassian,
and that gateway needs the cloud ID.

To find your cloud ID, use this command. The cloud ID is not a secret:

```console
$ curl -s https://your-org.atlassian.net/_edge/tenant_info
{"cloudId":"d8febd08-5555-5555-5555-db37c2369ce5"}
```

markfluence needs these scopes. You can copy this list:

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
write:confluence-content
read:content-details:confluence
```

> [!NOTE]
> Atlassian fixes the scopes when it issues a token. If a scope is absent, you
> must create a new token.

> [!NOTE]
> **The mixture of two name styles is correct. It is not a copy-paste error.**
> Atlassian has two scope vocabularies: the *classic* vocabulary, such as
> `read:confluence-user`, and the *granular* vocabulary, such as
> `read:page:confluence`. Atlassian grants them independently, so a token with
> one vocabulary does **not** hold the other. markfluence uses both API
> versions, and each version accepts one vocabulary only.

Use this table to diagnose a failure:

| Symptom | Meaning |
|---|---|
| `401 Unauthorized; scope does not match` | A scope is **absent**. Issue a new token |
| **403** | The token has the correct scope for the request, but the account does not have Confluence permission for that space or that page. Grant the access. A new token does not help |

**[docs/confluence/api.md](docs/confluence/api.md#scopes) is the reference.** It
gives these details:

- The scope that each API request needs, and how we found that out.
- Why the list mixes the two vocabularies.
- Which 3 requests Atlassian no longer documents.
- How to find the scopes that a token holds.

Atlassian has no introspection endpoint. But the scope gate runs before
Confluence routes the request, so one request for each scope gives you the
answer.

[svcacct]: https://support.atlassian.com/user-management/docs/understand-service-accounts/

## Usage

Each command explains itself. **`markfluence COMMAND --help`** is the reference
for what the command does, why it does it, and how you run it. This section
tells you which command to use.

**To publish Markdown to Confluence:**

| | |
|---|---|
| [`create`](docs/commands/markfluence_create.md) | Make new pages from files that have no `page_id` yet. It does a check of every file first, and it creates no page if one file would fail |
| [`update`](docs/commands/markfluence_update.md) | Publish files that already have a `page_id` again. It skips a file that did not change |
| [`check`](docs/commands/markfluence_check.md) | Do a check of files with no network and no credentials: dead links, broken images, and bad frontmatter |
| [`diff`](docs/commands/markfluence_diff.md) | Show what is different between one file and its page. stdout is a patch. It exits with `1` when the two are different |

**To get things out of Confluence:**

| | |
|---|---|
| [`read`](docs/commands/markfluence_read.md) | Print one page to stdout as Markdown, or as raw storage format |
| [`export`](docs/commands/markfluence_export.md) | Write a page, a page tree, or a whole space to files, with the attachments |
| [`page-info`](docs/commands/markfluence_page-info.md) | Show the metadata of one page: the space, the parent, the version, the width, the labels, and the authors |
| [`space-info`](docs/commands/markfluence_space-info.md) | Show the metadata of one space: what you can do in it, the page statuses that it gives you, and exact page counts |
| [`user-info`](docs/commands/markfluence_user-info.md) | Show who these credentials are and where they can publish. It also shows who an account id names |

**To find pages:**

| | |
|---|---|
| [`find`](docs/commands/markfluence_find.md) | Resolve an exact title to page ids and folder ids. It sees archived pages and folders, and `search` cannot see them. A folder id is correct only as a `parent` |
| [`search`](docs/commands/markfluence_search.md) | Do a full-text search, for when you do not know the title. It takes raw CQL with `--cql` |
| [`children`](docs/commands/markfluence_children.md) | List what is below a page, a folder, or a space |
| [`user-find`](docs/commands/markfluence_user-find.md) | Resolve the name of a person to the account id, and to the Markdown line that mentions them |

**Attachments.** The `create` and `update` commands do the work for the images
of a page. These commands are for all the other attachments:

| | |
|---|---|
| [`attachment-list`](docs/commands/markfluence_attachment-list.md) | Show what is attached to a page |
| [`attachment-upload`](docs/commands/markfluence_attachment-upload.md) | Attach a file. It skips a file when the checksum already agrees |
| [`attachment-download`](docs/commands/markfluence_attachment-download.md) | Get attachments back to the paths that they were published from |

The [`schema`](docs/commands/markfluence_schema.md) command prints the `--json`
schema.

Every command takes `--json`. The `create`, `update`, `export`,
`attachment-upload`, and `attachment-download` commands take `--dry-run`.

### Publish from CI

Use **[markfluence-action](https://github.com/mozilla/markfluence-action)**.

This action publishes the files that changed in a GitHub repository to
Confluence.

### Common workflows

To edit a page that already exists:

```sh
# what is its page id?
markfluence find --space SRE "Deploy runbook"
# download it and all its attachments
markfluence export 1234567890

# ...edit the file that it wrote...

# is this Markdown correct?
markfluence check deploy-runbook.md
# publish the changes to Confluence
markfluence update deploy-runbook.md
```

To create a new page:

```sh
vi deploy-runbook.md

# ...write the file...

# make sure that the Markdown is correct
markfluence check deploy-runbook.md
# create the page at the top level of the ENG space
markfluence create --space ENG deploy-runbook.md

# ...make some edits...

# make sure that the Markdown is correct
markfluence check deploy-runbook.md
# publish the edits
markfluence update deploy-runbook.md
```

To export a whole tree of pages and edit them:

```sh
# export a whole tree of pages, with the attachments that they reference
markfluence export --depth all --dest docs 1234567890

# ...make edits...

# make sure that the Markdown is correct
markfluence check docs/*.md docs/**/*.md
# update each page that changed
markfluence update docs/*.md docs/**/*.md
```

To get changes that somebody made in Confluence, do a read and then an edit.
Every command that writes goes one way, from your files to the page. The
`page-info` command shows the labels and the width of a page. The `read`
command prints the page as Markdown. The `export` command writes the whole page
to disk, with its frontmatter. No command writes the frontmatter of a file that
already exists from a page.

### What the output looks like

Look at the shape of these 3 commands before you run them. The `children`
command indents each row by its depth. It keeps the `TYPE` column and the `ID`
column in line, so you can use `grep` on the output:

```
TYPE    ID          TITLE
folder  2876047392  Articles
page    1675427879  MozCloud planning
page    1671692338    MozCloud observability focus and issues
```

The `find` command reports current pages, archived pages, and folders together.
All 3 types can hold the title that you asked for, but one type only is a page
that you can publish to:

```
TYPE    ID          SPACE          STATUS    TITLE           URL
page    1675427879  ENG            current   Deploy runbook  https://…
page    1293156436  CLOUDSERVICES  archived  Deploy runbook  https://…
```

The `search` command prints a block for each hit, not a row. The excerpt tells
you *why* the page matched. markfluence shows the terms that Confluence marked
in reverse video:

```
Deployment runbook
  page 2064154670  PXI
  https://org.atlassian.net/wiki/spaces/PXI/pages/2064154670/Deployment+runbook
  Keep an eye on the deploy-packages job...to be deployed)

    Showing 2 matches; more exist (use --limit all).
```

### `--json` output

The persistent `--json` flag makes a command write one machine-readable JSON
document to stdout. The command does not write its human output. Use this flag
for scripts and for CI. The output pipes to `jq` without trouble:

```sh
markfluence page-info 1234567890 --json | jq '.results[0].page_width'
markfluence update docs/*.md --json | jq '.summary'
```

The output is a stable **envelope** with a version. The `results` field always
holds one object for each target. It holds one element for `page-info` and for
`read`. The `summary` field holds the counts for the batch:

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

The full contract is a JSON Schema in draft 2020-12 at
[`schema/json-output/v1.json`](schema/json-output/v1.json). The `command` field
selects the shape of each `results` item and the shape of `summary`. The error
object on stderr is `#/$defs/errorObject`. A test does a check of the real
output of markfluence against the schema, so the schema cannot drift from the
code.

The binary holds the same schema. Thus a consumer can get the contract and
know nothing about this repository. See [`schema`](#schema).

**[docs/json-output.md](docs/json-output.md)** gives these other details:

- The status verbs of each command.
- What counts as one result for each command.
- The `broken` status of `check`.
- The preflight abort of `create`.
- Why `find` and `search` report an operational failure on stderr, and not as a
  result.

These are the exit codes:

| Exit code | Meaning |
|---|---|
| `0` | Success. This includes "no matches", because that is an answer that a caller acts on |
| `1` | A failure for one file or one target. The envelope is still on stdout. Each failed result has `ok: false` and an `error` value and a `code` value |
| `2` | A fatal preflight failure, such as a bad flag or a credential that does not resolve. There is no envelope. A typed error object goes to **stderr** instead |

The `diff` command is the one exception. It uses the exit codes of `diff(1)`:
`0` when the two are the same, `1` when they are different, and `2` for any
trouble. Thus `if markfluence diff FILE >/dev/null 2>&1` means "the file and
the page agree". The operational failures of `diff` give a `2`, and every other
command gives a `1` for them.

```json
{ "schema_version": 1, "command": "update", "error": "…", "code": "CONFIG", "warnings": [] }
```

These are the error `code` values: `CONFIG`, `AUTH`, `NOT_FOUND`, `VALIDATION`,
`CONVERT`, `IO`, `NETWORK`, `API`, and `CONFLICT`. `update` gives `CONFLICT`
when it refuses to overwrite a page that somebody changed after you made your
copy.

### `schema`

```
Usage: markfluence schema
```

This command prints the JSON Schema for the [`--json`
output](#--json-output) to stdout. The binary holds the schema, so a script, a
CI job, or an agent can get the contract while it runs. It can do a check of
the output of markfluence, or it can generate types from the schema. It does
not read the schema from this repository:

```console
$ markfluence schema | jq -r '.properties.command.enum | join(" ")'
page-info space-info user-info read update create check diff children find search user-find attachment-list attachment-upload attachment-download export

$ markfluence update docs/*.md --json > out.json
$ markfluence schema > schema.json
$ check-jsonschema --schemafile schema.json out.json
```

The document that it prints is byte-for-byte the same as
[`schema/json-output/v1.json`](schema/json-output/v1.json) at the revision that
the binary was built from. It describes the `schema_version` that the binary
writes. The tests do a check of the real output against the same embedded copy.
Thus the schema that you get from a binary is the schema that its output was
checked against.

This command does not talk to Confluence, so it needs no credentials. The
output is already JSON, and `--json` changes nothing.

## Markdown page structure

Each Markdown file is one Confluence page. A file has an optional YAML
**frontmatter** block, and then the Markdown **body**.

```
---
title: My Page Title
space: ENG
parent: null
page_id: 1234567890
page_status: Ready for review
page_width: max
---

# Body starts here
...
```

**[docs/markdown_file.md](docs/markdown_file.md) is the reference for the page
format.** It gives every frontmatter field and what each verb does with it. It
also gives every body construct:

- Tables and the conventions for their cells.
- GitHub alerts.
- Images, and how their paths resolve.
- Links between pages.
- Mentions.
- Confluence storage markup that you paste in.

## The documentation root

**Do you need a `markfluence.yaml` file?**

- If you export, edit, and publish files **one at a time**, no. The root of
  each file is its own directory, and that directory is already the directory
  that you want.
- If you work on a **tree of directories**, yes. An example is a set of pages
  that link to each other, or that share an `assets/` directory. Put a
  `markfluence.yaml` file at the root of that tree. Without one, the root of
  each file is still its own directory. Then a page cannot reach an image or
  another page that is *above* itself. A shared-assets layout as in
  [docs/markdown_file.md](docs/markdown_file.md) does not work at all without a
  declared root.

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
```

The file can also hold **defaults for the whole project**. With these defaults,
you write `space: ENG` one time, and not one time in each of 100 files:

```yaml
space: ENG
page_width: max
```

Each setting is a default, and a file overrides it. The sequence is **the flag
first, then the frontmatter, then the project file**. Thus the answer that is
nearest to the content wins. A key that markfluence does not recognise is an
error, and markfluence does not ignore it. A typo in a default for the whole
project is wrong for every file at the same time. Credentials are not settings
here, and that is deliberate. See
[docs/root-model.md](docs/root-model.md#what-it-deliberately-does-not-hold).

The file can also hold a **`pages:` block**. This block gives the page metadata
of each file, so the Markdown itself stays clean with no frontmatter at all:

```yaml
pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
```

This block is what makes `markfluence update docs/**/*.md` work from CI with no
input for each file. It is also why `update` has no `--page-id` flag and no
`--title` flag. Each of those flags can name one file only. Both locations are
legal, and markfluence says nothing when the two agree. Thus you can move the
metadata one file at a time. markfluence skips a file that neither location
mentions, and it does not fail. For the details, see
[docs/root-model.md](docs/root-model.md#pages--page-metadata-for-a-pristine-file).

The other part of this section is the exact version of the same idea. Every
Markdown file has a **documentation root**. The root is the directory that holds
`markfluence.yaml`, and markfluence finds it when it goes up from the directory
of the file. If no `markfluence.yaml` file is above the file, the root is the
directory of the file itself. The root bounds which images and which `parent:`
references a file can read. The recorded source path of an image is relative to
the root. markfluence reports the root that it used one time for each different
value in a run. The `--root PATH` flag overrides this search for the whole
command. For `create`, `update`, `diff`, and `attachment-upload`, it also moves
the directory that markfluence reads `.env` from. See
[Configure](#configure).

For the reasons behind this model, what it corrects, and what it costs, see
[docs/root-model.md](docs/root-model.md) and
[_plans/025_file-organization.md](_plans/025_file-organization.md). Those
documents also give every setting for the whole project.

### Move files and assets

**To move or rename a Markdown file.** Move it. The links to it resolve by its
real location, through the link index that is relative to the root. You edit
nothing in the other files. You publish nothing again, except the file that you
moved. Publish that file to get its own new links, if any link changed.

**To move an image or another asset.** Move it, with its page or without its
page. The name of an attachment is the file name of the asset, and not its
path. Thus a move keeps the same attachment. At the next publish, markfluence
records the new path on the attachment it already has. This is L3 in
[docs/guarantees.md](docs/guarantees.md), `identity-from-asset-location`.

**To rename an asset.** A new file name is a new attachment. The next publish
uploads the asset with the new name. The attachment with the old name stays on
the page with no reference to it, because markfluence never deletes. Issue
[#99](https://github.com/mozilla/markfluence/issues/99) tracks a future
`attachment-prune` command.

**Two assets with the same file name.** One page cannot reference two assets
with the same file name, such as `arch/diagram.png` and `deploy/diagram.png`.
markfluence refuses to publish that page, because the two assets would get
the same attachment name. Rename one of them.

## Inspirations

[pchuri/confluence-cli](https://github.com/pchuri/confluence-cli) for the
command line interface. markfluence tries to match the subcommands and the
arguments of confluence-cli. markfluence gives more attention to the publish of
Markdown documents, and less attention to a CLI for the full Confluence v1 and
v2 API.

[kovetskiy/mark](https://github.com/kovetskiy/mark) for Markdown support in
Confluence, and for how it represents things. markfluence tries to match the key
design decisions. It has defaults that I prefer, and it works better in
different scenarios.
