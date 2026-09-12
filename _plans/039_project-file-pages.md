# Plan: page metadata in `markfluence.yaml`

Add a `pages:` key mapping a root-relative path to that file's page metadata, so
a `.md` can be published to Confluence while staying pristine. Implements #139,
on top of #100's loader. Makes #29's GitHub Action usable: one step, a glob, no
per-file inputs.

The motivation is precise about why flags do not cover this. `update README.md
--page-id 12345 --title X` already works, but **flags scale with fields while
the problem scales with files**: `--title`/`--page-id` are single-FILE-only, so
`docs/**/*.md` cannot be expressed at all, and every new frontmatter field
(`labels`, `layout`) would grow its own flag to stay usable from CI.

## The shape

A repository whose markdown carries no frontmatter at all:

```
myrepo/
├── markfluence.yaml
├── assets/
│   └── architecture.png        shared: referenced from more than one page
└── docs/
    ├── engineering-docs.md
    ├── deploy-runbook.md
    ├── on-call-handbook.md
    └── on-call-handbook/
        └── pager-flow.png      page-specific
```

`docs/deploy-runbook.md`, in full — no frontmatter, and nothing markfluence-shaped
in the body either:

```markdown
# Deploy Runbook

![System architecture](../assets/architecture.png)

Escalation is covered in [the on-call handbook](on-call-handbook.md).
```

`docs/on-call-handbook.md` references the page-specific image the same way, with
a path that happens not to leave its own directory:
`![](on-call-handbook/pager-flow.png)`. The two images differ in nothing
markfluence sees — both resolve page-relative against the root — which is why
there is one rule and not two.

The directory is named for its page rather than arbitrarily, and that matters
for the round trip rather than for tidiness. **The convention is `<stem>.md`
beside `<stem>/`**: `export` writes a page as `<stem>.md` plus a `<stem>/`
directory holding its children *and* any attachment with no recorded source
path, so a page-scoped image belongs beside its page under the page's own name
(`cmd/export/layout`, `pagedoc.AttachmentDirFor`). A tree that follows it is a
tree `export` could have produced, which is what lets `export` and `update` be
used on the same tree rather than on two shapes of it.

`export` derives each stem from the *title*'s slug, which is why every file
above is spelled the way it is — `on-call-handbook.md` rather than `oncall.md`,
`engineering-docs.md` rather than `index.md`. So this tree is not merely
export-shaped by coincidence: it is exactly what `export` would write for these
three pages, which is the strongest version of the round-trip property and the
one worth holding the example to.

A hand-authored tree is not *required* to follow it — nothing resolves a path
through a title, and the only relationship markfluence needs is the attachment
directory tracking its page's stem. What a divergent stem costs is narrower than
a rename, and worth stating precisely: `export` never renames anything, it
writes the path it computed. So exporting a page whose file is called
`oncall.md` produces `on-call-handbook.md` *beside* it — a second copy, not an
update of the first, since the skip-if-present check tests the path `export`
computed and nothing told it the two are the same page.

The project file is where every markfluence-shaped thing about this tree lives:

```yaml
# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence

# Project-wide defaults (#100).
space: ENG
page_width: max

# Per-file page metadata (this issue). Keys are root-relative paths.
pages:
  docs/engineering-docs.md:
    title: Engineering Docs
    page_id: 12345
    labels: [howto]

  docs/deploy-runbook.md:
    title: Deploy Runbook
    parent: docs/engineering-docs.md
    page_id: 12346
    labels: [runbook, ci/cd]

  docs/on-call-handbook.md:
    title: On-call Handbook
    parent: docs/engineering-docs.md
    page_id: 12347
    page_width: wide
```

Six things to read off it, each of which a decision below has to hold up:

- **An entry is a frontmatter block, moved.** The same field names, the same
  value domains, the same canonical order (`title`, `space`, `parent`,
  `page_id`, then the rest alphabetically — `fieldOrder`, fact 7). There is
  deliberately **no `path: <id>` shorthand**: one form, not two.
