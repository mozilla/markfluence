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

Its existence is its whole meaning. Nothing in it is parsed or read; the
`.yaml` extension fixes the intended format for when a key is eventually
added (see [#100](https://github.com/mozilla/markfluence/issues/100)) without
that being a migration. It should carry a one-line comment saying what it
does, since a reader who finds it should be able to tell without already
knowing:

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
```

Committed and shared, unlike `.env`, which stays gitignored and personal. A
stray `.env` in an ancestor directory can hand a project credentials that
aren't its own — which is exactly why the root (and, by extension, where
`.env` was read from) is reported: visibility is the mitigation, not a
permission check. `markfluence` reads nothing from inside a project file and
executes nothing on account of its presence — walking up and trusting what's
found there is the shape of
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
