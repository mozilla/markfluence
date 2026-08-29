# What markfluence guarantees

Properties markfluence holds itself to. They exist to be cited: a change that
would break one needs an argument, and a new feature that cannot satisfy one is
telling you something about the design rather than about the guarantee.

This is the counterpart to [confluence/](confluence/), which records what we know
about *Confluence*. These are claims about **markfluence**.

Each is deliberately about one thing, so it can be argued with on its own.

## How to read an entry

Every guarantee carries a status, for the same reason every entry in
[confluence/](confluence/) carries provenance — "we promise this" and "we intend
this" deserve different amounts of trust:

- **Holds** — true today, with the thing that enforces it named.
- **Partial** — true on some paths. The failing ones are named.
- **Aspirational** — not true yet. What would make it true is named.
- **Vacuous** — nothing exercises it yet, so it is untested rather than proven.

## Changing this document

**Identifiers are permanent.** Never renumber and never reuse. Each guarantee
also carries a **label** — `no-read-outside-root` — which is a mnemonic for
reading and citing: "S2 (no-read-outside-root)" says enough in a heading that a
sentence-long gloss is unnecessary. Labels are permanent too, for the same reason
ids are: they get cited, and a renamed label makes an old reference silently
wrong. The id is what is authoritative when the two ever disagree. A guarantee that
stops making sense is marked retired, with the reason, rather than deleted —
plans and pull requests cite these by id, and a recycled id makes an old
reference silently wrong.

**A change may not quietly downgrade a status.** Taking a guarantee from Holds to
Partial is a decision that belongs in the commit message and in this file, not a
side effect noticed later.

*Retired: "Nothing in Confluence is deleted." An implementation fact rather than
a principle — it would have gone false the day the first prune feature shipped.
Replaced by S4–S6, which survive that feature and constrain how it works.*

## Safety

A violation here does damage, rather than producing a wrong answer.

| | label | guarantee | status |
|---|---|---|---|
| **S1** | `no-write-outside-root` | No file is written outside the root. | Holds |
| **S2** | `no-read-outside-root` | No file is read outside the root. | Holds |
| **S3** | `no-overwrite-without-force` | No existing file is overwritten without `--force`. | Holds |
| **S4** | `no-removal-as-side-effect` | Nothing is removed as a side effect. Removal is a command's stated purpose or it does not happen. | Vacuous |
| **S5** | `remove-only-ours` | markfluence removes only what markfluence created. | Vacuous |
| **S6** | `removal-is-previewable` | A command that removes says what it will remove before doing it, and honours `--dry-run`. | Vacuous |

**S1** is enforced by `attachfile.Resolve`, which refuses a traversing path
rather than clipping it.

**S2** now holds for all three reads `_plans/025` names.

The image leaf is enforced through `root.FS`, an `os.Root` scoped to the
documentation root (`internal/convert/images.go`): a lexically escaping path is
refused before ever asking it, and an escape only `os.Root` can see — a
symlinked intermediate directory — is refused too, closing what `withinRoot`'s
purely lexical comparison used to miss. The same leaf also refuses a symlink
outright via `os.Lstat`, even one resolving inside the root.