- **A field an entry omits falls back to the project-wide setting.**
  `docs/engineering-docs.md` has no `space:` and no `page_width:`, so it takes `ENG` and
  `max` from the keys above it. An entry sits exactly where frontmatter sits in
  #100's chain, since it *is* frontmatter that lives elsewhere — but "sits where
  frontmatter sits" is not a four-level precedence, and writing it as one is
  wrong. Frontmatter and an entry are **two spellings of one level**, so when
  both speak the rule is not "the higher wins" but D6's grading: a coordinate
  disagreement is an error and a soft one is a warning. Only *below* them is
  there precedence, and only for a field neither declares.
- **`parent` is a relative `.md` path here**, exactly as in frontmatter, and is
  resolved the same way. The `page_id` form works too; nothing new.
- **Keys are root-relative and lexical** (D3), which is the same key space
  `linkindex` already uses (fact 1) — so `docs/deploy-runbook.md` linking to
  `engineering-docs.md` resolves through the manifest with no new path vocabulary.
- **The `.md` files stay pristine.** That is the point: a README or a docs tree
  with other readers keeps no markfluence keys, and CI publishes it anyway.
- **Images need nothing from the manifest, and must not get anything.**
  `../assets/architecture.png` is resolved page-relative against the root and
  published as the attachment `architecture.png`, with the root-relative
  `assets/architecture.png` recorded in its comment as the `Source`
  (`images.go:148`, `AttachmentFilename`, **L3**). None of that reads an entry,
  and there is deliberately no `attachments:` key: attachments are *discovered
  from the body*, never declared, and a key that let the two disagree would be a
  new way to publish an image the document does not reference.

Then the whole GitHub Action is one step with no matrix and no per-file inputs:

```yaml
- uses: mozilla/markfluence@v1
  with: { command: update, files: "docs/**/*.md", url: …, username: … }
  env:  { CONFLUENCE_TOKEN: ${{ secrets.CONFLUENCE_TOKEN }} }
```

Two things about the image are worth noticing before the decisions, because both
fall out rather than needing designing.

**The manifest's key space and an attachment's `Source` are the same space** —
root-relative, slash-form, anchored on the directory holding `markfluence.yaml`.
So `docs/engineering-docs.md` as a key and `assets/architecture.png` in an
attachment comment are relative to the same thing, and a reader of the project file never
has to ask which. That is also why the manifest needs no path vocabulary of its
own (D3, fact 1).

**A shared asset above a page works here precisely because a manifest project
always has a project file.** `_plans/025` recorded that the shared-parent layout
is "repaired by this model *only when a project file exists*" — with none, a
page's root is its own directory and `../assets/architecture.png` publishes as
`IMAGE BROKEN`. A project using `pages:` has one by construction, so the layout
the README endorses is always available to it. Nothing to implement; worth
knowing before someone reads the `../` above and worries.

A mixed tree is equally legal and is what makes migration incremental (D6): a
file may carry frontmatter, or have an entry, or both with the same values.

## The core move: an entry *is* a parsed frontmatter block

#139 says an entry is "a whole frontmatter block that lives elsewhere — the same
keys, the same values, the same validation, the same canonical order." The way to
make that literal rather than aspirational is to give an entry the **same two
maps `frontmatter.MarkdownFile` carries**:

```go
type Entry struct {
    Fields map[string]string   // like MarkdownFile.Frontmatter
    Lists  map[string][]string // like MarkdownFile.Lists
}
```

Everything follows from that, including the thing that would otherwise sink the
design. `internal/project` **cannot** import `internal/pagewidth` or
`internal/labels` — both reach `internal/client`, which holds a `*project.Cache`
— so a validated, typed `Entry` would need either a broken cycle or a second
copy of every field's rules. With the two-map shape it needs neither:
`labels.Declared(e.Lists, e.Fields)`, `pagewidth.Declared(e.Fields)` and
`pageref.IsDigits(e.Fields["page_id"])` all work on an entry unchanged, in the
commands that already call them.

## What the code says — read 2026-09-12

1. **`linkindex.Build` already takes the root.** `Build(root *project.Root)`
   (`linkindex.go:53`) reads each sibling's frontmatter for `page_id`/`title`
   (`linkindex.go:68-76`) and keys `idx.pages` by the **root-relative,
   slash-separated path** — the identical key space `pages:` uses. So the merge
   needs no signature change: `root.Config` is already in hand. This is #139's
   "sharp one" and it is sharp in consequence, not in plumbing: miss it and every
   cross-document link in a manifest project degrades to the "exists on disk, not
   published yet" warning and republishes as plain text.

