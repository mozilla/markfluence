# The documentation root

How markfluence decides which directory bounds a markdown file's reads and
names its attachments. The model itself is `_plans/025_file-organization.md`;
this is a reader-facing explanation of what it settled, without re-deriving
the reasoning. `_plans/026_file-organization-implementation.md` is the
commit-by-commit implementation log, if you want to see exactly when a given
piece landed. For the practical "how do I—" version of this (moving a file,
sharing an asset across pages), see the README's
[Documentation root](../README.md#the-documentation-root) section.

## The root, and the other thing that looks like one

There are two different directory lookups in markfluence, and conflating them
was the source of most of the confusion this model exists to remove.

**The root** is discovered *per markdown file*: walk up from that file's own
directory looking for `markfluence.yaml`; the first ancestor that has one is
the root, and reaching the filesystem root with no hit means the file's own
directory is the root. This is the one that matters for correctness — it
bounds what a file may read (an image, a `parent:` reference), it's what an
attachment's recorded name and source are relative to, and it's what the
tree-wide link index is built from. When a project file exists and every file
in a batch sits under it, every file resolves to the same root — that's the
intended, ordinary case. It only fragments per file when there's no project
file at all, or when a batch happens to span more than one project (see
below).

**The `.env` lookup** is a separate, narrower pass: it starts from the
**working directory** — not a file's directory — and with no hit falls back
to the working directory itself. It exists solely to answer "where is
`.env`," runs once per invocation before any file is touched, and doesn't
bound anything. It is not called "root" anywhere in the code and it is not
reported. `--env-file` overrides it absolutely, unaffected by any of this.

Both passes walk up using the same primitive (`internal/project`'s
per-directory marker check), called with two different starting points and
two different "no hit" fallbacks. There is not a second algorithm — only a
second starting point. In the bare case (`project.Discover(cwd)`) that walk
is independent of anything else in the invocation. But `create`, `update`,
and `attachment-upload` each already build a `project.Cache` for their own
per-file root resolution, and hand that same cache to the client config
resolver (`client.ResolveOptions.Roots`) instead of leaving `.env` to make its own,
separate walk. Two consequences follow, for exactly those commands (one with
no per-file root concept, like `read` or `search`, never builds a cache to
share): `--root`'s override — which otherwise only redirects the per-file
root images/links/`parent:` resolve against — now redirects `.env` too, and
the walk itself is paid for once, not twice.

## `markfluence.yaml`: the project file

It marks the root, and it declares project-wide settings
([#100](https://github.com/mozilla/markfluence/issues/100)). A file with no
settings in it is perfectly normal — it is what `export` plants, and marking
the root was the file's only job until settings arrived:

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

The chain is **flag > frontmatter > project file**: the answer closest to the
content wins. This is *not* the credentials chain, and conflating the two is
the mistake the file's design forecloses — credentials resolve **flag >
environment > `.env`** and answer *who you are*, where a setting here answers
*what the content is*.

A project-wide setting is only ever read when both levels above it are silent,
so it never participates in a disagreement: `create` still refuses a `--space`
that contradicts a frontmatter `space:`, and a project default cannot become a
third party to that.

One consequence worth knowing before you add `page_width:`: `update` asserts a
width only when one is declared, and a project-wide declaration counts. A
project that wants each page's live width left alone as it is should leave the
key out.

Settings are per-root, so an invocation spanning two projects gets each
project's own defaults — see [Multi-root batches](#multi-root-batches-are-allowed).

### `pages:` — page metadata for a pristine file

A `pages:` block maps a path to that file's page metadata, so a markdown file
can be published while carrying no markfluence keys at all:

```yaml
space: ENG

pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
    labels: [runbook]
```

An entry **is a frontmatter block that lives elsewhere** — the same field names,
the same value domains, the same canonical order. Both locations are legal and
agreement is silent, which is what makes moving metadata into the manifest
something you can do a file at a time.

Where the two disagree, what happens depends on what the disagreement can
destroy:

| field | on disagreement |
|---|---|
| `page_id`, `space`, `parent` | **error** — the file fails. A `page_id` pasted from an old file would publish over a live page |
| `title`, `page_width`, `labels` | **warning**, and the frontmatter wins. Visible and recoverable |

`markfluence create` writes an entry here when this is where the file's
metadata belongs — a project that has chosen `pages:` never accidentally grows
frontmatter, and one without it behaves exactly as before. A file that already
carries its own frontmatter keeps it, so a half-migrated tree does not sprout
entries behind you. `--no-persist` records nothing anywhere.

One spelling to watch. A `parent:` that names a `.md` file is **relative to the
root inside an entry** (like every `pages:` key) but **relative to the file** in
frontmatter and on `create --parent` (matching how markdown links work). So the
same parent is `docs/index.md` in an entry and `index.md` from a sibling file.
markfluence translates between them, so nothing breaks — but if you move a
`parent:` value from one location to the other by hand, re-spell it. `create`
records the resolved page id rather than a path, so a round trip never hits
this.

Every command that takes a page accepts a pristine registered file, because they
all resolve the argument through one place (`internal/pageref`): `markfluence
info docs/deploy-runbook.md` works even though that file says nothing about
Confluence. The exception is `fix`, which can locate such a page but cannot yet
*write* to its entry, and says so rather than writing frontmatter instead.

A file **neither location mentions is skipped**, not failed: a repository
legitimately holds markdown that is not published, so `markfluence update
docs/**/*.md` does not go red because somebody added a draft. A file that *is*
registered but has no `page_id` fails — something claimed it and the page has
not been created yet.

**Path keys** are relative to the root, in slash form, and lexically cleaned
(`./docs/a.md` and `docs/a.md` are the same key). Two rules are load-time
errors, because either means the manifest's structure is wrong rather than one
entry being bad: a key that escapes the root, and two keys that normalize to one
path. Keys are compared exactly, with no case folding — on a case-insensitive
filesystem `Docs/a.md` opens the file but matches no `docs/a.md` key, so the
file reads as unmanaged and is skipped.

Resolution is lexical and never follows symlinks, for the same reason **L2**
requires of everything else here: a key whose meaning depended on how the
checkout was laid out would resolve differently on two machines.

### A file that cannot be understood stops the command

An unparseable file, or one holding a key markfluence does not recognise, is an
error naming what is wrong. It is specifically **not** treated as a valid root
marker: discovery does not walk on to an ancestor that happens to have a better
one, and does not fall back to the markdown file's own directory. The root
decides every attachment name and bounds every read, so a project file that
cannot be understood means the project's boundary is unknown, and guessing is
worse than stopping.

**Refusing an unrecognised key is the point, not a limitation.** A
`markfluence.yaml` written for a newer markfluence holds keys an older binary
would ignore, and ignoring a project-wide default means publishing with the
wrong space or the wrong width — silently, everywhere at once. It is also what
catches `spce: ENG`. So the file carries no schema version, and the error says
that an older binary is the likely cause.

`markfluence check` validates a project file offline, alongside the markdown
files you give it.

### What it deliberately does not hold

No `url`, `username`, or token. The reason is sharper than "those are
credentials": markfluence sends basic auth to whatever host the resolved URL
names, so a `url:` here would decide where `CONFLUENCE_TOKEN` is sent — and
this file is committed, shared, and walked up to from a subdirectory. One line
in a pull request would redirect a CI run's token to a host of the author's
choosing.

The asymmetry against the settings it does hold is the whole argument: a wrong
`space` publishes to the wrong place in your own instance, which is visible and
`fix` recovers it. A wrong `url` hands out the token, which is neither.

Committed and shared, unlike `.env`, which stays gitignored and personal. A
stray `.env` in an ancestor directory can hand a project credentials that
aren't its own — which is exactly why the root (and, by extension, where
`.env` was read from) is reported: visibility is the mitigation, not a
permission check. `markfluence` *reads* a project file but **executes** nothing
on account of its presence, and nothing in it can redirect a credential —
walking up and trusting what's found there is the shape of
[CVE-2022-24765](https://github.blog/2022-04-12-git-security-vulnerability-announced/)
(pre-fix git walking up for `.git` with no ownership check) and of the
`.git`-directory hook-execution CVEs that followed it (e.g. CVE-2024-32002),
and `.editorconfig`'s discovery model — walk up, nearest wins, no execution —
is the one this borrows rather than git's. If a hook system is ever added, its
own consent step has to be separate from this file's presence; that isn't
decided here.

**No `init` command generates this file.** Create it by hand; that's
[#5](https://github.com/mozilla/markfluence/issues/5).

## `--root`

A persistent flag overriding discovery for the whole invocation, with one
value applied uniformly to every file — not a per-file setting, since it's
meant to say "treat this directory as the root, full stop," including for a
tree that has no `markfluence.yaml` and never will (a checkout you don't
control, a generated snapshot).

## What the root bounds

**S1/S2** (`docs/guarantees.md`): no file is written, and — as of
`_plans/026` commit 6 — no file is read, outside the root. Three reads exist:

- An **image leaf**. Resolved relative to the root (so `../assets/logo.png`
  from a page one directory below the root is fine, and the same reference
  from a page at the root is not); a path that resolves outside the root is
  `IMAGE BROKEN`, and a symlink is refused outright even when it resolves
  inside the root (`os.Lstat`, not `os.Stat`).
- A frontmatter **`parent:` path**. Read through the same root, but the
  failure mode is different on purpose: an escaping or symlinked parent is a
  **hard error**, not a broken-and-reported image. A parent is load-bearing —
  publishing under the wrong one, or silently under none, is worse than not
  publishing at all.
- **Link and anchor resolution** needs no clamp at all. The index
  (`internal/linkindex`) is built by walking *down* from the root, so nothing
  outside it can ever be in the index, and a destination that would escape
  simply isn't found there — the guarantee holds by construction rather than
  by a check. See [Non-goals](guarantees.md#symlinks) for why the walk itself
  cannot be tricked by a symlinked ancestor either.

## Attachment identity

An image's recorded `Source` — what `read`/`export` use to put it back where it
came from, and the only record of its path there is — is relative to the root,
not to the page that references it (`_plans/026` commit 4). The attachment
*name* is the file's base name and carries none of this (`_plans/029`). Two
pages at different depths referencing the same file now record the same
source and get the same attachment; before, each recorded the reference as
written, and the same file had two identities in Confluence. This is L3
(`identity-from-asset-location`, `docs/guarantees.md`), and it's also why
moving a page's own images along with it now churns where it used to be
free — see the README's recipes for what that means in practice.

## Link resolution

`internal/linkindex.Build` walks the root's tree once, keying the page and
anchor maps by each file's path relative to the root rather than by bare
filename — so a link to `setup/overview.md` can't be satisfied by an unrelated
`overview.md` sharing that basename elsewhere in the tree (`_plans/025`
Scenario A). The index is built once per root and shared across every file
converted under it in the same command, which is also an ~80× performance
win at 400 files — rebuilding it per directory, per conversion, was an
accidental O(n²) (`_plans/025`'s measurement).

A link that would resolve outside the root — `../../../../etc/passwd.md` —
isn't refused; it's simply never in the index, so it resolves the same way
any other unresolved link does: left exactly as written, and (since
`_plans/026` commit 5) reported. That's the minimal form of **R1**
(`report-unresolved-references`): a same-tree `.md`-shaped link that doesn't
resolve lands in the same warnings list an unresolved image already used.
It's not the dedicated diagnostic `_plans/025` gestures at (auditing a whole
tree without publishing, distinguishing *why* a reference failed) — that's
still open work, tracked loosely against a future `check` command.

`create`'s reserve phase (`_plans/026` commit 8) is the other half of link
resolution: every file in a batch gets its Confluence id reserved — a
content-less stub — before any of them is converted, and each id is fed into
the shared index immediately (`Index.SetPage`), including under
`--no-persist`. That's what makes a link between two files in the same batch
resolve regardless of which direction it points, or whether the two link to
each other.

## Multi-root batches are allowed

A single invocation can span more than one project — nested
`markfluence.yaml` files, or files under entirely separate ones — and nothing
refuses this. Each file's root is discovered independently; a link across a
root boundary simply doesn't resolve (unresolved, not an error, same as any
other miss); a `parent:` escaping a file's own root is still a hard error even
when the target happens to be part of the same command under a *different*
root. This falls out of per-file discovery with no special-casing, the same
way `.editorconfig` and `tsconfig.json` resolution nest without needing to
forbid it.
