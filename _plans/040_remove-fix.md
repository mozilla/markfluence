# 040 — Remove `markfluence fix`

Closes #151.

`fix` reconciles a file *to* its live page: it locates the page, reads the width
and labels, and writes `page_id`, `space`, `parent`, `page_width`, `labels` and
a missing `title` back into the frontmatter, normalizing field order on the way.
It is read-only on the server, and `--dry-run` previews the whole thing.

That direction is the problem. Every other write verb makes the page match the
file; `fix` makes the file match the page. #10 wants `update` to enforce
`space`/`parent` and move pages, which makes the file authoritative for
coordinates outright — so a verb that pulls coordinates *from* Confluence works
against the grain rather than complementing it. What the drift use case actually
wants is "show me where they differ", followed by `update` to push: a read-only
report composes into CI, a hook or a cron, and `fix` structurally cannot.

## Why now, and the one thing that is lost

#151 said to do this *after* something else reports drift, because
`fix --dry-run` is the only field-by-field comparison of a file against its page
that markfluence has:

```
! DRY RUN — no changes will be written.
  [a.md] set space: ENG -> ~60c36d0718e9f60071326951
  [a.md] set page_width: narrow -> max
  [a.md] set labels: [wrong] -> []
```

#154 (`markfluence diff`) is now filed and owns that reporting — and covers more
than `fix --dry-run` did, since it compares the whole file, body included, where
`fix` only reported the metadata it was prepared to write. But **#154 is not
built**, so this removal opens a window with no drift report at all.

Accepted deliberately. markfluence is unreleased, both issues sit on the 1.0.0
milestone, and the alternative is carrying a verb whose direction is already
known to be wrong — plus the #139 obligation below. Recorded here so the gap is
a decision rather than an oversight.

**Automated repair is the genuine regression.** "Somebody labeled or resized
this page in the UI and I want my file to match" becomes: look at the page
(`info` shows labels and width, `read`/`export` emit them) and edit the file.
Locate-by-title goes the same way: `fix` would find a page from its title alone
and write the id in, which is now `find "Title"` plus a paste. Both are real and
neither is worth the verb.

**It also drops #139's hardest remaining consumer.** A pristine file whose
metadata lives in a `pages:` entry has no frontmatter for `fix` to reconcile, so
it refuses outright today (`cannot reconcile a pages: entry yet`). Making it
work means aiming a nested surgical writer at the manifest in the *opposite*
direction from `create`'s — the largest unbuilt piece of #139's write half, for
a command we are deleting. Removing `fix` means #139's write half is done.

## What gets deleted

| | |
|---|---|
| `cmd/fix/` | 1,646 lines: `fix.go` 499, `json.go` 150, `fix_test.go` 805, `json_test.go` 192 |
| `cmd/root.go` | the import and the `rootCmd.AddCommand(fix.Cmd)` line |
| `docs/commands/markfluence_fix.md` | generated — deleted by `make docs`, never by hand |

## Schema

Four edits in `schema/json-output/v1.json`:

1. `"fix"` out of the `command` enum (line 12).
2. The `if`/`then` branch for it (lines 56-61).
3. The `fixResult` `$def` (line 464).
4. The `fixSummary` `$def` (line 684).

**Edited as text, never through a JSON round-trip.** Re-serializing this
hand-authored document reformats all of it — 1,728 lines of diff for a ten-line
change, measured in #139 and reverted.

`fixSummary` is referenced by `checkSummary`'s own description ("the same
granularity `fixSummary` uses", line 697), so that sentence has to be rewritten
rather than left pointing at a `$def` that no longer exists.

Two tests close the loop from both sides, so a half-removal fails the build
rather than shipping: `cmd`'s `TestCommandEnumMatchesRegisteredCommands` (every
registered subcommand is in the enum or in `noJSONEnvelope`) and
`internal/schematest`'s document tests (every enum name has an `if`/`then`
branch constraining `results.items` **and** `summary`).

## Docs

Per-file, with the decision for each. Several of these are rewrites rather than
deletions, and one is a simplification worth having.