2. **The dialect refuses nesting, and must keep refusing it for frontmatter.**
   `plainScalar` rejects a `*ast.MappingNode`, pinned by
   `TestParseErrorWordingIsExact`'s `nested value` case. `pages:` needs two
   levels (`pages:` → path → fields), so nesting has to be opt-in per `Dialect`.

3. **Both write paths are flat and identical in shape.** `fix` (`fix.go:198-208`)
   and `create` (`create.go:988-992`) each call
   `frontmatter.UpdateField`/`UpdateListField` then `Normalize`, on the file's own
   content. Writing an entry needs the nested analogue, and there is none.

4. **`create` writes each file's frontmatter as that page is published**
   (`create.go:556`, inside the per-record loop), not once at the end. A crash
   therefore leaves every already-created page recorded. The manifest must keep
   that property (D10).

5. **`updateResult` already has a `skipped` status** and `createResult` has
   `not_created`; `update`'s required field list has 16 entries and no
   `omitempty` anywhere. So `metadata_source` is a new **required** field on both,
   and #139's "unmanaged file is skipped" reuses an existing status value.

6. **`update`'s three doomed flags are load-bearing in four places**:
   `titleFlag`/`pageIDFlag`/`pageWidthFlag` declarations (`update.go:33-35`),
   `init` (85-90), `overrideNeedsSingleFile` (96, 369) and the two resolvers
   (183, 216). Removing them deletes `overrideNeedsSingleFile` outright.

7. **The canonical field order is already shared.** `fieldOrder` +
   `keyLess`/`fieldRank` (`frontmatter.go:63-110`) is "the single source of
   frontmatter field order across all commands", so an entry reuses it and
   #139's "the same canonical order" costs nothing.

## Decisions

**D1 — An entry is `Entry{Fields, Lists}`,** for the reason above. `Config`
gains `Pages map[string]Entry`, keyed by normalized path.

**D2 — The reader gains *depth*, not key names.** `Dialect` gains
`MaxDepth int` (0 = flat, which is what `blockReader` keeps, so every
frontmatter message and refusal is untouched — the property the #100 review
caught me breaking once already). `Item` gains `Map []Item`, populated for a
mapping value within the allowed depth. The project dialect uses 2.

Nesting support does not loosen any per-key rule: a mapping where the `settings`
table says scalar is refused by the table, with the table's own message. Depth
rather than a name list also keeps the dialect from learning about `pages`,
matching the existing rule that the parser learns a kind, not a name.

**D3 — Path keys are lexical, root-relative, slash-form, byte-compared,** and an
argument is normalized the same way before lookup. A mismatch now means a silent
*skip* (D7), so the rule has to be exact.

- **Lexical only, no `EvalSymlinks`,** matching `withinRoot`/`attachfile.Resolve`/
  `destPath`. A symlinked `docs/` is legitimate, and resolving it would make a key
  depend on the checkout's layout, which **L2** forbids.
- A key **escaping the root** (`../elsewhere.md`) is a **load-time** error: the
  project file declares the project's boundary.
- **Two keys normalizing to one path** (`docs/a.md` and `./docs/a.md`) is a
  **load-time** error naming both; YAML only catches literal duplicates.
- **No case folding.** Known limit, documented beside `pageslug`'s NFD/NFC note:
  on a case-insensitive filesystem `Docs/a.md` opens the file but matches no
  `docs/a.md` key, so it reads as unmanaged and is skipped.

Both load-time errors are deliberately *not* per-file: an escaping or duplicate
key means the manifest's structure is wrong, not that one entry is bad.

**D4 — Load validates structure; a value is validated where it is consumed, and
only for a file the invocation named.** This is #100's structure/vocabulary line
plus #139's scoping constraint, and they agree. At load: `pages:` is a mapping of
mappings, each key is a legal path, each field name is known, each field's value
has the right shape (scalar vs list). Reported by the command, for its own files
only: a non-numeric `page_id`, an invalid `page_width`, an invalid label, an
empty `title`.

An **unknown field name inside an entry** is load-time, not per-file, and that
is a judgment call worth stating: it is the same typo class as an unknown
top-level key (`titel:` silently ignored is wrong forever, for that page), and it
means the manifest was written against a different markfluence — which #100
settles as fatal. A bad *value* is per-file because one bad entry must not block
every invocation in the repo.

