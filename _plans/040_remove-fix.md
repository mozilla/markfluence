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

No shared code becomes dead. Verified caller-by-caller rather than assumed:

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

One observation to record and *not* act on: `fix` is the only production caller
that omits `SearchPagesByTitle`'s variadic `statuses`, so after removal the
`len(statuses) == 0` → `StatusCurrent` default has no production caller.
`internal/client/client_test.go:695` still exercises it, so it stays honest.
Removing the default is a separate simplification and is out of scope here —
touching the client while deleting a command is how a removal grows a
regression.

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
