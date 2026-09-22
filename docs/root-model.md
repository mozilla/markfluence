# The documentation root

This document tells you how markfluence finds the directory that bounds the
reads of a Markdown file and names its attachments. The model itself is in
`_plans/025_file-organization.md`. This document explains, for a reader, what
that plan decided. It does not give all the reasoning again.

`_plans/026_file-organization-implementation.md` is the log of the
implementation, one commit at a time, if you want to know when a part landed.
The [Documentation root](../README.md#the-documentation-root) section of the
README gives the practical version. It tells you how to move a file, and how to
share an asset between pages.

## The root, and the other lookup that looks like a root

markfluence has two different directory lookups. Most of the confusion that
this model removes came from the use of one for the other.

**markfluence finds the root for each Markdown file.** It goes up from the
directory of that file and looks for `markfluence.yaml`. The first ancestor
that has one is the root. If it gets to the filesystem root with no result, the
directory of the file is the root.

This is the lookup that is important for correctness. It bounds what a file can
read, such as an image or a `parent:` reference. The recorded source path of
an attachment is relative to it. markfluence builds the link index for the
tree from it.

When a project file exists and every file in a batch is under it, every file
resolves to the same root. That is the usual case, and it is the intended case.
The root is different for each file in only two cases. There is no project
file at all, or a batch covers more than one project (see below).

**The `.env` lookup is a separate, narrower pass.** It starts from the
**working directory**, and not from the directory of a file. If it finds
nothing, it uses the working directory itself. Its only purpose is to answer
"where is `.env`?". It runs one time for each invocation, before markfluence
touches any file, and it bounds nothing. The code never calls it "root", and
markfluence does not report it. `--env-file` overrides it fully, and nothing
here changes that.

Both passes go up with the same primitive: the check for the marker in each
directory, in `internal/project`. They start from two different points, and
they use two different fallbacks when they find nothing. There is no second
algorithm, only a second starting point.

In the plain case (`project.Discover(cwd)`), that walk does not depend on
anything else in the invocation. But `create`, `update`, `diff`, and
`attachment-upload` each already build a `project.Cache` for their own per-file
root resolution. They give that same cache to the resolver of the client
configuration (`client.ResolveOptions.Roots`). Thus `.env` does not make its
own separate walk.

This has two results, for exactly those commands. A command with no per-file
root, such as `read` or `search`, never builds a cache to share.

- `--root` otherwise moves only the per-file root that images, links, and
  `parent:` resolve against. For these commands, it now also moves `.env`.
- markfluence does the walk one time, and not two times.

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

This is *not* the sequence for credentials, and the design of the file stops
you from using one for the other. Credentials resolve in the sequence **the
flag, then the environment, then `.env`**, and they tell *who you are*. A
setting here tells *what the content is*.

markfluence reads a setting for the whole project only when the two levels
above it say nothing. Thus it never takes part in a disagreement. `create`
still refuses a `--space` that does not agree with a frontmatter `space:`, and
a project default cannot become a third party to that.

Know one result before you add `page_width:`. `update` asserts a width only
when something declares one, and a declaration for the whole project counts. If
a project wants markfluence to leave the live width of each page alone, do not
add the key.

Settings are specific to each root. Thus an invocation that covers two
projects gets the defaults of each project. See
[Multi-root batches](#multi-root-batches-are-allowed).

### `pages:` — page metadata for a pristine file

A `pages:` block maps a path to the page metadata of that file. Thus
markfluence can publish a Markdown file that has no markfluence keys at all:

```yaml
space: ENG

pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
    labels: [runbook]
```

**An entry is a frontmatter block in a different location.** It has the same
field names, the same value domains, and the same canonical sequence. Both
locations are legal, and markfluence says nothing when they agree. Thus you can
move metadata into the manifest one file at a time.

When the two disagree, the result depends on what the disagreement can
destroy:

| field | on disagreement |
|---|---|
| `page_id`, `space`, `parent` | **error**: the file fails. A `page_id` pasted from an old file would publish over a live page |
| `title`, `page_width`, `labels`, `page_status` | **warning**, and the frontmatter wins. You can see it and recover from it |

`markfluence create` writes an entry here when the metadata of the file belongs
here. A project that uses `pages:` never gets frontmatter by accident. A
project that does not use `pages:` works as it did before. A file that already
has its own frontmatter keeps it. Thus in a tree that is half migrated, entries
do not appear behind you. `--no-persist` records no metadata in either
location. It still adds a line to the action log (see
[`.markfluence/`](#markfluence-local-state-not-committed)).

One spelling needs attention. A `parent:` that names a `.md` file is
**relative to the root in an entry**, as every `pages:` key is. It is
**relative to the file** in frontmatter and in `create --parent`, as a Markdown
link is. Thus the same parent is `docs/index.md` in an entry and `index.md`
from a sibling file.

markfluence changes one form to the other, so nothing breaks. But if you move a
`parent:` value from one location to the other by hand, spell it again.
`create` records the resolved page id, and not a path, so a round trip never
has this problem.

Every command that takes a page accepts a pristine registered file. All of
them resolve the argument in one place (`internal/pageref`). Thus
`markfluence page-info docs/deploy-runbook.md` works, although that file says
nothing about Confluence. There is no exception. `fix` was one until it was
removed (#151). It could find such a page, but it could not write to its entry.

markfluence **skips a file that neither location mentions**. It does not fail
it. A repository can correctly hold Markdown that nobody publishes. Thus
`markfluence update docs/**/*.md` does not fail because somebody added a draft.
A file that *is* registered but has no `page_id` fails. Something claimed it,
and nobody created the page yet.

**Path keys** are relative to the root, use slashes, and are cleaned by their
text (`./docs/a.md` and `docs/a.md` are the same key). Two conditions are
errors when markfluence loads the file: a key that goes outside the root, and
two keys that normalize to one path. Each one means that the structure of the
manifest is wrong, and not only one entry.

markfluence compares keys exactly, with no case folding. On a filesystem that
ignores case, `Docs/a.md` opens the file but matches no `docs/a.md` key. Thus
the file is unmanaged, and markfluence skips it.

Resolution is lexical, and it never follows symlinks. **L2** needs the same of
everything else here, for the same reason. If the meaning of a key depended on
the layout of the checkout, the key would resolve differently on two machines.

### A file that markfluence cannot understand stops the command

A file that markfluence cannot parse, or that holds a key that markfluence does
not recognise, is an error that names the problem. markfluence does **not**
treat it as a valid root marker. The search does not continue to an ancestor
that has a better marker. It also does not use the directory of the Markdown
file.

The root decides the recorded path of every attachment and bounds every
read. Thus if
markfluence cannot understand the project file, the boundary of the project is
not known, and a guess is worse than a stop.

**The refusal of an unrecognised key is the purpose, and not a limit.** A
`markfluence.yaml` written for a newer markfluence holds keys that an older
binary would ignore. Assume that markfluence ignored a default for the whole
project. Then it would publish with the wrong space or the wrong width,
everywhere at the same time, with no message. The refusal also finds
`spce: ENG`. Thus the file has no schema version, and the error says that an
older binary is the probable cause.

`markfluence check` does a check of a project file offline, with the Markdown
files that you give it.

### What it deliberately does not hold

There is no `url`, `username`, or token. The reason is more exact than "those
are credentials". markfluence sends basic auth to the host that the resolved
URL names. Thus a `url:` here would decide where `CONFLUENCE_TOKEN` goes. This
file is committed and shared, and markfluence finds it when it goes up from a
subdirectory. One line in a pull request would send the token of a CI run to a
host that the author chose.

The difference from the settings that it does hold is the full argument. A
wrong `space` publishes to the wrong place in your own instance, and you can
see that and repair it by hand. A wrong `url` gives away the token, and you can
do neither.

This file is committed and shared. `.env` is different: git ignores it, and it
is personal. A stray `.env` in an ancestor directory can give a project
credentials that are not its own. That is exactly why markfluence reports the
root, and thus where it read `.env` from. `create`, `update`, and
`attachment-upload` print it, and `--json` gives it in `roots`. The protection
is that you can see it, and not a permission check.

`markfluence` *reads* a project file, but it **executes** nothing because the
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

**No `init` command makes this file.** Make it by hand. That is
[#5](https://github.com/mozilla/markfluence/issues/5).

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

This is a persistent flag that overrides discovery for the whole invocation. It
applies one value to every file in the same way. It is not a per-file setting,
because it means "use this directory as the root, and nothing else". That
includes a tree that has no `markfluence.yaml` and never will, such as a
checkout that you do not control or a generated snapshot.

## What the root bounds

**S1/S2** (`docs/guarantees.md`): markfluence writes no file outside the root.
Since `_plans/026` commit 6, it also reads no file outside the root. There are
3 reads:

- An **image leaf**. It resolves relative to the directory of the page, and
  the root bounds it. Thus `../assets/logo.png` from a page one directory below
  the root is in bounds, and the same reference from a page at the root is
  not. A path that resolves
  outside the root is `IMAGE BROKEN`. markfluence refuses a symlink, also when
  it resolves inside the root (`os.Lstat`, not `os.Stat`).
- A frontmatter **`parent:` path**. markfluence reads it through the same root,
  but the failure is different on purpose. A parent that escapes, or that is a
  symlink, is a **hard error**, and not an image that is broken and reported.
  The parent is important: a publish under the wrong parent, or under no parent
  with no message, is worse than no publish.
- **Link and anchor resolution** needs no clamp at all. markfluence builds the
  index (`internal/linkindex`) when it goes *down* from the root. Thus nothing
  outside the root can be in the index, and markfluence does not find a
  destination that would escape. The guarantee holds by construction, and not
  by a check. See [Non-goals](guarantees.md#symlinks) for why a symlinked
  ancestor also cannot trick the walk.

## Attachment identity

The recorded `Source` of an image is relative to the root, and not to the page
that references it (`_plans/026` commit 4). `read` and `export` use it to put
the image back where it came from, and it is the only record of the path. The
attachment *name* is the base name of the file, and it has none of this path
(`_plans/029`).

Two pages at different depths that reference the same file now record the same
source and get the same attachment. Before, each page recorded the reference as
it was written, and the same file had two identities in Confluence. This is L3
(`identity-from-asset-location`, `docs/guarantees.md`). Because the name is
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

markfluence finds the root of each file independently. A link across the
boundary of a root does not resolve. That is an unresolved link, and not an
error, as any other miss is. A `parent:` that escapes the root of its file is
still a hard error. This is true also when the target is part of the same
command under a *different* root.

This comes from per-file discovery with no special case. The resolution of
`.editorconfig` and `tsconfig.json` nests in the same way, and they do not
need to forbid it.