**D5 — Resolution lives in a new package, `internal/pagemeta`.**
`Resolve(key string, mf *frontmatter.MarkdownFile, root *project.Root)
→ (Resolved, error)`, where `Resolved` carries the merged `Fields`/`Lists`, a
`Source` (`frontmatter` / `manifest` / `none`), and the disagreements.

A package rather than a helper because `update`, `create`, `fix`, `check` **and
`linkindex`** all need the identical merge, and a per-command copy is exactly how
two commands come to publish one file to two different pages. It imports
`frontmatter` and `project` and nothing else, so it stays out of every cycle.

**D6 — Both locations are legal and agreement is silent; disagreement is graded
by what it can destroy.** #139's table, unchanged:

| field | on disagreement | |
|---|---|---|
| `page_id`, `space`, `parent` | **error, that file fails** | the clobber case: a `page_id` pasted from an old file publishes over a live page |
| `title`, `page_width`, `labels` | **warning**, frontmatter wins | visible and recoverable, and frontmatter-wins is #100's chain |

`create` preflights every file, so a coordinate disagreement in **any** file
aborts the whole batch; `update` fails only that file.

Silent agreement is what makes migration incremental: copy values into the
manifest, verify, delete them from the files later, with everything working
throughout. There is no project "mode" and no all-or-nothing switch.

**D7 — A file with no metadata anywhere is `skipped`, `ok: true`, exit 0.**
Behavior change: `update` errors on that today. Repositories legitimately hold
markdown that is not published, drafts are a normal state, and a glob-driven CI
run must not go red because someone added a file. A file that *is* registered but
whose entry lacks `page_id` still **errors** — someone claimed it and `create`
has not run.

**D8 — `update` loses `--title`, `--page-id`, `--page-width`.** The rule is
*flags describe the run; files describe the page*. `--message`/`--force`/
`--dry-run` stay, being invocation metadata and behavior. `--page-width` is the
real loss, being the only batch-ok one, and #100's project-wide `page_width:`
covers it better and permanently. markfluence is unreleased, so there is no
migration to design.

`create` **keeps** its flags, for a principled reason rather than squeamishness:
it is the verb that *establishes* metadata and then persists it, so
`--space`/`--parent`/`--title` are how an entry that does not exist yet gets
bootstrapped. `update` only ever consumes metadata, so it should have no way to
invent any. This also settles permanently what #138 kept bumping into: there is
no `--labels`, and no flag for whatever field comes after it.

**D9 — New metadata is written wherever that file's metadata already is,** and
with none, to the manifest if the project has a `pages:` block, otherwise into
the file's frontmatter. Inferred, no flag: a project that has chosen the manifest
never accidentally grows frontmatter, and a project without one behaves exactly
as today.

**D10 — `create` writes each entry as that page is published,** read-modify-write
per page, not one deferred write at the end. Fact 4 is the reason: a crash must
leave every already-created page recorded, which is the property the per-file
frontmatter write already has. N reads and writes of one small file is the price,
and N is the number of pages a person creates by hand.

**D11 — The manifest writer is `project.SetPageEntry`,** a surgical nested edit
holding the same contract `frontmatter`'s writer holds: an existing key keeps its
own key node (so a preceding blank line and comments survive), only values are
replaced, and the result is **re-read and verified** before it is written. It is
built on an exported node-level helper from `internal/frontmatter` rather than a
second serializer, so quoting, the `page_id`/`parent` typing, and the
write-then-re-read fallback exist in one copy.

**D12 — An entry naming no file on disk is not an error and is not reported.**
A branch that deleted a file, or a sparse checkout, is legitimate, and every
diagnostic here is scoped to the files an invocation names — which never includes
a file that is not there. Auditing a manifest against a tree is a real want and a
separate verb; see Follow-ups.

**D13 — Land this as two PRs on one issue: read, then write** (settled
2026-09-12).

- **PR 1 — read and consume.** The dialect's depth, `Config.Pages`, path keys,
  `internal/pagemeta`, `linkindex`, `update` (flag removal, manifest lookup,
  `skipped`), `check`'s lint, `metadata_source`. This is the entire CI use case
  and it is independently useful, testable and reviewable: a person hand-writes
  entries, CI updates from them.
- **PR 2 — write.** `frontmatter`'s exported node helper, `project.SetPageEntry`,
  `create` persisting entries, `fix` migrating inline keys.

