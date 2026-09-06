# Plan: convert in create's preflight phase

Make `create` convert every file in phase 1, so a document defect the converter
finds aborts the batch instead of surfacing after phase 2 has already created a
page and written its id into the file. Fixes #127.

## The bug

`create` is three-phase (`cmd/create/create.go`):

1. **Preflight** -- per file: parse frontmatter, resolve root/index, title,
   width, then `checkPageID`, space resolution, `resolveParent`, and
   `checkTitleFree`. If any file fails, `abort()` runs and nothing is created.
2. **Reserve** -- `reserveOne` creates a content-less page for each file,
   parents-first, and immediately writes `page_id` back into the frontmatter.
3. **Publish** -- `publishOne` calls `MdToConfluence` and `UpdatePage`s the real
   content in.

Nothing in phase 1 converts, so a conversion failure lands in phase 3. Concretely,
with #59's base-name attachment naming: `doc.md` referencing both
`arch/diagram.png` and `deploy/diagram.png` wants one attachment named
`diagram.png` for two assets, which `images.go:121` refuses with a
`NameCollisionError`. By then the page exists and `doc.md` carries its id, so the
user has a content-less page, an id they did not ask for, and a re-run that fails
with "a page already exists at page_id" from `checkPageID`. Recovering means
deleting the page or hand-editing the frontmatter.

Phase-3 failures have always left stubs -- `publishOne`'s comment says so, and
`_plans/026` accepted it -- because the usual one is a network or server
condition nothing local could predict. The collision is the first that is purely
a property of files on disk: `markfluence check` diagnoses it with no client, no
credentials, and no network, and everything it needs (the images, the root) is
resolved by the end of phase 1. So this one is knowable before anything is
created, and phase 1 simply is not asking.

## Decisions

**Phase 3 cannot reuse phase 1's conversion; convert twice.** This was #127's
open question and the answer is no. `createAll` seeds the shared link index
between the phases (`r.index.SetPage(...)`, create.go:389) and `rewriteDocLink`
puts `entry.PageID` straight into the built URL (links.go:180-182), so a phase-1
conversion runs against an unseeded index and renders "link not resolved" for
every in-set sibling link. Publishing that result would be a correctness
regression -- exactly the ordering dependency `_plans/026` removed. The extra
cost is one local conversion per file over bytes already in memory: no network,
no I/O.

**The converse is what makes the preflight sound, and the reason is narrower
than "the converter ignores the index".** `MdToConfluence` returns exactly two
errors -- `NameCollisionError` (images.go:121) and goldmark's own `Convert`
failure (convert.go:103). Neither reads `index` directly, but whether
`renderImage` is *reached* does depend on it: `renderLink` returns
`WalkSkipChildren` for a Broken target, and Broken is decided by
`index.FileExists`. What actually holds is that reserve only ever calls
`SetPage`, which writes `idx.pages` alone -- nothing in `idx.pages` can raise
an error or change one's text -- while `FileExists` and `Anchor` read
`idx.anchors`, fixed at `Build` time and identical in both phases. So phase 1's
verdict is neither a false positive nor a false negative against phase 3's, and
the error text is identical. A change making `SetPage` also mark a file as
existing would break this, which is why it gets a test rather than only a
comment.

**Phase 1's `Broken` and `Warnings` are discarded.** They are *wrong* for any
file linking to an in-set sibling, for the same unseeded-index reason: phase 1
warns "link not resolved" where phase 3 emits a real URL. Only the error return
is read. Nothing about a successful run's output changes. This looks like an
oversight, so the doc comment says plainly that they are thrown away and why.

Filling them into an aborted result was considered and rejected: they would be
unreliable in exactly the multi-file case `abort()` exists for, and `broken` on
an aborted result reads as though a page went up with dead links.

