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
| **S7** | `no-partial-create` | A file that `create` fails to publish leaves no page behind. | Partial |

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

### S7 and the stub create leaves behind

**S7** is **Partial**, and the boundary is worth stating exactly, because the
gap is not the part that looks alarming.

`create` is three-phase: preflight validates every file, reserve creates a
content-less page for each and persists its `page_id`, publish converts and
fills each page in. Anything preflight rejects aborts the batch with nothing
created, and since #127 that includes **every defect the converter can find in
the files on disk** — preflight converts each file and keeps the error, so a
document the converter refuses (two assets wanting one attachment name, say)
never reaches the reserve phase. It used to, which is what made the guarantee
worth writing: the author was left with a content-less page, a `page_id` they
did not ask for, and a re-run that refused because a page was already at that
id.

Three residuals remain, and only the first is purely remote:

- **A server or network failure while publishing.** The stub stays, with its
  `page_id` already in the frontmatter, so a plain `markfluence update`
  finishes publishing it.
- **An attachment that cannot be read.** `client.SyncAttachments` opens every
  asset to checksum and upload it, which the converter never does — it only
  `Lstat`s. So an unreadable image (mode `000`, a file replaced between the two
  steps) fails in the publish phase, locally, after the stub exists. Deliberately
  not pre-flighted: it duplicates the read the upload makes anyway and races the
  filesystem, so the check can pass and the upload still fail.
- **A frontmatter file that cannot be written.** `reserveOne` calls
  `os.WriteFile` *after* `CreatePage`, so a read-only `.md` leaves a stub whose
  id is **not** persisted — the one case where `markfluence update` cannot pick
  the work up, since nothing on disk names the page. `failKeepingPage` keeps the
  id and URL in the result for exactly this reason: the run's own output is the
  only remaining trace.

The first is deliberate rather than unaddressed: `_plans/026` accepted it as the
price of reserving every id before converting anything, which is what stopped
link resolution depending on creation order. In all three a page exists that the
command reported as failed, so the guarantee does not hold as written.

Closing it would mean deleting the stub, and that is not a change this
guarantee can authorise on its own: it would make **S4** and **S5**
non-vacuous, and S4 says removal is a command's stated purpose or it does not
happen.

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
| **L9** | `declared-metadata-is-asserted` | A metadata field a file declares is made true of the page; a field it omits leaves the page alone. | Partial |

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