The reason to split is not size alone: PR 2's nested surgical writer is the part
most likely to need a second pass, and holding the read half hostage to it would
mean re-reviewing all of it. The accepted cost is that between the two,
#139's bootstrap flow needs a `page_id` copied by hand out of `create`'s output.

## Considered and rejected: `pages:` as a hierarchy

`pages:` is a **map** of maps, keyed by path — not a list, because the key *is*
the lookup and a list would permit duplicates with no way to ask "what is the
metadata for `docs/foo.md`?". The natural follow-on question is whether it
should instead nest, mirroring the parent/child relationships it currently
states with a `parent:` field:

```yaml
pages:
  docs/engineering-docs.md:
    title: Engineering Docs
    page_id: 12345
    children:
      docs/deploy-runbook.md:
        title: Deploy Runbook
        page_id: 12346
```

**No.** A Confluence space really is a tree and a flat map really does not show
it, so the legibility argument is genuine — but four things weigh against, and
the first two are decisive.

**It cannot replace `parent:`, only duplicate it for a subset of cases.**
`parent` has three value domains: `null` (a space-root page), an opaque page
**or folder** id, and a relative `.md` path. Nesting expresses only the third. A
page parented to a Cloud folder, or to a page outside the project, must sit at
the top level *and* carry an explicit `parent: <id>` regardless — so both
mechanisms ship, which is the "one form, not two" #139 already settled when it
refused the `path: <id>` shorthand.

**Every consumer looks up by path, so a hierarchy is flattened on load anyway.**
`update`, `linkindex` and `pagemeta` all ask for the metadata at a path, and a
nested document answers that only after being flattened into the map above. That
makes nesting a pure *serialization* preference, bought with an unbounded-depth
reader in place of D2's `MaxDepth 2`, diagnostics that need a path-within-the-
document rather than a line, and a materially harder writer in PR 2 — `create`
would have to locate a parent's `children:` block, create one when absent, and
insert at the right indent, all under D11's write-then-re-read contract, on the
part of the change already most likely to need a second pass.

**A page tree and a directory tree are not the same tree.** Nothing stops
`docs/a.md` being the child of `docs/sub/b.md`, so YAML nesting would diverge
from directory nesting and be confusing in a new way rather than a familiar one.

**Worse diffs, in a file both `create` and humans edit.** Flat, adding a page is
a self-contained hunk at one indent level; nested, it edits its parent's block,
and moving a subtree reindents everything beneath it. The same reason `go.mod`
carries a flat `require` block rather than a dependency tree.

The one correctness argument in nesting's favour does not survive contact with
the code: the failure modes it would make impossible by construction are already
caught. A cycle among in-set parents is rejected by `create`'s topological sort
(`create.go:948`), and a `parent:` naming a file with no metadata fails that
file. Both would only ever have been covered for in-project parents anyway, per
the first reason.

What the legibility point deserves is a **read-only view, not a storage
format** — see Follow-ups. Storing the tree in order to display the tree is the
expensive way round.

## Implementation

### `internal/frontmatter`

- `Dialect.MaxDepth int`; `Item.Map []Item`. `reader.items` recurses when the
  current depth is under the limit and the value is a mapping, and otherwise
  behaves exactly as now. `blockReader` sets nothing, so frontmatter is untouched.
- `TestParseErrorWordingIsExact` guards that: its `nested value` case must still
  produce the byte-identical message.
- PR 2: an exported way to build a value node for a field (today's unexported
  `valueNodeFor` + `readsBackAs` verification) so `project.SetPageEntry` reuses
  the typing and the fallback rather than re-deriving them.

### `internal/project`

- `Entry{Fields, Lists}`; `Config.Pages map[string]Entry`.
- `settings` gains `"pages": kindMapping` — the kind table earns its shape here,
  exactly as #100 predicted.
- `pagekey.go`: normalization (`path.Clean`, slash-form, root-relative), the
  escaping-key and duplicate-after-normalization load-time errors, and the
  argument-side normalizer commands call before lookup. One copy, because a
  mismatch is a silent skip.
- `entryFields` — the known field names and their shapes, which is the only place
  the manifest's schema is written down.
- PR 2: `SetPageEntry(path string, e Entry) error` (D11).

### `internal/pagemeta` (new)

