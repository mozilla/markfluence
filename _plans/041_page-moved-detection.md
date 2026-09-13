# 041 — `update` refuses to overwrite a page that moved

Closes #149.

`update` overwrites a page that has moved on since the local copy was made, with
no warning and nothing in `--json` a consumer could branch on. The mtime skip
looks like it protects against this and does not.

The diagnosis is in the issue and is not repeated here. The one sentence worth
restating, because everything below follows from it: **telling "I changed it"
from "they changed it" needs a merge base — what *this copy* was derived from —
which is a per-copy fact no page-side state can hold.**

This plan records what changed about the design since the issue was written,
then the design as it will be built.

## `markfluence update --force` means "always PUT"

**`update --force` means "always PUT". No logic may cause markfluence not to
send the request.** This affects two scenarios:

1. Updating content where the source of truth is external to Confluence. For
   example, updating content via CI from a GitHub repository. git state tells you
   which files changed and you update those files regardless of what's in
   Confluence using `markfluence update --force`.
2. A user wants to force push an update to Confluence because they know the
   full situation or as an escape hatch.

In these cases, `markfluence update --force` should be "always PUT" and that
shouldn't be affected by *divergence* check or *idempotence* check.

This plan ensures that `markfluence update --force` means "always PUT".

## Measured: the live body in Confluence can never byte-match what was sent

`docs/confluence/storage-format.md` already records that macros gain a
server-generated `ac:macro-id` on write. Re-measured 2026-09-13 against a
scratch page in the personal space, because the HTML-comment finding had made
comments look like the only obstacle:

```diff
- <ac:structured-macro ac:name="code" ac:schema-version="1">
+ <ac:structured-macro ac:name="code" ac:schema-version="1" ac:macro-id="36f410f4-…">
```

A fresh uuid, per macro, per write. **A code block is a macro**, and so is the
TOC, so a sent-vs-stored comparison fails for most real pages rather than for
the comment edge case. Making it work needs comments *and* whitespace collapse
*and* `ac:macro-id`/`ac:local-id` *and* self-closing-tag spacing — and a case it
misses fails toward "differs forever", which silently republishes everything.