**The conversion runs last in `resolveFile`, after `checkTitleFree`.** Existing
error precedence is then untouched -- the page_id-first ordering that
`resolveFile`'s comment justifies ("the most specific thing wrong with the file",
three fewer API calls) and that tests pin stays exactly as it is. The cost,
stated plainly: a file that cannot convert still makes ~4 API calls before being
told. Running it first would report a document defect with zero API calls,
matching `check`, but it re-orders precedence for a file that is wrong in two
ways at once, and that reasoning is load-bearing enough not to churn for a
saving that only applies to broken input.

It is called with the real `c.SiteURL()`, `spaceKey`, and `buildinfo.Stamp()` --
all in hand at that point in `resolveFile` -- rather than `check`-style
placeholders. The error does not depend on any of them, but passing the truth
costs nothing and does not invite the question.

**A preflight conversion failure reports `CONVERT`, not `VALIDATION`.** The same
defect reports `CodeConvert` from phase 3 today, so keeping it means a `--json`
consumer's distinction between "the document did not convert" and "the server or
the frontmatter said no" survives the move. `abort()` currently hardcodes
`jsonout.CodeValidation` (json.go:203, json.go:210); `failure` gains a `code`
field instead. Note what this does *not* buy: `newFailure` still defaults an
HTTP error from the server checks to VALIDATION, so the distinction is between
"did not convert" and "everything else", not a full code taxonomy -- see Out of
scope. The schema needs no change: its `code` enum is global (v1.json:159)
and already contains both, and nothing in the `create` branch constrains which
appears.

**No special case for `NameCollisionError`.** `check` distinguishes it
(check.go:173) because there it is a document defect like a dead link rather than
the converter having failed. As the type's own doc comment says, "publishing
commands need no such distinction: either way the file does not go up." One
`if err != nil` branch, and a future third converter error is covered for free.

**`--dry-run` changes behavior for a defective file, and must.** A dry-run
converts in `publishOne` today, so a colliding file currently prints a per-file
`CONVERT` failure under the `DRY RUN` banner. After this it aborts the batch --
which is right, because a dry-run's job is to predict the real run, and the real
run now aborts. This is a behavior change to note, not a regression.

**Scope is the conversion only.** Three adjacent things stay out, below.

## Implementation

### `cmd/create/create.go`

- `convertFailure` -- a typed wrapper for a preflight conversion error, mirroring
  the existing `pageIDFailure` idiom in this file (a typed phase-1 error carrying
  what the result needs beyond the message). Holds the wrapped error and
  implements `Error()`/`Unwrap()`.
- `resolveFile` -- after `checkTitleFree`, before building the `record`:

  ```go
  if _, err := convert.MdToConfluence(
      mf, root, index, c.SiteURL(), spaceKey, buildinfo.Stamp(),
  ); err != nil {
      return record{}, &convertFailure{err: err}
  }
  ```

  carrying a comment that states why the page is discarded and why the error
  survives the phase boundary -- the two paragraphs under Decisions above.

- `failure` gains `code jsonout.Code`, and `validationFailure(filename, message)`
  is the constructor for a failure with no error value behind it (the two run()
  diagnoses itself). Between it and `newFailure`, nothing sets `code` by hand,
  which is what keeps a forgotten one from emitting `""` into a field whose enum
  does not contain it.
- `newFailure` sets `code: jsonout.CodeValidation` by default and
  `jsonout.CodeConvert` when `errors.As(err, &cf)` matches a `*convertFailure`,
  alongside the `pageIDFailure` field-carrying it already does.
- The two bare `failure{...}` literals in `run` -- "parent page is not in the
  target space" and the `"(hierarchy)"` topo-sort failure -- go through
  `validationFailure`.
- `publishOne`'s doc comment: keep the stub-is-permanent paragraph, and add that
  a conversion failure no longer reaches it -- what survives here is a server or
  network condition no local check could have predicted (S7).

### `cmd/create/json.go`

- `abort()` -- both `abortedResult(..., jsonout.CodeValidation)` calls become
  `abortedResult(..., f.code)`.