`Resolve`, `Resolved{Fields, Lists, Source, Warnings}`, and the graded
disagreement (D6). `Source` is what `--json`'s `metadata_source` reports.

### `internal/linkindex`

`Build` resolves each walked file's `page_id`/`title` through `pagemeta` rather
than from `mf` alone. A file with an entry and no frontmatter gets a `PageEntry`;
a mixed tree resolves correctly per file throughout a migration.

### Commands

- **`update`** — flags removed (fact 6, `overrideNeedsSingleFile` deleted),
  metadata from `pagemeta`, `skipped` for an unmanaged file (D7), coordinate
  disagreement fails the file, soft disagreement warns.
- **`check`** — the half-and-half lint: a file carrying inline markfluence keys
  in a project that also uses `pages:` is a **warning**, not an error, so "no
  half-and-half" is enforceable without becoming a wall someone hits
  mid-migration. Plus the per-file entry-value validation of D4.
- **`create`** (PR 2) — writes a complete entry when the project has a `pages:`
  block (D9/D10); flags unchanged.
- **`fix`** (PR 2) — moves a file's inline keys into its entry, as an ordinary
  `change`. A convenience rather than a prerequisite, since agreement is legal —
  but it is what gives the `check` warning an obvious remedy, and it is read-only
  against Confluence, so migrating touches no live page.

### `--json` and the schema

`metadata_source` (`"frontmatter"` / `"manifest"` / `null`) on `updateResult` and
`createResult`, required, no `omitempty`. Debugging "why did it publish to *that*
page" in CI otherwise means reproducing the resolution by hand. D7's `skipped`
and D6's warnings surface through the existing `status`/`warnings` fields.

## Tests

- `internal/frontmatter`: nesting at, below and past `MaxDepth`; that
  `blockReader` still refuses a nested value with the byte-identical message.
- `internal/project`: a `pages:` block read into entries; an unknown entry field;
  a wrong-shape field; an escaping key; two keys normalizing to one; path
  normalization of every spelling; a scalar `pages:`; `pages: {}`.
- `internal/pagemeta`: all nine field/location combinations — absent, one side,
  both agreeing, both disagreeing — for a coordinate and for a soft field; that
  `Source` is right in each; that a *blank* value on one side is not a
  disagreement.
