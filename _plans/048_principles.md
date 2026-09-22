# 048: turn guarantees.md into design principles

Replaces the status-tracking shape of `docs/guarantees.md` with a set of
aspirational design principles. Gaps between a principle and the code move to
GitHub issues: #184, #185, #186.

## The problem

`docs/guarantees.md` was meant to guide architectural decisions. In practice
each entry carries three things that change whenever the code changes:

- a **status** (Holds, Partial, Aspirational, Vacuous),
- a note naming **what enforces it**, with code paths,
- the **history** of how the status got where it is.

Keeping those accurate is a steady stream of fiddly edits, and they still went
stale: in one review, three notes described code that no longer existed (L3's
statement, the orphaned-attachments reason, the `withinRoot` clamp) and L4's
Holds was true only under a `markfluence.yaml`. Readers do not need the precise
state of each property. The part that has earned its keep is the principle and
its reasoning.

## Decisions

**D1. The document states principles, not their state.** No status column, no
enforcement notes, no history. The document changes when a principle changes,
and at no other time.

**D2. Ids and labels are kept, and stay permanent.** They are cited in 15 Go
files, 20 times in `CLAUDE.md`, and 5 times in `docs/root-model.md`.
Renumbering would break every one. A principle's *statement* may be reworded
when the principle itself changes; its id and label may not.

**D3. Each entry has the same four parts.**

```markdown
### S3 `no-overwrite-without-force`

markfluence does not overwrite a file that exists unless you ask it to.

**Why:** ...
**Rules out:** ...
**Accepts:** ...   (only where a real trade-off was chosen)
```

"Accepts" records a deliberate design trade-off, which is a decision and does
not go stale. It never records a bug.

**D4. A gap between a principle and the code is an issue, not an edit.** The
issue names the principle by id. The document does not list or link open gaps,
so fixing one never needs a docs change.

**D5. The trade-off with no project file is recorded under S8 and L4 as an
Accepts.** Without a `markfluence.yaml` there is no action log
(`actionlog.For` returns nil), so `update` has no merge base and no publish
hash. It neither refuses a moved page nor skips an unchanged body. This is a
consequence of #139's rule that a root with no project file never gets one
created for it, and we have no better design. The README's "Do you need a
`markfluence.yaml`?" section already tells users; the principles doc records
it as the trade-off it is.

**D6. The filename stays `docs/guarantees.md`.** The title becomes "Design
principles". Renaming would break the live references for no reader-facing
gain, and every plan that links the file.

## Disposition of each entry

| id | becomes | notes |
|---|---|---|
| S1 | principle | rules out clipping a path, which writes something under a name nobody chose |
| S2 | principle | the upload race is a bug: #186 |
| S3 | principle | carries "an overwrite and a removal are different risks" |
| S4, S5, S6 | principles | drop "Vacuous" and the future-removal commentary; #99 and #129 are where removal will arrive |
| S7 | principle | Accepts: a network failure or unreadable attachment mid-publish leaves a stub whose id is on disk, and `update` finishes it (the price of reserving every id before converting). The unrecoverable case is a bug: #185 |
| S8 | principle | Accepts: protection needs a base, so it needs a `markfluence.yaml`, starts at the first publish or export, and a fresh clone has none. `--force` overrides it by design |
| L1, L2, L8 | principles | L2 Accepts: behavior can differ between checkouts, because the log is not committed; the published bytes do not |
| L3 | principle | statement as corrected in f74ba55 (file name, not location) |
| L4 | principle | scope is the body. Accepts: the same project-file dependency as S8 |
| L5, L6 | principles | reworded to what was always meant: the round trip keeps the *meaning*, and Markdown is a fixed point. The byte-level measurements move out (below) |
| L7, C1, C2 | principles | C2's implementation detail moves out (below) |
| L9 | principle | `create`'s default width is a bug or a decision to make: #184 |
| R1, R2 | principles | scope notes stay short (R1: `.md` links and images only) |
| Symlinks non-goal | kept | a decision, not a status; the `#symlinks` anchor has 4 inbound links |
| failure policy | kept | "never a silent partial success" |
| "How each kind is verified" | dropped | it describes test practice, which is not a principle |

## Where the removed detail goes

Most of it already has a home, and the rule is: code facts live beside the
code, Confluence facts live in `docs/confluence/`.

- **C2's YAML mechanics** (self-check, node kind, single-line rule, flow vs
  block): already in `CLAUDE.md` under `internal/frontmatter` and in that
  package's comments. Deleted from the doc.
- **L5's storage measurements** (`<li><p>`, TOC `ac:local-id`/`ac:macro-id`):
  `docs/confluence/storage-format.md` already covers the macro ids; add the
  list-item case there if it is missing.
- **S7's phase detail**: already in `CLAUDE.md` under `cmd/create` and in
  `create.go`'s comments. Deleted from the doc.
- **Status history** (L4's mtime story, R1's path to Holds, the retired
  "Nothing in Confluence is deleted"): git history and the plans hold it.
  Deleted.

## Files

| file | change |
|---|---|
| `docs/guarantees.md` | rewritten per D1-D6; roughly 150 lines |
| `CLAUDE.md` | the paragraph describing the file: principles, no statuses, gaps are issues. Also its id range, which says S1-S7 and L1-L8 |
| `README.md` | the documentation table row ("each one with an honest status") |
| `docs/root-model.md` | check its three references still read correctly |
| `docs/confluence/storage-format.md` | the `<li><p>` measurement, if missing |

## Not in scope

- **Fixing #184, #185, #186.** Each is its own change.
- **Renaming tests after principle ids** (option B). Worth doing only
  gradually, as tests are touched.
- **Editing plans that cite statuses.** Plans are history (`_plans/README.md`).
- **Rewording the README's `markfluence.yaml` advice.** It already states the
  trade-off in D5.
