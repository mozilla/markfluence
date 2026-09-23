# The documentation root

A Markdown file refers to other files with relative paths: an image, a link to
a different `.md` file, or a `parent:` path. To use a reference, markfluence
expands its relative path into the path of a file on the filesystem. This
document tells you how markfluence expands references, and what the
documentation root has to do with that.

The [Documentation root](../README.md#the-documentation-root) section of the
README gives the practical version. It tells you how to move a file, and how to
share an asset between pages.

## How markfluence expands a file reference

Each file reference is relative to a directory. markfluence joins the reference
to that directory, and cleans the result by its text: it removes each `.` and
applies each `..`. The result is the path of a file on the file system.

markfluence does not resolve a symlink to do this.

The documentation root does two things for every expansion:

- **It bounds the result.** An expanded path must be inside the root. If it is
  not, markfluence does not read the file. If you `read` or `export` content
  from Confluence, references could read outside of the root to anywhere on your
  file system. Preventing read/write file access outside of the directory root
  prevents unsafe file access.
- **It is the base of each path that markfluence records.** When markfluence
  writes a path down, the path is relative to the root. Examples are the
  recorded source of an attachment, a `pages:` key, and the `file` key of a log
  line. Thus a recorded path has the same meaning on every machine.

| reference | relative to | if it expands outside the root |
|---|---|---|
| an image, `![logo](../assets/logo.png)` | the directory of the Markdown file | `IMAGE BROKEN`, and markfluence publishes the page |
| a link, `[setup](../setup/overview.md)` | the directory of the Markdown file | `LINK BROKEN`, and markfluence publishes the page |
| a frontmatter `parent: ../index.md` | the directory of the Markdown file | a hard error: the file fails |
| a `pages:` key in `markfluence.yaml` | the root | an error when markfluence loads the file |
| a `parent:` in a `pages:` entry | the root | a hard error: the file fails |

Thus `../assets/logo.png` from a page one directory below the root is in
bounds, and the same reference from a page at the root is not.

The three results are different on purpose:

- **An image** is a leaf. markfluence also refuses a symlink, also when it
  resolves inside the root (`os.Lstat`, not `os.Stat`). A broken image is
  visible on the page, so the page still publishes.
- **A link** needs no bound check at all. markfluence builds the link index
  (`internal/linkindex`) when it goes *down* from the root at startup. It
  uses the link index to resolve links. Thus a link that expands to a path
  outside the root is never in the index. See
  [Link resolution](#link-resolution), and see
  [Non-goals](design-principles.md#symlinks) for why a symlinked ancestor also cannot
  trick the walk.
- **A parent** is important. A publish under the wrong parent, or under no
  parent with no message, is worse than no publish. Thus a parent that escapes,
  or that is a symlink, stops the file.

markfluence expands a path by its text alone, and never asks the filesystem
where a symlink points. Thus the same reference expands to the same file on
every machine.

## How markfluence finds the root

markfluence finds the root separately for each Markdown file. It goes up from
the directory of that file and looks for `markfluence.yaml`. The first ancestor
that has one is the root. If it gets to the filesystem root with no result,
then there is no `markfluence.yaml` file and the directory of the file is the
root. `--root` overrides this for every file in the command (see
[`--root`](#--root)).

When a project file exists and every file in a batch is under it, every file
gets the same root. That is the usual case, and it is the intended case. The
root is different for each file in only two cases. There is no project file at
all, or a batch covers more than one project (see
[Multi-root batches](#multi-root-batches-are-allowed)).

## Where markfluence reads `.env`

`.env` is not a reference in a file. markfluence reads it one time for each
invocation, before it touches any file. It uses the same search as for a root,
but it starts from the **working directory**, and not from the directory of a
file. If the search finds no `markfluence.yaml`, markfluence reads `.env` from
the working directory itself. `--env-file` overrides this fully.

`create`, `update`, `diff`, and `attachment-upload` already build a
`project.Cache` to find the root of each file. They give the same cache to the
resolver of the client configuration (`client.ResolveOptions.Roots`). Thus for
those commands:

- `--root` also moves the directory that markfluence reads `.env` from.
- markfluence does the search one time, and not two times.

A command that has no per-file root, such as `read` or `search`, builds no
cache. For it, `--root` has no effect on `.env`.

## `markfluence.yaml`: the project file

This file marks the root, and it declares settings for the whole project
([#100](https://github.com/mozilla/markfluence/issues/100)). A file with no
settings in it is normal. `export` writes that file, and marking the root was
the only job of the file until settings came:

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
```

### The settings

| Setting | What it defaults | Overridden by |
|---|---|---|
| `space` | the space `create` publishes into | `--space`, then a frontmatter `space:` |
| `page_width` | the width `create` and `update` assert | `--page-width`, then a frontmatter `page_width:` |

```yaml
space: ENG
page_width: max
```

The sequence is **the flag first, then the frontmatter, then the project
file**. The answer that is nearest to the content wins.

> [!NOTE]
> This is *not* the sequence for credentials, and the design of the file stops
> you from using one for the other. Credentials resolve in the sequence **the
> flag, then the environment, then `.env`**, and they tell *who you are*. A
> setting here tells *what the content is*.

markfluence uses a setting from markfluence.yaml only when neither the flag nor
the frontmatter gives one. Thus a project setting never disagrees with anything.
`create` still refuses a `--space` that does not agree with a frontmatter
`space:`, and a project default cannot become a third party to that.

> [!NOTE]
> For `page_width:`, `update` asserts a width only when something declares
> one, and a declaration for the whole project counts. If a project wants
> markfluence to leave the live width of each page alone, do not add the key.

Settings are specific to each root. Thus an invocation that covers two
projects gets the defaults of each project. See
[Multi-root batches](#multi-root-batches-are-allowed).

### `pages:` — page metadata for a pristine file

A `pages:` block maps a path to the page metadata of that file. Thus
markfluence can publish a Markdown file that has no frontmatter at all:

`docs/deploy-runbook.md`:

```markdown
# Runbook: How to deploy LookupService

Summary: Deploying LookupService consists of 23 manual steps ...
```

`markfluence.yaml`:

```yaml
space: ENG

pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
    labels: [runbook]
```

**A `pages:` entry has the same structure and fields as the frontmatter block in a Markdown file.**
You can use either location for metadata, but some field values differ depending
on which location they're in. Additionally, markfluence has rules for occasions
where there is metadata in both locations and it disagrees. This allows you to
migrate from one model to the other one file at a time.

When a Markdown page has both frontmatter and a `pages:` entry and the two
disagree, the result depends on what the disagreement can destroy:

| field | on disagreement |
|---|---|
| `page_id`, `space`, `parent` | **error**: the file fails. A `page_id` pasted from an old file would publish over a live page |
| `title`, `page_width`, `labels`, `page_status` | **warning**, and the frontmatter wins. You can see it and recover from it |

`markfluence create` writes a `pages:` entry when the metadata of the file
belongs here. A project that uses `pages:` never gets frontmatter by accident. A
project that does not use `pages:` uses frontmatter.  A file that already has
its own frontmatter keeps it. Thus create never adds a pages: entry for a file
that has frontmatter, and a file's metadata is never in two places without your
knowledge.  `--no-persist` records no metadata in either location. It still adds
a line to the action log (see
[`.markfluence/`](#markfluence-local-state-not-committed)).

A `parent:` that names a `.md` file differs in what it's relative to. For example:

```
markfluence.yaml
docs/
  index.md           # parent page
  runbook.md         # child page
```

- In the frontmatter of `runbook.md`, write parent: `index.md`. The path is
  relative to the file, as a Markdown link is. `create --parent` uses the same
  rule.
- In the `pages:` entry in `markfluence.yaml` for `docs/runbook.md`, write
  `parent: docs/index.md`.  The path is relative to the root, as the key of the
  entry is.

If you move a `parent:` line from one location to the other by hand, change the
path.

`create` records the resolved page id, and not a path, so a round trip never
hits this problem.

You can give any command that takes a page a Markdown file, also when its
`page_id` is in a `pages:` entry and not in the file. For example,
`markfluence page-info docs/deploy-runbook.md` works on a file with no
frontmatter. All of these commands resolve the file in one place
(`internal/pageref`), so each one reads both locations.

markfluence **skips a file that has no metadata in frontmatter or `pages:`**.
A repository can correctly hold Markdown that is not published. Thus
`markfluence update docs/**/*.md` does not fail because somebody added a draft
or because some of the files are not meant to be published to Confluence.
A file that *is* registered but has no `page_id` fails.

**Path keys in `pages:`** are relative to the document root, use slashes, and
are cleaned by their text (`./docs/a.md` and `docs/a.md` are the same key). There are two
conditions when markfluence loads the file that raise an error: a key that goes
outside the root, and two `pages:` keys that normalize to one path. Each one
means that the structure of the manifest is wrong, and not only one entry.

markfluence compares keys exactly, with no case folding. On a filesystem that
ignores case, `Docs/a.md` opens the file but matches no `docs/a.md` key. Thus
the file is unmanaged, and markfluence skips it.

Resolution is lexical, and it never follows symlinks.

### A `markfluence.yaml` file that markfluence cannot understand stops the command

A `markfluence.yaml` file that markfluence cannot parse, or that holds a key
that markfluence does not recognise, is an error that names the problem.
markfluence does **not** treat it as a valid root marker. The search does not
continue to an ancestor that has a better marker. It also does not use the
directory of the Markdown file.

The root decides the recorded path of every attachment and bounds every read.
Thus if markfluence cannot understand the project file, the boundary of the
project is not known, and a guess is worse than a stop.

**markfluence refuses a `markfluence.yaml` that has a field it does not know.**
This catches two problems:

- An older markfluence. A file written for a newer version can have a setting
  that an older version does not know. If the older version ignored it, it would
  publish every page in the project wrong, with no message. The refusal makes you
  upgrade.
- A typo. markfluence finds `spce: ENG` when it loads the file, and does not
  ignore it.

Because of this refusal, the file needs no schema version. The error says that
an older markfluence is the probable cause.

`markfluence check` does a check of a project file offline, with the Markdown
files that you give it.

### What `markfluence.yaml` deliberately does not hold

There is no `url`, `username`, or token. The reason is more exact than "those
are credentials". markfluence sends basic auth to the host that the resolved
URL names. Thus a `url:` here would decide where `CONFLUENCE_TOKEN` goes. This
file is committed and shared, and markfluence finds it when it goes up from a
subdirectory. One line in a pull request would send the token of a CI run to a
host that the author chose.

The difference from the settings that it does hold is the full argument. A
wrong `space` publishes to the wrong place in a Confluence instance, and you can
see that and repair it by hand. A wrong `url` gives away the token, and you can
do neither.

This file is committed and shared. `.env` is different: git ignores it, and it
is personal. A stray `.env` in an ancestor directory can give a project
credentials that are not its own. That is exactly why markfluence reports the
root, and thus where it read `.env` from. `create`, `update`, and
`attachment-upload` print it, and `--json` gives it in `roots`. The protection
is that you can see it, and not a permission check.

`markfluence` *reads* a project file, but it *executes* nothing because the
file is present. Nothing in the file can send a credential to a different
place. To go up and trust what you find is the shape of
[CVE-2022-24765](https://github.blog/2022-04-12-git-security-vulnerability-announced/).
That was git before the fix, which went up to find `.git` with no check of the
owner. It is also the shape of the CVEs for hook execution from a `.git`
directory that came after it (for example, CVE-2024-32002).

markfluence uses the discovery model of `.editorconfig`, and not the model of
git: go up, the nearest file wins, and nothing executes. If somebody adds a
hook system later, it must have its own consent step, separate from the
presence of this file. This document does not decide that.

## `.markfluence/`: local state, not committed

Next to `markfluence.yaml`, a root gets a `.markfluence/` directory the first
time that a command has something to record there. Now it holds one file,
`log.jsonl`. This is the action log, and markfluence only adds lines to it
(#149).

**It is not committed**, and the directory ignores itself. With the first line
that it writes, markfluence also writes a `.markfluence/.gitignore` that holds
`*`. Thus you do not have to add anything to a `.gitignore` that markfluence
does not own. markfluence never overwrites a `.gitignore` that exists.

The log is not committed for a structural reason, and not for convenience. A
shared repository *is* a declaration that the repository is the source of
truth. In that arrangement, `update --force` is the answer, and markfluence
reads nothing in the log.

Thus the log serves one arrangement: a local copy, with the source of truth in
Confluence. There, state that is specific to each checkout is the correct
shape, because the sync point of a different person is not related to mine. A
commit of the log would give nothing. It would also cause a conflict at the end
of the file on every concurrent publish.

### What the log is for

Without the log, `update` cannot tell two cases apart. In one case, the page is
different from my file because I have edits to publish. In the other case, the
page is different because somebody published first. To tell them apart,
markfluence needs a merge base: the version that *this copy* came from. No
state on the page can hold that, because the page cannot know where a local
copy came from.

Thus `create`, `update`, and `export` each add a line. The line records the
page version that the command left, and a sha of what the body `PUT` sent. The
last successful line for a file is the base of that copy.

There is one JSON object on each line. markfluence only adds lines. It accepts
a field that it does not recognise, so the format can grow with no version:

```jsonl
{"time":"...","action":"update","status":"ok","file":"docs/some-page.md","page_id":"123456789","page_version":44,"publish_sha256":"b800cc4f…","markfluence":"1.2.3"}
```

`file` is the same lexical key, relative to the root, that
[`pages:`](#pages--page-metadata-for-a-pristine-file) uses. That is what makes
a batch with more than one root work. The line for each file goes to the log of
its own root. A key means nothing without the root that it is relative to.

**No run depends on the log.** Two checks in `update` use it: the refusal to
overwrite a page that moved, and the skip of a body that did not change. But a
log that is missing, unreadable, corrupt, or half-written only makes those
checks weaker. It never stops the run. markfluence skips a line that does not
parse. If markfluence cannot read a log, no file has a base, and markfluence
publishes.

A root with **no `markfluence.yaml` at all** gets no log, and markfluence does
not create one. The reason is the same reason that nothing here creates a
project file: that is [#5](https://github.com/mozilla/markfluence/issues/5).

## `--root`

This is a persistent flag that overrides the search for the root, for the
whole invocation. It
applies one value to every file in the same way. It is not a per-file setting,
because it means "use this directory as the root, and nothing else". That
includes a tree that has no `markfluence.yaml` and never will, such as a
checkout that you do not control or a generated snapshot.

## Attachment identity

The recorded `Source` of an image is relative to the root, and not to the page
that references it (`_plans/026` commit 4). `read` and `export` use it to put
the image back where it came from, and it is the only record of the path. The
attachment *name* is the base name of the file, and it has none of this path
(`_plans/029`).

Two pages at different depths that reference the same file now record the same
source and get the same attachment. Before, each page recorded the reference as
it was written, and the same file had two identities in Confluence. This is L3
(`identity-from-asset-location`, `docs/design-principles.md`). Because the name is
only the base name, a move of an image keeps the same attachment, and the next
publish records the new path. A rename of the file creates a new attachment.
See the recipes in the README for what that means in practice.

## Link resolution

`internal/linkindex.Build` goes through the tree of the root one time. It keys
the page map and the anchor map by the path of each file relative to the root,
and not by the bare filename. Thus a link to `setup/overview.md` cannot go to an
unrelated `overview.md` in a different part of the tree (`_plans/025` Scenario
A).

markfluence builds the index one time for each root, and shares it with every
file that it converts under that root in the same command. This is also about
80 times faster at 400 files. A new index for each directory and each
conversion was an O(n²) cost that nobody intended (the measurement of
`_plans/025`).

A link that would resolve outside the root, such as
`../../../../etc/passwd.md`, is never in the index. markfluence reports it as
Broken: it replaces the whole link with the text
`LINK BROKEN: … (outside the documentation root)`. It does the same for a
target that does not exist (`not found`). A target that exists but has no
`page_id` yet gives a warning. This is **R1** (`report-unresolved-references`),
and `markfluence check` gives the same diagnostics for a whole tree with no
publish (#42).

The reserve phase of `create` (`_plans/026` commit 8) is the other half of link
resolution. Preflight first converts every file to find defects, and discards
the result (#127). Then markfluence reserves a Confluence id for every file in
the batch before it publishes any of them. Each reservation is a stub with no
content.
markfluence puts each id into the shared index immediately (`Index.SetPage`),
also under `--no-persist`. Thus a link between two files in the same batch
resolves, in either direction, and also when the two files link to each other.

## Multi-root batches are allowed

One invocation can cover more than one project, and nothing refuses this. The
projects can be nested `markfluence.yaml` files, or files under fully separate
ones.

markfluence finds the root of each file separately. A link across the
boundary of a root does not resolve. That is an unresolved link, and not an
error, as any other miss is. A `parent:` that escapes the root of its file is
still a hard error. This is true also when the target is part of the same
command under a *different* root.

This comes from finding the root for each file, with no special case. The
resolution of `.editorconfig` and `tsconfig.json` nests in the same way, and
they do not need to forbid it.