Link and anchor resolution needs no clamp at all: `internal/linkindex.Build`
walks *down* from the root once, so nothing outside it can be in the index and
no file outside it is ever opened for this purpose — the guarantee holds by
construction rather than by a check, exactly as `_plans/025` describes. See
[Non-goals](#symlinks).

A frontmatter `parent:` path is read the same way the image leaf is
(`cmd/create.resolveParent`, through `root.FS`), but the failure mode differs
on purpose: an escaping or symlinked parent is a **hard error**, not an
unresolved-and-reported case the way a link is. A parent is load-bearing —
publishing under the wrong one, or silently under none, is worse than not
publishing at all (`_plans/026` commit 6).

**S3** is enforced by `export`, which stats the destination and skips both the
markdown and each attachment unless `--force`.

### Overwriting and removing are not the same risk

S3 covers overwriting and S4–S6 cover removal, because the risks have different
shapes.

Overwriting is **in scope** of the operation. `export` was told to write to that
path, so the exposure is one file at a path the user named, and consent is a
flag.

Removal is **out of scope**. Nothing in "publish this file" or "export this page"
implies deleting anything, so the exposure is unbounded in principle — which
things, and how many — and no consent gesture is defined for it. "Not without
`--force`" is the wrong shape for removal. The right shape is that it does not
happen unless removing is what was asked for.

### Why S4–S6 are written before anything removes

Nothing removes a local file (there is no `os.Remove` in the tree) and nothing
deletes in Confluence, so all three are vacuous and untested. They are written
anyway, because the alternative is a guarantee that expires, and because removal
is already visible on the horizon in two places:

- **Orphaned attachments.** An attachment's identity is derived from its path, so
  renaming an asset strands the old attachment. The README already tells people
  to remove those by hand.
- **`export --clean`.** Subtree export will want it, so that re-exporting does
  not leave pages deleted upstream lying around as stale files.

**S5 is the one with teeth, and the machinery exists.**
`client.AttachmentMeta.Managed` is true when an attachment carries the
`markfluence: ` comment prefix and false for a hand-uploaded one. Today it is
only reported, by `attachment-list`. It is what lets a prune remove stranded
markfluence attachments while never touching a file someone attached by hand.

## Laws

Algebraic properties of the three mappings markfluence performs — **Resolve** (a
markdown reference to a local file), **Name** (a local file to a Confluence
identity), and **Place** (a Confluence attachment to a local file). Each is
stated so a property test can generate trees and assert it.

| | label | guarantee | status |
|---|---|---|---|
| **L1** | `resolve-what-was-named` | A reference resolves to the file it names, or to nothing. | Holds |
| **L2** | `invocation-independent` | How a reference resolves, and what an attachment is named, depend only on the files on disk — not on the working directory, nor on which files were passed in the same command. | Holds |
| **L3** | `identity-from-asset-location` | An attachment's identity depends only on the asset's location. | Holds |
| **L4** | `publish-is-idempotent` | Publishing a file that has not changed makes no change in Confluence. | Holds |
| **L5** | `roundtrip-from-confluence` | Exporting a page, then publishing it back unedited, makes no change to the page. | Partial |
| **L6** | `roundtrip-from-disk` | Publishing a file, then exporting it, yields markdown that publishes to the same page. | Partial |
| **L7** | `output-is-valid-markdown` | Anything markfluence writes to disk is markdown that renders. | Holds |
| **L8** | `no-layout-inference` | Page identity and hierarchy are never inferred from disk layout. | Holds |

**L1** is about correctness, not cardinality. A basename lookup used to
resolve to exactly one file — just not the one the reference named, which was
how a link to `sub/dup.md` reached `./dup.md`. `internal/linkindex` resolves by
path instead, so a basename can no longer match the wrong file
(`_plans/026` commit 5).

**L2** is deliberately narrow. `--title` and `--page-width` change what gets
published and are meant to, so the law constrains resolution and naming only.
Within that scope it rules out a root derived from the working directory, and
equally one derived from the *set* of arguments — the same file would otherwise
be named differently depending on what else was in the batch. `internal/project`
finds the root by walking up from each file's own directory, independent of the
working directory and of what else is in the same command (`_plans/026`
commits 1–4).

**L3** is what makes moving a page free. Moving an *asset* still changes its
identity; buying that back would need content-addressed names, at the cost of
being able to reconstruct a tree on export. `images.go` records an attachment's
`Source` relative to the root rather than to the referencing page, so identity
follows the asset alone (`_plans/026` commit 4).

**L5** and **L6** are partial because both fail before they start for any layout
with an asset above the page, which export refuses (see S1).

**L7** is why a markdown destination is percent-encoded on the way out: an
unencoded space produces a file that no longer parses as a link.

**L8** is stated negatively on purpose. Identity does not come *only* from
frontmatter — `--page-id` overrides it — so the claim worth guaranteeing is that
neither identity nor hierarchy is ever derived from where a file sits on disk.
That is what forecloses inferring a parent from a directory.

### A corollary worth naming

**Two people publishing the same repository from different checkouts produce the
same attachment names and the same links.** This follows from L2 and L3 together
rather than standing on its own, and it is not worth satisfying separately. It is
named because it is the form a user recognises, and because it is the one that
visibly forbids recording a checkout's disk layout on the shared server copy.

## Conformance

| | label | guarantee | status |
|---|---|---|---|
| **C1** | `preview-compatible-resolution` | A reference resolves the way a Markdown preview resolves it, GitHub's included. | Holds |

Not an internal property: agreement with an external specification. It always
held for images, which resolve page-relative; links now resolve the same way
(root-relative internally, but composed from the referencing page's own
directory the same way a preview would) rather than by basename in one
directory (`_plans/026` commit 5).

Kept separate from L1 because this is the one that could in principle be traded
away — markfluence could choose its own resolution rules and document them — and
L1 could not.

## Reporting

Not invariants. Publishing a dead link may be acceptable; doing it **silently**
is not. The obligation is to communicate, which is why R1 can be false while
nothing is computing a wrong answer.

| | label | guarantee | status |
|---|---|---|---|
| **R1** | `report-unresolved-references` | Every reference markfluence could not resolve is reported. | Partial |
| **R2** | `report-unplaceable-attachments` | Every attachment markfluence could not place is reported. | Holds |

**R1** was false by design and documented as such: the README said an
unresolved link was "published as-is, which on Confluence is a dead relative
link. There is no warning for this." A same-tree `.md` link that doesn't
resolve now lands in the same `warnings` list an unresolved image already
used (`_plans/026` commit 5) — Partial rather than Holds because that's a
minimal warning reusing an existing mechanism, not the dedicated diagnostic
(distinguishing *why* a reference failed, auditing a tree without publishing)
`_plans/025` gestures at and leaves for later.

## Non-goals

Decisions about what markfluence will not do. They live here because they
constrain future work the way a guarantee does: a request that needs one reversed
is a design conversation, not a bug report.

### Symlinks

**markfluence does not follow symlinks.** Not ones that leave the project, and
not ones that stay inside it either — one rule, because two rules produced a
system where the same symlink worked for an image and failed for a link.

Three enforcement points, in decreasing order of how much work they take:

| where | mechanism | cost |
|---|---|---|
| the link and anchor index | `filepath.WalkDir`, which reports a symlinked directory and does not descend it | free |
| reading a leaf, such as an image | `os.Lstat` and refuse anything that is not a regular file | one call |
| an escape through an intermediate symlinked directory | `os.Root` scoped to the root, which refuses it | already the pattern in `internal/attachfile` |

**Verified 2026-08-28.** `WalkDir` from `docs/` over a tree containing
`docs/escape → ../outside`:

```
dir                      docs/
SYMLINK (not descended)  docs/escape       ← outside/out.md never enumerated
dir                      docs/sub/
file                     docs/sub/in.md
```

And `os.Root` scoped to `docs/`, for the escape case an `Lstat` on a leaf cannot
see:

| path | `os.Root` |
|---|---|
| no symlink | allowed |
| relative symlink staying inside the root | allowed |
| relative symlink escaping the root | refused — `path escapes from parent` |
| absolute symlink, any target | refused |

Two consequences worth having.

**Where the root came from stops mattering.** Paths are addressed relative to the
open root handle, so a tree reached through a symlinked checkout works — `/tmp` is
`/private/tmp` on macOS and home directories are frequently links, and none of
that needs special-casing.

**Sharing an asset directory by symlink is not supported**, deliberately. The
capability people reach for it for — one asset directory used by many pages — is
what recording attachment sources relative to the root provides directly. Git
symlinks are also not portable to Windows without `core.symlinks` and developer
mode, so a repository depending on them is not cloneable everywhere.

This is how **S2** stops being lexical. `convert.withinRoot` compares
`filepath.Abs` output with `filepath.Rel` and never resolves anything, so today a
symlinked directory inside the tree passes the clamp while its bytes come from
outside it.

## When a guarantee cannot be met

Best effort, and where best effort is unavailable, an error that names the
problem and a safe next step. Never a silent partial success.

This is a policy rather than a guarantee: it cannot be property-tested, and it
applies to all of the above rather than sitting beside them. It is also the
reason S1 refuses a traversing attachment path instead of clipping it — clipping
would write *something*, under a name nobody chose.

## How each kind is verified

| kind | verified by |
|---|---|
| Safety | adversarial tests: traversal attempts, pre-existing files |
| Laws | property tests: generate trees, assert the equation |
| Conformance | fixtures checked against what a Markdown preview renders |
| Reporting | example tests asserting a specific message appears |
| Policy | review judgement |