**`docs/guarantees.md` — L9.** The paragraph placing `fix` outside the law goes,
but what it explained has to survive in some form: L9 says a declared field is
asserted and an omitted one leaves the page alone, and `fix` was the documented
*opposite* direction, which is why an absent `labels` key meant one thing to
`update` and the reverse to `fix`. With `fix` gone the law has no exception at
all, which is worth saying plainly along with where the adopt-a-hand-labelled-
page workflow went (#154 reports, the author edits).

**L9's status does not change.** It is `Partial` because `create` asserts a
default `page_width` of `max` for a file that declares none; `fix` being outside
the law was never what made it partial. CLAUDE.md requires a status downgrade to
be stated — this is not one, and saying so explicitly is cheaper than leaving a
reader to check.

**`docs/root-model.md`** — two places, and they pull in opposite directions.

- Line 139: "every command that takes a page accepts a pristine registered file
  … the exception is `fix`, which can locate such a page but cannot yet *write*
  to its entry". The exception **disappears**, which is a genuine
  simplification: the rule becomes unconditional.
- Line 191: "a wrong `space` publishes to the wrong place in your own instance,
  which is visible and `fix` recovers it." The recovery clause is no longer
  true. The asymmetry argument against holding a `url` in the project file
  survives untouched and is the point of the sentence; only the remedy needs
  restating honestly — visible, and repairable by hand, versus a wrong `url`
  which hands out the token and is neither.

**`docs/markdown_file.md`** — the `labels` row ends "`fix` writes back the live
page's labels, which is how you adopt a page labeled in the UI", and the
`page_width` row ends "`fix` writes back the live page's width." Both sentences
go. The labels one names a capability being lost, so it is replaced by where to
look instead rather than simply cut.

**`docs/json-output.md`** — line 30 lists `changed`/`consistent` as `fix`'s
status pair, and line 36 lists the verbs whose result names a file. Both lose
`fix`.

**`docs/confluence/labels.md`** — two bullets in the verified end-to-end list
(lines 238-240). Deleted, with one re-attribution: "a block-style list rewritten
by `fix` comes back as a block-style list" is a fact about the *frontmatter
writer*, which `create --persist` still exercises, so it is kept and attributed
to the writer rather than to the command. The adoption bullet describes
markfluence behaviour that no longer exists and goes.

**`README.md`** — the command-table row (line 201), the `--dry-run` sentence
(line 230: `create`, `update` and `fix` → `create` and `update`), and the
worked `fix` section (lines 289-297).

**`CLAUDE.md`** — four places:

- line 64, the command-package list and "`fix` is read-only on the server"
- line 79, `internal/pageref`: "`create`, `update`, and `fix` all report them"
- line 111, the whole **"`fix` runs the opposite direction"** paragraph
- line 113, "Two output rules that look arbitrary and are not" — the first rule
  is `fix`-specific (`reordered` reported separately from a value change), the
  second is `read`/`export` emitting no `labels:` key. Keep the second; the
  sentence has to be re-opened for one rule rather than two.

**`CONTRIBUTING.md` needs no change.** Its only hit is the Conventional Commits
type `fix`, which is unrelated. Same for `internal/completion/completion.go`
("fixed vocabulary") — both were false positives in the first survey.

## What is *not* touched

Almost no shared code becomes dead — **one exception, and the first draft of
this plan got it wrong.** Verified caller-by-caller rather than assumed:

| helper | other callers |
|---|---|
| `labels.Read` | `info`, `read`, `pagedoc` |
| `pagewidth.Read` | `update`, `info`, `read` |
| `client.SearchPagesByTitle` | `create`, `client/find.go` |
| `jsonout.CodeOr` | `update`, `attachment-upload`, `attachfile` |
| `pageref.NotFoundMessage` | `create`, `update` |
| `pageref.NotNumericMessage` | `create`, `update`, `check` |
| `frontmatter.Normalize` | `create` |
| `frontmatter.UpdateField` | `create` |

**`frontmatter.UpdateListField` is the exception, and this table originally
omitted it.** `cmd/fix/fix.go:244` was its only non-test caller, and it is the
sole entry point to the sequence-style-preservation path — `readsBackInSeqAs`
and the `seq.IsFlowStyle` read that keeps a block `labels:` list from being
rewritten as an unreadable single flow line. After this removal **nothing in
the shipped binary writes a frontmatter sequence surgically**: `create` persists
five *scalar* fields via `UpdateField` and deliberately never writes `labels`,
`update` writes no files at all, and `read`/`export` go through `Render`, which
builds a block from scratch. So the flow-vs-block contract is pinned only by
`internal/frontmatter`'s own unit tests, and can regress without any command
noticing.

Kept rather than deleted, and the choice is deliberate rather than lazy:
`internal/frontmatter` is the library that owns the frontmatter dialect, not a
helper for current callers, and `UpdateListField` is `Render`'s surgical
counterpart with a tested contract that the next verb writing a list would want.
Deleting it is a separate decision, and #151's own reasoning applies — touching
adjacent machinery while removing a command is how a removal grows a regression.
What is *not* acceptable is the claim this plan made, so it is corrected here.

Two smaller observations, recorded and not acted on:

- `fix` is the only production caller that omits `SearchPagesByTitle`'s variadic
  `statuses`, so the `len(statuses) == 0` → `StatusCurrent` default now has only
  `internal/client/client_test.go:695` exercising it.
- `internal/labels`'s exported `Field` const has no reference outside its own
  package now. It still documents the key the package owns and is used at three
  internal sites, so it is not dead — only no longer part of anyone's API.

## Order

1. This plan.
2. `git rm -r cmd/fix`, drop the registration, schema text edits. `make check`
   fails loudly until the schema and the registry agree, which is the point.
3. Docs, including the L9 rewrite and the `fixSummary` cross-reference.
4. `make docs` to drop the generated page.

## Verification

- `make check` — `TestCommandEnumMatchesRegisteredCommands`,
  `internal/schematest`, `docs-check`, and `TestSubcommandsDocumentThemselves`
  between them catch a partial removal in either direction.
- `grep -rn 'markfluence fix\|cmd/fix\|fixResult\|fixSummary'` over tracked
  files, excluding `_plans/`, must come back empty. `_plans/` is deliberately
  left alone: it is a history of what was built, including `004_fix-subcommand`,
  and rewriting it would make the record lie.
- `markfluence --help` no longer lists it; `markfluence fix` exits non-zero with
  cobra's unknown-command error.

## As built

Three things differed from the plan above.

**A straggler the file survey missed.** `cmd/check/check.go` explained the
half-and-half warning with "`fix` moving the keys is the remedy" — a *code
comment*, so it was outside the doc list, and only the final `git ls-files |
xargs grep` sweep found it. It was also **wrong before this change**: `fix`
never moved keys into the manifest, it refused a manifest-only file outright.
So it went from describing a capability that never existed to describing a
command that no longer does. The comment now says the remedy is manual, and why.
The lesson for the next removal is that the survey has to cover comments, not
just prose files and identifiers.

**The README says less than planned.** The plan had the worked `fix` section
replaced by an explanation of where the capability went, which the first draft
wrote as "`markfluence fix` used to and was removed (#151)". Cut: markfluence is
unreleased, so nobody ever ran it, and a removal note in a 50,000-foot overview
is noise for its audience. The section now states the direction plainly and the
removal is recorded where the *reasoning* lives — `CLAUDE.md`, `guarantees.md`
and this plan.

**One commit did not build, and a swallowed error is why.** `git add -- cmd/fix
cmd/root.go schema/… docs/commands/…` was run with `2>/dev/null` after
`cmd/fix` had already been `git rm`'d, so the stale pathspec made git reject the
whole invocation — staging none of the remaining paths. `git status` showed them
unstaged in the second column and that went unread, so the commit landed with
`cmd/fix/` deleted and `cmd/root.go` still importing it. Amended, and the
amended commit was verified green in isolation with `git stash` + `make check`
rather than assumed. Two habits: do not redirect stderr away from `git add`, and
read which column `git status --short` puts the marker in.

## Verified

- `make check` green on the removal commit in isolation, and on the branch tip.
- `markfluence --help` no longer lists `fix`; `markfluence fix docs/a.md` exits
  **2** with cobra's `unknown command "fix" for "markfluence"` and suggests
  `find`.
- The sweep over `git ls-files`, excluding `_plans/`, returns only intentional
  historical references: `CLAUDE.md` and `guarantees.md` explaining the
  direction rule, `root-model.md` noting the exception is gone, and
  `labels.md`'s re-attributed round-trip bullet. `_plans/` is untouched by
  design — it records what was built, `004_fix-subcommand` included, and
  rewriting it would make the record lie.

## Review findings

A local code review found eleven stragglers, all documentation, comment or
dead-code drift rather than behaviour. None was catchable by the sweep this plan
specified (`markfluence fix\|cmd/fix\|fixResult\|fixSummary`), which is the
lesson: **a removal's survey has to cover prose, code comments, doc examples,
and callers of anything the deleted package called** — four surfaces, where this
plan listed one and a half.

Two were substantive.

**`frontmatter.UpdateListField` lost its only production caller**, contradicting
this plan's own "no shared code becomes dead" claim. Corrected above.

**The `labels.md` re-attribution was false.** The bullet was kept and pointed at
`create`'s persist step as the code path still exercising block-style
preservation — but `writeBackFrontmatter` writes five *scalar* fields and
`persistToManifest` documents that "labels is deliberately *not* written", so
`create` never rewrites a `labels:` sequence and cannot exercise it. The bullet
now says plainly that no command does, which is the honest version and is the
same fact as the `UpdateListField` finding seen from the docs side. Re-attributing
a verified claim to a code path without checking that the path reaches the
behaviour is the mistake to avoid repeating.

Two more were pre-existing errors this PR turned into contradictions:

- **`docs/markdown_file.md`'s `page_id` row** said `update` "looks it up by
  `title` and writes it back when missing". It never did: `cmd/update/update.go`
  fails with "no page id: set page_id in this file's frontmatter or in its
  markfluence.yaml entry, or create the page first", and searches for nothing.
  Wrong before, and now directly contradicting the README paragraph this PR
  added.
- **`README.md`'s `markfluence schema` console example** printed an enum
  containing `fix` and missing `check` — stale for `fix` as of this PR, and
  already wrong about `check`. The output is now verified against the binary.

And one where the edit itself was wrong: **`README.md`'s `--dry-run` list** went
from "`create`, `update` and `fix`" to "`create` and `update`", when five
commands register the flag (`export`, `attachment-upload` and
`attachment-download` too). An understatement was preserved while the line was
being touched anyway.

The rest were comments naming the deleted command: `internal/pageref/message.go`
(which had the *count* wrong too, and disagreed with the CLAUDE.md sentence this
PR rewrote for that very line), `internal/pagemeta`'s package doc,
`cmd/create/create.go` twice, `cmd/update/update.go`'s `previewWidth`,
`internal/jsonout`, and `internal/labels`'s `Diff` ("three commands", now two).
`internal/jsonout/jsonout_test.go` also built an envelope with `command: "fix"`,
a value no longer in the published enum — harmless under its substring
assertion, but it documented an invalid document as the canonical example.