A project-wide `space:` or `page_width:` in `markfluence.yaml` (#100) sits
inside that scope rather than straining it, and in the direction L2 wants: the
value is declared in a committed file on disk, found by the same
working-directory-independent walk, so two people in different directories
resolve it identically. It is strictly better for L2 than the `--space` flag it
replaces, which is invocation state by definition. Status unchanged.

**L3** is what makes moving a page free. `images.go` records an attachment's
`Source` relative to the root rather than to the referencing page, so identity
follows the asset alone (`_plans/026` commit 4).

Both halves of the sentence that used to follow are now wrong, and it is worth
saying how. It read that moving an *asset* still changes its identity, and that
fixing that would need content-addressed names at the cost of being able to
reconstruct a tree on export. Neither survives `_plans/029`: an attachment is
named by its base name, so moving an asset within the tree keeps its identity
and restamps its recorded path, and reconstruction was never the name's job
anyway — the comment carries the path. What identity still follows is the
asset's *file name*, so renaming the file is what creates a new attachment.

**L5** and **L6** stay **Partial**, and #59 -- the issue this file said would
settle them -- is what established that they cannot be Holds as worded.

Multi-page export now exists, which is what the previous note said was missing:
provenance-based attachment placement, directory mirroring, and a tree whose
`parent:` paths let it publish into fresh pages (`_plans/029`). The attachment
half of the round-trip is repaired too, and by a different change than expected
-- an attachment is named by its base name, so positioning a page's images no
longer moves its attachments (`_plans/029` §"The thing 025 got wrong").

What does not hold is the wording: *"publishing it back unedited makes no change
to the page"*. Measured 2026-09-05 against a live page, exporting and
republishing changes the stored storage in two ways that have nothing to do with
content:

- Confluence's editor writes `<li><p>text</p></li>`; the converter emits
  `<li>text</li>`.
- A TOC macro carries `ac:local-id`, `ac:macro-id` and `data-layout`
  attributes; the converter's canonical form omits them.

Both render identically and neither loses anything, which is the converter's
stated design target -- *semantic* rather than byte-for-byte equivalence. So the
guarantee as written asks for something markfluence deliberately does not do,
and the honest reading is that L5 and L6 are the wrong shape rather than unmet.
Rewording them is a decision in its own right and is not taken here.

What *is* now verified, by a property test rather than an assertion
(`TestRoundTripMarkdownIsAFixedPoint`, over every `storage2md` case rather than
a hand-kept list): **markdown is a fixed point.** Export a page, publish that
markdown back, export again, and the markdown is identical. Once a page has been
through markfluence it stops moving. That test found real drift on its first run
-- a hard break gained a leading space on every cycle -- which is the argument
for having it, and answers the note this file added when #125 showed L5 had no
property test at all.

Two exceptions to the fixed point are expected, and both converge on the second
cycle. A Confluence-native attachment is unmanaged, so the first republish
restamps its comment. And a mention written by Confluence's editor carries an
`ri:local-id` that markfluence does not emit (#91), so the first republish drops
it — verified harmless, since a mention carrying only the account id resolves to
the same person ([links-and-anchors.md](confluence/links-and-anchors.md)).

**L5/L6 narrow with #91 rather than closing.** A mention was the single most
common thing an `<ac:link>` could be — 80% of all usage — and it used to survive
the round trip only as raw storage. It now converts in both directions, so the
*readable* half of the round trip covers the case that dominates real pages. The
laws stay **Partial** for the reasons already given above: the wording asks for
byte-for-byte equivalence, which the converter deliberately does not target.

**L9** is what makes a frontmatter field safe to add. Without it, every new
field is a choice between two bad defaults: assert it always, and a page
configured by hand is silently reverted by a run that never mentioned the
field; assert it never, and the field cannot be used to manage anything.
Declaring it is the signal.

It is **Partial**, and the gap is `page_width` rather than `labels`. `update`
honours the law exactly -- a `page_width` line is asserted, an absent one leaves
the live width alone -- but `create` asserts a *default* width of `max` for a
file that declares none, so an omitted field is not left alone there. `labels`
holds in both verbs: absent means no label request is made at all, which is
stronger than "no write" and is pinned by a test in `cmd/update`.

`fix` is deliberately outside the law rather than a violation of it. It
reconciles the *file* to the page, so the page is the authority and an absent
field gets filled in -- which is the only way to adopt a page somebody labeled
or resized by hand. The law constrains the direction that writes to Confluence.

## Conformance

| | label | guarantee | status |
|---|---|---|---|
| **C1** | `preview-compatible-resolution` | A reference resolves the way a Markdown preview resolves it, GitHub's included. | Holds |
| **C2** | `frontmatter-is-valid-yaml` | Frontmatter markfluence writes parses as YAML, and reads back as the values it wrote. A value is a single-line scalar, or a sequence (in either YAML style) whose every element is one. | Holds |

Not an internal property: agreement with an external specification. It always
held for images, which resolve page-relative; links now resolve the same way
(root-relative internally, but composed from the referencing page's own
directory the same way a preview would) rather than by basename in one
directory (`_plans/026` commit 5).

Kept separate from L1 because this is the one that could in principle be traded
away — markfluence could choose its own resolution rules and document them — and
L1 could not.

**C2** was false until #130, and false in a way only an outside tool could see.
`internal/frontmatter` was a hand-rolled flat-key parser that split each line at
the first `:`, so it read back its own `title: Deploy Runbook: Part 2`
perfectly while every real YAML parser rejected it. The colon was one member of
a class: booleans, numbers, nulls, flow collections and the reserved indicators
were all written bare and read back as the wrong type or not at all.

It holds now because the block is parsed and emitted by `goccy/go-yaml`, and
because the writer **verifies its own output** rather than trusting it: it emits
with goccy's chosen style, re-reads the result, and falls back to a
double-quoted scalar when the two disagree. That fallback is load-bearing, not
belt-and-braces — goccy drops a tab, emits a value beginning `? ` as a document
it then refuses to parse, and writes `.inf`/`.nan` bare, where any conforming
reader sees a float. A hand-written predicate listing those shapes would be
incomplete, since they turned up only by probing; checking beats predicting, and
the check keeps holding if goccy regresses.

The check compares the re-read **node kind** as well as its text, which is what
makes the `.inf` case work. An earlier version compared text alone and passed:
the reader flattens every scalar to its token, so markfluence read `.inf`
back as `.inf` and the round-trip looked clean while the file said "float" to
everyone else. Comparing text is comparing the wrong thing.

The single-line contract is enforced on **read**, not assumed: a scalar whose
source spans more than one line is refused. It has to be, because an untouched
key is re-emitted from the node the parser produced and goccy's re-emission of a
parsed node is not identity. Measured against the pinned goccy: a `|` or `>`
block re-emits as a block, which the reader's node-kind whitelist then refuses,
so a write would produce a file markfluence cannot read — in `create` only after
the page had been made. A continued plain scalar and a multi-line quoted one are
milder: both re-emit folded onto one line, which parses but silently rewrites
the author's file. Neither is a thing to do on the way past while setting an
unrelated field.

Sequences extended this without weakening it. A value may now be a list, in
either YAML style, and the rule became "a *sequence* may span lines; every
element must be a single-line scalar" — so a block list and a flow list wrapped
across lines are both fine and an element continued onto the next line is not.
The element check trims its origin at both ends, because a leading newline in an
element's origin is structure (the item began on a new line) rather than
content.

The write-side self-check had to grow a second context, and finding out why is
the closest thing to a repeat of #130. The scalar check verifies a value as a
*mapping* value, and two shapes pass it and then corrupt a list: emitted bare
into a flow sequence, `x,y` becomes two elements and `has]bracket` ends the
sequence outright. Block style has its own, different trap — `? q` is YAML's
explicit-key indicator there and parses as a mapping. So verification runs in
the style about to be written. Flow is the stricter context, which means
verifying there is always *safe*; the style parameter buys minimal quoting, and
what it protects against is a block-context check standing in for a flow write.

markfluence now emits both styles, because a rewrite keeps the style it found: a
set large enough to be written as a block list is exactly the set whose flow
spelling is an unreadable single line. Style is preserved, formatting is not —
block items are re-emitted at this package's own indent rather than the
author's, since valid YAML in the right style is the guarantee and
byte-preservation is not.

One consequence worth naming, because a comment used to justify itself with the
old rule: `Normalize` drops blank lines with a **textual** filter, which was
safe "because nothing this package emits spans more than one line". A
passed-through block list makes that false. The accurate statement is that no
value markfluence can emit carries a *meaningful* blank line — a blank line
between two list items is inert in YAML, and the shape where one is content is a
`|` block, which is refused on read.

Two limits, stated rather than papered over. Quoting is goccy's, but **typing is
ours**: `page_id` and `parent` are written as YAML integers and nulls, because
`page_id: "123"` is valid YAML that says the wrong thing. That is a hand-rolled
rule, confined to two keys whose value domains are closed. And the verification
is a *self*-check — it proves goccy can re-read what goccy wrote, not that
another implementation can. There is no second YAML library in `go.mod` to
settle that, deliberately; a divergence reported by a real tool is an issue to
fix, and the failing frontmatter is the evidence.

Kept separate from **L7** (`output-is-valid-markdown`) because they are checked
against different external specifications. A file with broken frontmatter still
renders as markdown — GitHub shows it, and it was VSCode's YAML extension that
complained — so folding this into L7 would leave its status ambiguous about
which spec had failed.

## Reporting

Not invariants. Publishing a dead link may be acceptable; doing it **silently**
is not. The obligation is to communicate, which is why R1 can be false while
nothing is computing a wrong answer.

| | label | guarantee | status |
|---|---|---|---|
| **R1** | `report-unresolved-references` | Every reference markfluence could not resolve is reported. | Holds |
| **R2** | `report-unplaceable-attachments` | Every attachment markfluence could not name or place is reported. | Holds |

**R1** was false by design and documented as such: the README said an
unresolved link was "published as-is, which on Confluence is a dead relative
link. There is no warning for this." A same-tree `.md` link that doesn't
resolve first landed in the same `warnings` list an unresolved image already
used (`_plans/026` commit 5) — Partial rather than Holds, because that was a
minimal warning reusing an existing mechanism rather than the dedicated
diagnostic (distinguishing *why* a reference failed, auditing a tree without
publishing) `_plans/025` gestured at and left for later.

That dedicated diagnostic now exists (#42): a doc-link target that's missing
entirely or resolves outside the documentation root is Broken and replaces
the published element, exactly as a broken image already does; one that
exists but has no `page_id` yet, or whose `#fragment` matches no heading,
warns instead of publishing silently. Every message carries the source line
it came from when one is findable — a link/image with no visible text at all
has no `*ast.Text` to walk to, and reports the message unprefixed rather than
a wrong line (`nodeLine`'s documented `ok=false` case). `check` adds the
"auditing a tree without publishing" half — the same diagnostics without
ever touching Confluence or the filesystem outside reading.

R1 is scoped to the two reference kinds markfluence actually attempts to
resolve: doc-links (`.md` siblings) and images. A relative link to a local
non-`.md`, non-image file (e.g. a PDF) gets no existence check at all,
before or after #42 — `rewriteDocLink` only ever attempts resolution for
hrefs ending in `.md`, so that case is never a resolution attempt in the
first place, by design, since only images are uploaded and a relative href
to anything else would be dead regardless. That sits outside R1's claim
rather than inside it unmet, so it does not block Holds.

**R2** covers naming as well as placement, which is a widening of the note and
not of the label — labels are permanent (see *Changing this document*), so
`report-unplaceable-attachments` stays as it is even though the guarantee now
reaches one step earlier in the pipeline.

The step is new. Since `_plans/029` an attachment is named by its base name, so
two assets in one document can want one name — and there is no correct way to
publish that, since a name is unique per page. The converter refuses the file
and names both paths and both lines; `attachment-upload` refuses the same
collision across a batch; `check` reports it offline as a Broken entry, in the
same list as a dead link, because renaming a file is the fix in either case.
An attachment that cannot be named is as unusable as one that cannot be placed,
and the obligation to say so is the same one.

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
| Safety | adversarial tests: traversal attempts, pre-existing files, a failing preflight asserted to have created nothing |
| Laws | property tests: generate trees, assert the equation |
| Conformance | C1: fixtures checked against what a Markdown preview renders. C2: the writer verifies its own output at runtime, plus a round-trip test and fuzz target; agreement with *other* YAML implementations is review judgement |
| Reporting | example tests asserting a specific message appears |
| Policy | review judgement |