- `internal/linkindex`: a link to a manifest-only sibling resolves (the
  regression #139 names); a mixed tree; frontmatter and entry agreeing.
- `cmd/update`: an unmanaged file is `skipped`/`ok`/exit 0 in a batch whose other
  file publishes; a registered file with no `page_id` fails; a coordinate
  disagreement fails only that file; a soft one warns and frontmatter wins;
  `metadata_source` for each source; the three flags are gone (a test that
  `Cmd.Flags().Lookup` returns nil, so their removal is pinned rather than
  incidental).
- `cmd/check`: the half-and-half warning; an entry's bad value reported only for
  a named file; nothing reported about an unnamed file's entry.
- Schema conformance for both new fields, via each command's own builder.
- PR 2: a `SetPageEntry` round-trip preserving comments and a preceding blank
  line; an entry whose value needs quoting; `create` recording page N before
  publishing page N+1; `fix` moving keys with the file and manifest agreeing
  afterwards.

## Docs

`docs/markdown_file.md` (an entry is the same block, elsewhere — the field table
already exists and should not be duplicated), `docs/root-model.md` (`pages:`
beside the settings, the path-key rules), README (the CI flow, the removed
flags), `docs/guarantees.md` (**L2** is why keys are lexical and root-relative),
`docs/github-actions.md` (the one-step workflow this exists for), `CLAUDE.md`
(`internal/pagemeta`, and `internal/project`'s bullet gains `pages:`), and
`markfluence <cmd> --help` for `update`'s flag removal, which regenerates
`docs/commands/`.

## Out of scope

- **Creating pages in CI.** A workflow committing a new `page_id` back to the
  repo means `contents: write`, a bot commit per new doc, and a race if two runs
  create at once. Page creation stays a human act (#139).
- **Sidecar files (#38)** — superseded for the pristine-markdown use case.
- **`export` writing manifest entries** — `export` creates a fresh tree and
  writes frontmatter; whether an exported tree can be manifest-shaped is later.
- **A `--persist-to file|manifest` flag** — D9's inference covers every case
  anyone has named.
- **`update` enforcing `space`/`parent`** (#10). Until then a manifest `space` is
  read for disagreement detection but changes nothing about where `update`
  publishes, since the space comes from the live page.

## Follow-ups

- **`markfluence status`: the local tree, plus what `update` would do** (filed
  as #148). Three
  wants meet here, and one command answers all three. Seeing the page tree a
  project declares (the legibility the rejected hierarchy was reaching for).
  Asking the questions D12 deliberately makes invisible — manifest entries
  naming no file, files under root in neither location, entries whose `page_id`
  resolves to nothing. And per-page drift.

  **Offline by default, network by opt-in.** The tree, published-vs-not,
  unmanaged files and dangling entries all come from disk, and requiring a token
  to look at a tree would be wrong; the precedent is `check`, which is its own
  command precisely because it is the offline verb (the first whose `run()`
  never constructs a client). A flag adds one GET per page for the drift
  columns.

  **What drift can honestly mean is the part to get right.** The answerable
  comparison is mtime against the page's last-version timestamp — which is what
  `update` already acts on, so `status` would report exactly what `update` would
  do. Comparing *content* is the trap `docs/confluence/` warns about: the
  converter targets semantic, not byte-for-byte, equivalence, and any save
  through the Confluence editor re-serializes through ADF (the
  `coalesceSplitMarks` case), so a byte comparison would report changes on pages
  nobody touched — and "`body.storage` proves only what was stored, never what
  takes effect" is one of the two recorded traps that have each already produced
  a confident wrong conclusion. A semantic comparison needs a normalizer nobody
  has written and would be a second source of truth about equivalence.

  **Much of the drift half already exists**, which is the honest reason this is
  a follow-up and not a gap: `update --dry-run docs/**/*.md` reports
  skipped-vs-would-publish per file today, honouring the mtime skip (the skip
  returns at `update.go:250-258`, before the dry-run branch, so the forecast is
  real). The genuine delta is the hierarchy, the audit facts, and **page newer
  than file** — the direction nothing reports at all, since `update` simply
  skips it, so a stale local copy is currently silent.

  Not a flag on `children`. `children` asks Confluence what is under a node: it
  takes a page or a space, needs credentials, reports folders, and reports live
  ids. A local view takes the *root*, needs none of that, has no folders to
  report, and has no id for an unpublished page — it would share only the output
  shape, and `children`'s argument rule is already "exactly one of PAGE or
  --space".

- **Recording the version markfluence last published, which is what makes "the
  page is ahead of your copy" answerable at all** (#148 for the display, **#149**
  for the defect it fixes). Timestamps cannot answer it:
  `version.createdAt` against mtime says which side was written most recently,
  not which side has content the other lacks, and it reports every page as
  "ahead" immediately after a successful `update` — publishing sets `createdAt`
  to now while the file's mtime is from when it was saved. A `status` column
  built on timestamps alone would therefore light up for the wrong reason most
  of the time.

  One content property holding the version number markfluence last wrote makes
  it exact: a version number increments only when somebody saves, so "live
  version > recorded version" means precisely *someone other than markfluence
  has written to this page since markfluence last did*. The pattern is already
  in the codebase — an attachment records a SHA-256 in its comment and that is
  how `SyncAttachments` decides skip-vs-update — and so is the machinery
  (`SetContentProperty`/`ListContentProperties`, `page_width`'s two properties
  per page). A recorded *hash* of the body would be more granular and is worse
  here: an ADF round-trip changes the stored bytes when someone opens the editor
  and saves without editing, so a hash reports "changed" where a version number
  reports, accurately, "somebody saved it".

  **The display is not the valuable part.** `update` currently avoids clobbering
  a UI edit only by accident — page newer than file means skip — but a file
  edited *after* the UI edit wins on mtime and overwrites it silently, and
  `--force` bypasses the check regardless (`update.go:250-258`). A recorded
  version turns that into a real warning: "this page was changed in Confluence
  since markfluence last published it; publishing will overwrite that." That is
  a safety property rather than a convenience, and it is worth more than the
  column that prompted it — filed as **#149**, a bug, separately from #148's
  feature. Nothing here is needed for #139.
- `layout:` (#21) and any later field: they should need nothing here, and a test
  that an unknown-to-the-test field survives a manifest round-trip is what would
  prove it.