So the comparison is out of this plan entirely. (It remains a live defect
elsewhere: see [`updateLanded`](#a-separate-pre-existing-bug) below.)

## The design

One append-only JSONL log at the project root, recording what each publish was
derived from. Two fields of the last entry for a file answer two orthogonal
questions, and both checks run only when `--force` is absent:

| field compared against | question | outcome |
|---|---|---|
| logged `page_version` vs. the live version in Confluence | did the Confluence page move past my base? | **refuse** the publish |
| logged `publish_sha256` vs. this run's title and render | would publishing change anything? | **skip** the body PUT |

Neither comparison touches a clock, an mtime, or the live page body.

### One change from the issue: the sha is of the render, not the source

#149 records a sha of the raw source file, and accepts a wart for it: a file
whose source is unchanged but whose *published* output would differ is skipped.

The sha's only job is to decide whether to send the body `PUT`, so the scenarios
worth tabulating are the ones where something changes what that request would
carry — and, just as important, the ones where nothing does, because those are
where a source sha and a render sha are both right and where the earlier drafts
of this plan over-claimed.

| | scenario | what has to happen | source-file sha | render sha |
|---|---|---|---|---|
| 1 | the markdown body is edited | body `PUT` | ✓ | ✓ |
| 2 | **`title:` edited in the file's own frontmatter** | body `PUT` — the title travels with it | ✓ | **✗** |
| 3 | **`title:` edited in the file's `pages:` entry** | body `PUT` | **✗** | **✗** |
| 4 | a sibling gains a `page_id`, so a doc link in this file is rewritten | body `PUT` | **✗** | ✓ |
| 5 | the converter changes between markfluence versions | body `PUT` | **✗** | ✓ |
| 6 | **F2** — clone, pull, checkout, `touch`; nothing actually changed | nothing | ✓ | ✓ |
| 7 | `page_width` or `labels` change, in either location | their own passes | N/A | N/A |
| 8 | `architecture.png` is redrawn | the attachment pass | N/A | N/A |
| 9 | the file's `page_id` is changed to name a **different page** | body `PUT` | N/A | N/A |

Scenarios 4 and 5 are the crux of why we need to take a sha of the rendered
content rather than the source file.

Scenario 6 is #149's own scenario id, and is the one the mtime check gets wrong
and *both* shas get right — it is the reason for having a content hash at all.

Scenarios 7 and 8 get updated if they're different regardless of whether the
body content is updated.

Scenario 9 is N/A for a different reason than 7 and 8: there the sha is
irrelevant because the body is not involved, here the whole base is discarded
before anything looks at it. The base records a `page_id` as well as a version,
so once the file names page 456 the entry for page 123 describes a different
page — comparing its v44 against 456's v7 would report the page moving
backwards 37 versions. #149 calls this the one case that produces a *wrong*
answer rather than no answer, which is why it is a row here and a row in the
no-base table below.

### What's part of the sha

**The rule: the sha covers exactly what the body `PUT` would send.** That
request is what the sha gates, so anything it carries belongs in the hash and
anything it does not carry must stay out, or the sha fires for a change it
cannot publish.

`client.UpdatePage(pageID, title, html, next, message)` carries two things that
describe the page:

| | | |
|---|---|---|
| **the resolved title** | frontmatter, or the file's `pages:` entry, or the live page's title as a fallback | rows 2 and 3 |
| **the rendered body** | `convert.ConfluencePage.HTML` — the storage format the `PUT` sets as `body.storage.value` | rows 1, 4 and 5 |

Rows 2 and 3 are the hole this found. `convert.ConfluencePage` carries `HTML`,
`Attachments`, `Broken`, `Warnings` and `Mentions` — **no title**, because
`update` passes it separately. A render-only sha would therefore skip the body
`PUT` for a file whose title changed, and the title would never publish. Hashing
both is what makes the field `publish_sha256` rather than `render_sha256`: a
name saying "render" while hashing the title too is a small lie in a persisted
format.

It also closes row 3, the one case a complete-source sha won on, so
title-plus-render dominates both alternatives rather than trading against them.

**The cost, stated plainly:** the skip decision now needs the render, so it
happens *after* `convert.MdToConfluence` rather than before. `update`'s comment
at the mtime check currently notes that building the link index sits below it so
"a file that is skipped never pays for one". That stops being true for a file
that is skipped as unchanged — though not for the two cases that still return
early, an unmanaged file and a refused one. The index is cached per root
(`linkindex.Cache`), so the cost is one index per batch rather than one per
file, and a render is local CPU against files already read. The comment gets
corrected rather than the ordering preserved.

A **complete-source** sha was the near miss: the file's bytes plus the resolved
metadata, which covers rows 2 and 3 and is cheaper, since it needs no render.
It loses rows 4 and 5, and the converter case is the one that decided it — with
a publish sha, fixing a bug in `tables.go` republishes exactly the pages with
tables, by itself. Its other advantage, that `export` could compute one during
its walk where a render sha cannot, turned out to be answerable another way: see
the second pass below.

### What's not part of the sha

Everything below is left out because the body `PUT` does not carry it. Each has
its own pass that runs whether or not the body is republished, or is not page
content at all — so folding any of it in would bump the page version, notify
every watcher and add a history entry for a change that never touched the body.

| | why not | who handles it |
|---|---|---|
| **`page_width`** | two content properties, and neither write bumps the page version (measured) | `pagewidth.Apply`, every run where a width is declared |
| **`labels`** | a v1 label call per add/remove, neither of which bumps the version | `labels.Apply`, every run where the key is declared |
| **an attachment's bytes** | the body names an attachment, it does not embed it, so re-uploading is the entire fix and Confluence re-renders from the new file | `SyncAttachments`, by SHA-256 against the attachment's own comment |
| **an attachment's recorded path** | moving an asset between directories keeps its base name, so the body is unchanged | `SyncAttachments` again — a recorded `path=` disagreeing with the local source is an update even when the checksum matches |
| **the version `message`** | version metadata, not content. Including it would make `--message "typo fix"` republish the whole tree | nothing; it rides along on whatever `PUT`s do happen |
| **`page_id`** | a coordinate. It is recorded as its own field in the line, not mixed into the hash, so a retarget discards the base rather than changing a sha (row 9) | the id-mismatch rule below |
| **`space` and `parent`** | coordinates, and `update` does not assert them at all today (#10) | nobody yet |
| **`Broken`, `Warnings`, `Mentions`** | diagnostics the `PUT` never sends. A `LINK BROKEN:` message is already *inside* `HTML`, since it replaces the `<a>` element, so the published half is covered and the reported half is not page state | reported per run |
| **the source file's bytes, its mtime, any clock** | the whole point. See the scenario table |

The attachment rows are the ones worth being sure about, because an image is the
case where "surely the page changed" is most tempting. Every way the attachment
set can affect the *body* already moves the render on its own: a new reference
adds an `ri:filename`, a removed one drops it, a rename changes the base name.
What is left is pixels and paths, and those are exactly what `SyncAttachments`
is content-addressed over.

So the two halves stay separate, each hashed over what it governs — the publish
sha gates the body `PUT`, the attachment checksums gate the uploads. That is a
better property than one combined hash, which could only ever fire both. What it
requires is the ordering fix below.

### The log

| | |
|---|---|
| path | `<root>/.markfluence/log.jsonl` |
| format | one JSON object per line, appended |
| committed | no |

A line:

```jsonl
{"time":"2026-09-13T07:30:00Z","action":"update","status":"ok","file":"docs/some-page.md","page_id":"123456789","page_version":44,"publish_sha256":"b800cc4f…","markfluence":"1.2.3"}
```

- **`file`** is the same root-relative lexical key `pages:` uses, via
  `pagemeta.KeyFor` — the one place a file's path becomes a manifest key, used
  here for the same reason: a key whose meaning depends on the checkout's
  layout is what **L2** forbids. A file with no key (outside the root) gets no
  line, matching #139.
- **`time`** and **`markfluence`** are provenance. **Nothing compares `time`**,
  which is the point of the whole exercise; it is there because "which build
  published this, and when" is the first question on any surprising page.
- **`status`** is `ok` or `failed`. A failure logs so the history is complete
  for debugging; the base reader ignores any line that is not `ok`.

**A directory rather than a bare file**, with `.markfluence/.gitignore`
containing `*`, planted alongside the log the first time one is written. One
ignore entry covers every future piece of local state, compaction can rename
within the directory, and the git-specific knowledge is confined to a file whose
absence breaks nothing. `export` plants `markfluence.yaml`; it plants this too.

**No root, no log.** `project.Root.File == ""` means `Discover` found no
`markfluence.yaml` walking up from the file's own directory. Nothing is read,
nothing is created, and the file falls into the *when there is no base*
semantics below: publish, silently, exactly as today.

This is reachable rather than hypothetical. `update` requires a file to be
managed, which means a `page_id` or a `pages:` entry — an entry implies a
project file but a frontmatter `page_id` does not, so a lone `notes.md` carrying
`page_id: 123` with no marker above it publishes fine and never has a base.

Planting one anyway would be worse than the gap. In that state `Root.Dir` is a
*fallback* — the file's own directory, not a root anybody declared — so a
`.markfluence/` would land per directory across a tree, each log keyed by a bare
filename because every key is relative to its own directory. And #139 already
settled the general question: a root with no project file refuses rather than
creating one, because whether markfluence may create a project file is #5's.

So the remedy is one empty `markfluence.yaml`, the same gesture that turns on
`pages:` and root-relative attachment naming.

**Multi-root batches write to several logs.** A batch may legitimately span
roots (`docs/root-model.md`), and each file's line goes to its own root's log,
resolved by the walk that already ran for it. This is not a new decision; it
falls out of the key being root-relative.

**Appends are unsynchronized and that is deliberate.** One `O_APPEND` write per
line, no lock — the same optimistic posture as `project.SetPageEntry` and
`client.SetContentProperty`, and for the same reason: a lock file brings
stale-lock handling to a verb a person invokes by hand. A torn line is a
tolerated read failure, below.

### Which commands write a line

| | commands | |
|---|---|---|
| **Establishes a base** | `create`, `update`, `export` | Each pairs a publish sha with a page version. `export` matters as much as the other two: without it an export → edit → update flow has no base for its first publish, which is arrangement 2's whole flow. It needs a second pass to do it (see below). |
| **No line** | everything else | Read-only, or no local-file/page pairing. `read` prints to stdout, so markfluence does not know where the bytes went — which is exactly why it cannot record a base the way `export` can. |

`fix` was removed in (#151) so is no longer relevant.

`attachment-upload`/`attachment-download` are #149's "history only" rows. They
are **deferred**: an attachment upload does not bump the page version
(measured), so it cannot invalidate a base, and writing lines nothing reads is
machinery for #148 to ask for when it exists.

The rules that go with it:

- **One line per page, appended as that page completes** — not one per run. #139
  D10's reasoning: a run that dies partway must leave every finished page
  recorded.
- **A body-unchanged skip writes a line too.** This is load-bearing rather than
  tidy, and #149 does not have it: once the sha does the skipping, most runs
  skip, so a publish-only log would never accrue a base in a tree that is
  already published. A skip is a perfectly good observation of the base — the
  render matches the page at version N — so it records one.
- **A page `export` skipped records no line.** `export` skips a page whose file
  already exists (**S3**), and that file is somebody's — possibly edited. A base
  asserting "this copy was derived from v44" is a claim export has no grounds
  for, and it would silence divergence detection for exactly the file most
  likely to need it.
- **`--dry-run` writes nothing.** A preview that logged would claim a publish
  happened.

### `export` computes its shas in a second pass

A sha taken *during* the walk would be wrong: a file's render depends on the
link index over the whole tree, and its siblings are not on disk yet. Once the
walk finishes they are, so a post-pass builds the index once and renders each
file it wrote, recording the sha exactly as `update` would.

It is self-consistent in the way that matters: the pass hashes **our own
render**, not a comparison against the page, so it does not depend on round-trip
fidelity — **L5**/**L6** being Partial is irrelevant here, because the value
recorded is precisely the one a later `update` recomputes from the same file. A
file the converter refuses (a `NameCollisionError`) simply gets no sha, and the
degrade-per-field rule covers it.

The cost is one index build plus one conversion per exported page, local CPU
against files just written, in a command dominated by page GETs and attachment
downloads. It is not extra work in total: it is the first `update`'s work, moved
earlier. Leaving it out would cost one spurious version per exported page on
that first `update` — identical content, a version bump, a watcher notification
each — which is tolerable for arrangement 2's one-page flow and is exactly what
`docs/github-actions.md` warns about for a tree.

### The check, in order

Inside `processFile`, after `GetPageOrNil` and before the attachment pass:

1. **`--force` short-circuits both checks.** Always PUT.
2. Read this root's base for the file — the last `ok` line naming it. Read once
   per root and cached, not once per file.
3. **Divergence:** base known and `live.Version.Number != base.page_version` →
   fail the file. It needs no render, so it comes first and a refused file pays
   for nothing. One comparison, and deliberately *not* qualified by the sha: a
   page that moved must not be overwritten whether or not I also have local
   edits, and the case where I have none is the worse one — publishing would
   replace their work with the very bytes they started from.
4. Build the index, render, take `sha256` over the resolved title and `pageContent.HTML`.
5. **Idempotence:** base known and this run's sha equals `base.publish_sha256` →
   skip the body PUT, and **continue** to width, labels and attachments.
6. Publish, then append the line.

`GetPageOrNil` stays as it is. Nothing here needs the live body, so no
`body-format=storage` fetch is added.

### The related bug the ordering fixes

Today's skip returns at `cmd/update/update.go:279-287`, **before** the width,
label and attachment passes, so a skipped file gets none of them. One timestamp
on the `.md` decides the fate of the body *and* of everything else the file
governs.

That is live today, and it bites twice — the second the worse:

- A file whose only change is a new `labels:` line is silently ignored whenever
  its mtime happens to be older than the page's last version.
- **An edited image is never uploaded.** Redraw `assets/diagram.png` without
  touching the markdown and the `.md`'s mtime does not move, so the whole file
  skips and `SyncAttachments` never runs. The page keeps serving the old
  diagram, which reads as correct.

Step 5 above is the fix: it skips the body `PUT` alone and the run continues to
attachments, width and labels.

### The mtime check is deleted, with no fallback

**`cmd/update/update.go:279-287` comes out as part of this work**, and nothing
replaces it for a file with no base.

#149 leaves that open, and it resolves against keeping it. Its failure modes
are precisely the ones this issue exists to fix, so keeping it as a no-base
fallback would preserve every one of them for exactly the population that has
no base — which at first is everyone. The narrower version, letting mtime
*skip* but never publish, bounds the damage to P3 and is still wrong for the
same reason: it silently withholds a publish on the strength of two
unsynchronized clocks.

Dropping it costs one noisy run. A tree of 200 pages with no log republishes
all 200, then has a base for each and behaves from then on. That is tolerable,
and F2 already means a fresh checkout republishes everything anyway — so for
the case people actually hit, this changes nothing except that it stops
happening a second time.

It also means the two checks are the only thing standing between `update` and a
`PUT`, which is the property worth having: every skip is now explained by a
recorded base rather than by a timestamp comparison that could go either way.

### When there is no base

**An unknown base means publish, silently, exactly as today.** Unknown must
never read as "changed" — a warning on every file would be scrolled past within
a day, and the real one with it — and never as "unchanged", which would skip a
file that needs publishing.

None of this is today's behaviour — there is no log today, so "no base" is not
a state that exists. The table is every way a base can be **absent or
unusable** once there is one, and what `update` will do about each. **Unknown
base** resolves to the outcome in the paragraph above: publish, silently. The
last two rows are the ones that do *not* reach it.

| situation | what `update` will do |
|---|---|
| **no project root** — no `markfluence.yaml` above the file | no log is read and none is created → **unknown base** → publish|
| **the file has no manifest key** — `pagemeta.KeyFor` reports none. Defensive: `update` resolves the root by walking up from the file's own directory, so the file is always under `root.Dir` and `Rel` always succeeds; `KeyFor` fails only for a caller consulting a *different* root, which `update` never does | nothing was ever written for it and nothing can be looked up → **unknown base** → publish |
| **no log file**, or no line at all naming this file | **unknown base** → publish|
| **lines for this file, but none a success** — every run for it failed | the base reader ignores any line whose `status` is not `ok`, so a history of failures is history and not a base → **unknown base** → publish|
| **the log cannot be read** — permissions, a read-only checkout | **unknown base**, plus one warning: this is the only row where something is wrong with the machinery rather than merely absent → publish|
| **the entry names a different `page_id`** than the file resolves to now | **discard the entry** — the base describes another page, so comparing its version would invent a "the page moved 40 versions" warning out of a retarget (scenario 9) → **unknown base** → publish|
| **a malformed line** — truncated JSON from an interrupted append | skip that line and keep reading; the base is whatever the most recent intact `ok` line says, and only a file with no such line falls through → **usable base** → the two checks decide. Report the skipped line under `--debug` |
| **the entry has a version but no sha, or the reverse** — an `export` line before its second pass, or a partial write | **degrade per field, not per line** — the two comparisons are independent, so a version alone still refuses a moved page and a sha alone still skips an unchanged one → **partial base** → the check it can still make decides |

The last two rows are the only ones that do not short-circuit to publish: they
hand a base — whole or partial — to the normal path, and the divergence and
idempotence checks decide from there, so the file may be refused, skipped or
published exactly as one with a clean base would be.

### Saying so: one line per run, not one per file

An unknown base means the check the author asked for by *not* passing `--force`
did not run, so saying nothing is not defensible. Per file is wrong too: right
after this lands it fires on 200 of 200 files and says the same thing 200
times, carrying no per-file information at all.

So **one summary line per run**, on stderr, only when the count is nonzero:

```
warning: no publish base for 200 of 200 files; published without the moved-page check
```

It extinguishes itself — the next run says 0 of 200 and prints nothing — and in
steady state a "3 of 200" is genuinely worth reading, since those three came
from another machine or were compacted out. It is suppressed under `--force`,
where no check ran and there is no gap to report, and absent under `--json`,
where per-file `base: null` already says it better.

The earlier draft argued for silence on the grounds that a warning here would
drown the real one. Half of that is wrong and worth correcting rather than
quietly dropping: **divergence is a failure, exit 1, not a warning**, so warning
volume cannot bury it. What is actually wrong with a per-file warning is only
that it is uninformative.

**Two rows do not belong in that count, because they never self-heal.** No
project root and an unreadable log are not waiting for a publish to fix them —
no publish will ever record a base — so each earns its own warning naming the
remedy, an empty `markfluence.yaml` or the file's mode. The other four rows are
transient by construction: publishing records a base and the line stops.

**Nothing about the log ever fails a command**, which is the rule behind every
row above. It is advisory bookkeeping, so a missing, unreadable, corrupt or
half-written one degrades the check and never the run. The only non-zero exit
anywhere in this design is the divergence refusal, and that is a decision about
the *page*.

Not in the table, because it is not a missing base: **the log cannot be
written** after a successful publish. Both checks have already run by then, and
the page is published, so failing the result would report a false failure —
warn instead, the non-fatal shape `pagewidth.Apply` and `labels.Apply` already
have. Its cost lands on the *next* run, below.

Once compaction exists it adds one more way to lose an entry, which is the
same row as "no line naming this file" and needs no separate answer.

Two consequences to state in the docs rather than let people discover:

**Protection accrues; it is never migrated.** A file becomes protected the first
time `create`, `update` or `export` records a base, so there is no init step and
no adopt command. But **the first `update` after this lands is unprotected for
every existing file**, because "I upgraded and it still overwrote my colleague's
edit" is the predictable report.

**A failed log write costs the next run a false refusal.** The logged version
then trails the live one by my own unlogged publish, so the next `update`
refuses. `--force` is the remedy, and refusing is the safe direction, but it is
a real cost of not failing the publish.

### Reporting

**Divergence is a failure, not a warning.** #149 leaves the choice open; it is
made here. A warning that publishes anyway is today's silent data loss with
extra text, and in a batch of 200 it scrolls past. Per file: `ok: false`,
`status: failed`, and a **new code `CONFLICT`** — `VALIDATION` would be wrong,
since nothing about the file is defective. The rest of the batch proceeds and
the command exits 1.

The message names the remedy: *the page has changed since your copy (v41 → v44);
re-export it before publishing, or `--force` to publish over it.* When #154
(`markfluence diff`) lands it becomes the first thing named, since "how did it
move" is the next question and nothing answers it today.

`--json` gains two fields on `updateResult`, both always present:

```json
"base": { "page_version": 41, "publish_sha256": "b800cc4f…" },
"body_changed": true
```

`base` is `null` when no usable base was found, which is the "the check did not
run" signal #149 asks for — a CI consumer needs it and a human does not, the
same split as `metadata_source`. `body_changed` is `null` alongside it.

**The status enum does not change.** `published` means the page was written to —
body, width, a label, or an attachment — and `skipped` means nothing was. A body
that was not republished shows as `version.previous == version.new`.

The human renderer needs a case it does not have. An attachment-only run — the
`architecture.png` redraw, which is the flow the ordering fix exists for — must
not print `Updating 'Title' (v44 -> v45)`, because no version bump happens (an
attachment upload does not bump the page version), and must not print nothing
either, because that reads as a no-op when the diagram really was replaced. So
the `Updating`/`Published v…` pair is conditional on the body having moved, and
a run that wrote only attachments, a width or a label reports those lines and
says the body was unchanged at its current version.

A `skipped` result keeps today's `Skipping -- no changes`, which is now finally
true when it is printed: it means the render matched the base *and* no
attachment, width or label action was found, rather than "the mtimes happened to
line up".

### Guarantees

**A new `S8`**, not an extension of `S3`: S3 (`no-overwrite-without-force`) is
about an existing *file* on disk, and there is no counterpart protecting an
existing *page* — the remote side being the one with somebody else's work on it.

> **S8** `no-overwrite-of-a-moved-page` — A page that has moved past the local
> copy's base is not overwritten without `--force`. **Partial**: a file with no
> recorded base has nothing to compare, and publishes.

**`L4` (`publish-is-idempotent`) is marked Holds and does not.** F2 is a
counterexample — mtime is what implements L4 today, and git does not preserve
mtimes, so a clone, pull, checkout or `touch` republishes an unchanged file.
This is a **correction, not a downgrade caused by this change**: L4 goes to
**Partial** with F2 named, and back to **Holds** when the render-sha skip lands,
both stated in `guarantees.md` and in the commit message per that file's own
rule.

**An explicit note beside `L2`.** An uncommitted log means two people running
the same command on the same tree can get different *behaviour* — one refuses,
one does not. That is not L2 as written, which constrains resolution and naming,
but it is the spirit that keeps `pagedoc.UserCache` unpersisted. It is
unavoidable for any per-copy base, so it is stated rather than noticed later.

## Why the log is not committed

A shared repository *is* a declaration that the repository is the source of
truth — which is arrangement 1, where `--force` is the answer and no base is
consulted. So the log serves one arrangement: a local copy, source of truth in
Confluence. Per-checkout state is the right shape there, and another person's
sync point is irrelevant to mine: Bo needs to know what *his* copy was derived
from, not what Ana did. Committing it would buy nothing and would generate a
tail-append conflict on every concurrent publish.

## What this does not do

**It only ever detects.** The outcome is publish or refuse. There is no merge,
and there should not be: bodies round-trip through `read`/`export`, and a
three-way merge of Confluence storage does not belong in this tool.

**It does not catch a page with no base.** See accrual, above.

**It does not catch two files publishing to one page.** Two files naming one
`page_id` each keep their own base, so publishing from A bumps the version and
makes B's base stale — B then refuses as diverged. That is a behaviour change
worth knowing about, and an improvement on today's silent ping-pong, but it is a
side effect rather than a feature.

## A separate pre-existing bug

`client.UpdatePage` sends `version: current + 1`. When that PUT returns an
error, markfluence cannot tell two situations apart — the write never happened,
or it happened and the *response* was lost — and it cannot simply retry, because
a re-send carries a version number the page has already reached and is refused.

`updateLanded` is the recovery: re-read the page and ask whether it is now
exactly what was sent. If the version, the title **and** `body.storage` all
match, the write landed and the error was cosmetic, so report success. It
insists on all three deliberately — the version alone proves nothing, since a
concurrent human edit could have produced that same number, and claiming
success over somebody else's content is worse than a false failure.

**The comparison can never match for a page holding a macro.** `body.storage`
is what Confluence *stored*, and Confluence does not store what it was sent: it
injects a server-generated `ac:macro-id` into every macro, measured above. A
code block is a macro, and so is a TOC. So `live.Body.Storage.Value != body`
always holds for those pages and the recovery can never fire.

Narrow but total. It only runs when a PUT errors, which is rare — but when it
does run against an affected page it is *guaranteed* to report a failure for a
publish that actually succeeded. `docs/confluence/storage-format.md` records
this for HTML comments only, which reads as an edge case about author-written
comments rather than as something covering most real pages.

**Out of scope here**, and not #149's defect: fixing it needs the normalizer
this plan removed from its own critical path — strip `ac:macro-id` and
`ac:local-id`, reconcile self-closing-tag spacing, drop comments, collapse the
whitespace their removal leaves behind. Establishing the full surface of what
Confluence rewrites on write is most of the work, and belongs with the fix.

**File it as its own issue**, with the measurement. Two things worth carrying
into it:

- **The dangerous direction is over-normalizing**, the same as it would have
  been for a body-comparison idempotence check. Too aggressive there skips a
  publish that was needed; too aggressive here reports that my write landed when
  the page actually holds somebody else's content that normalized to look like
  mine. Neither tolerates a normalizer that erases a real difference.
- **Erring the other way is free here, and that is not true of the idempotence
  check.** Too-weak normalization in an idempotence check costs a spurious
  republish — a version bump and a notification to every watcher. Too-weak
  normalization in `updateLanded` degrades to exactly today's behaviour, a
  spurious failure on a write that landed. So the fix can be attempted
  incrementally without risking a regression, which is the argument for doing it
  at all rather than deleting the recovery.

## Deferred

- **Compaction.** #149 promises a setting. A line is ~180 bytes, so a 200-page
  tree published daily grows ~13 MB a year of uncommitted local state. Real
  eventually, not at 1.0.0, and an unknown-key-fatal setting is cheap to add
  later. File it.
- **`attachment-*` history lines.** See above.
- **`info` reporting the base.** "Last published by markfluence at v44" is
  cheap and useful, and is #148's business rather than this plan's.

## Build order

Two PRs. The first is independently valuable and lands first so bases start
accruing while the second is written.

**PR 1 — the log, written but not read.**

| | |
|---|---|
| `internal/actionlog` | `Append`, `Base(key)`, the tolerant reader, per-root cache, `.markfluence/` and its `.gitignore` |
| `cmd/update`, `cmd/create` | append a line as each page completes |
| `cmd/export` | the same, plus the post-walk pass that renders each written file for its sha |
| `docs/root-model.md`, `CLAUDE.md` | where the log lives, that it is not committed, and the new package |

No behaviour change: nothing reads a line yet, so the only observable effect is
a new uncommitted file. That is what makes it safe to land ahead of the checks.

**PR 2 — `update` reads the base.**

| | |
|---|---|
| `cmd/update/update.go` | delete the mtime check; divergence refusal; render-sha skip; the width/label/attachment ordering fix |
| `cmd/update/json.go`, `schema/json-output/v1.json` | `base`, `body_changed`, and `CONFLICT` in the code enum |
| `internal/jsonout` | `CodeConflict` |
| `docs/guarantees.md` | `S8`; `L4` Partial → Holds with F2 named; the `L2` note |
| `docs/github-actions.md` | `--force` means always PUT, and git narrowing is how CI publishes only what changed |
| `update --help`, `docs/commands/` | the mtime wording is gone from `Long` and from the `--force` flag help |
| `#149` | update the body: the `--force` reversal, the render sha, the skip-writes-a-line rule, the resolved mtime question |

## Verification

Unit tests carry the degradation table and the tolerant reader. The two things
that need the live instance, against the personal space:

1. **Divergence.** `update` a file; PUT a new body directly (standing in for a
   UI edit); `update` again → refused with `CONFLICT`; `--force` → publishes.
2. **Idempotence and the ordering fix.** `update` twice → the second reports no
   body publish; then add a `labels:` line and `update` → the label is applied
   *and* the body is still not republished.

Purge every probe page.