### `docs/guarantees.md`

Add **S7** `no-partial-create`, status **Partial**: a file that fails leaves no
page behind. The prose states the limit rather than burying it -- every failure
knowable from the files on disk is caught in preflight, so nothing a document
defect can do creates a page; a phase-3 server or network failure still leaves a
content-less stub, with its id already persisted, which a plain `markfluence
update` finishes. Note that this is why the guarantee is Partial and not Holds,
and that `_plans/026` accepted the stub deliberately as the price of removing
`create`'s ordering dependency.

The "How each kind is verified" table at the foot of the file is per-kind rather
than per-guarantee; Safety's row ("adversarial tests: traversal attempts,
pre-existing files") gains S7's shape -- a failing preflight asserted to have
created nothing.

## Tests

- **End-to-end, `cmd/create/run_test.go`**: a fixture referencing
  `arch/diagram.png` and `deploy/diagram.png` through the existing
  `fakeConfluence`. Assert the batch aborted, `f.pages` is empty, and the file's
  frontmatter still has no `page_id`. Not asserted on the fake refusing an
  attachment request: reverting the fix fails the file at `MdToConfluence`
  inside phase 3, which is before `SyncAttachments`, so the fake never sees one.
- **The batch is refused, not just the bad file**: two files, one colliding.
  The clean one reports `not_created`, and no page exists for it either. This
  pins the actual behavioral change -- a conversion failure goes through
  `abort()` rather than failing one file in place.
- **`--json` code is `CONVERT`**: an aborted result for the colliding file
  reports `CONVERT` while a page_id or title failure in the same batch still
  reports `VALIDATION`. Pins `failure.code` against a refactor collapsing it
  back to a constant.
- **Phase 1 and phase 3 agree on the error** (`internal/convert`): the same file
  converted against an unseeded index and against one seeded with a page entry
  returns the identical error. This is the invariant the design rests on -- that
  no error path reads the index -- and its failure mode is someone making the
  index matter to an error, which is exactly what should break a test.

## Docs

- `README.md` -- the `create` section: preflight converts, so a document defect
  aborts the batch instead of leaving a stub.
- `CLAUDE.md` -- the `cmd/create` bullet: phase 1 converts and discards the
  result, with the reuse-is-unsound reason in one clause.

## Out of scope

- **Deleting the stub on a phase-3 failure.** It would make S4
  (`no-removal-as-side-effect`) and S5 (`remove-only-ours`) non-vacuous for the
  first time, and S4 says removal is a command's stated purpose or it does not
  happen. That needs its own argument and its own status change, not a
  ride-along.
- **Pre-flighting attachment readability.** `SyncAttachments` opens each file to
  upload it, and an image that passed `Lstat` in `images.go` can still fail to
  open. Checking it in phase 1 duplicates work the upload does anyway and races
  the filesystem -- the check passes and the upload still fails -- and unlike
  the converter it is not network-free, since it needs the page's existing
  attachments. It is therefore one of S7's three named residuals, not something
  this plan closes; anything claiming publish can only fail remotely is wrong.

- **Routing a preflight HTTP error through `jsonout.CodeFor`** (#133). `newFailure`
  defaults to VALIDATION, so a rejected credential or a 5xx from `checkPageID`,
  `ResolveSpaceID`, `checkTitleFree` or `checkParentInSpace` still reports
  VALIDATION rather than AUTH/NETWORK/API. Pre-existing, and a real improvement
  now that `failure` carries a code at all -- but it changes the code on
  failures this issue is not about, so it wants its own decision. `cmd/fix`'s
  `locateCode` is the rule to copy, and `CodeFor` alone is not: it answers
  NETWORK for any non-`HTTPError`, so `no title given` would become a network
  problem.
- **`update`.** It has no reserve phase, so a conversion failure already fails
  the file with nothing created. Nothing to fix.
