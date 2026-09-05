# Plan: recursive export, and the attachment name it depends on (#59)

Exports a page's subtree, or a whole space, as a directory tree that mirrors the
Confluence hierarchy and publishes back unedited. Closes #59, and flips **L5**
(`roundtrip-from-confluence`) and **L6** (`roundtrip-from-disk`) from Partial to
Holds -- the two guarantees `docs/guarantees.md` already defers to this issue.

The directory layout and the two attachment placement rules were settled in
#59's design comment and in `_plans/025` §"Multi-page export layout". This plan
does not re-derive them. What it adds is the sequence, and the two decisions 025
got wrong -- the attachment name, and what to do about a slug collision.

## The thing 025 got wrong

025 says of page-scoped attachment placement: "Single-page export adopts the same
rule ... Nothing depends on today's flat-in-the-root behaviour."

Something does: the attachment **name**.

`images.go:104` names an attachment `AttachmentFilename(rootRel)` -- the
root-relative source path, percent-encoded -- and `client.go:1028` keys existing
attachments by name to decide create-vs-update. So the name is the identity, and
it is a function of the path. Page-scoping changes the path a native attachment's
markdown points at (`diagram.png` becomes `home/diagram.png`), which changes the
name (`home%2Fdiagram.png`), which makes republishing **create a second
attachment and orphan the first** -- with a changed `ri:filename` in the body, so
the page changes too. That breaks the attachment-name round-trip, which is the
half of L5 that does hold today -- not L5 as a whole, which #125 showed failing
single-page for an unrelated reason (`260585a`).

Putting the page file inside its own directory does not dodge it: `Source` is
root-relative since `_plans/026` commit 4, so the name follows the asset's
position in the tree and not its position relative to the page. **Flat at the
dest root is the only placement that preserves an encoded name.**

### So the name changes instead

An attachment is named by its **basename**. `attachname.go`'s own doc comment
says the encoding buys exactly two things, and both are payable another way:

| the encoding bought | replaced by |
|---|---|
| no two assets can collide on one name | an explicit refusal at publish time, naming both paths |
| the path is recoverable from the name alone | the `path=` already recorded in the comment (026 commit 7) |

What that buys back, beyond L5:

- **Page-scoped placement becomes free.** The name stops moving when the file
  moves, so `dest/<page dir>/<name>` costs nothing and orphans nothing.
- **Moving an asset stops orphaning it.** `assets/flow.png` to `img/flow.png`
  keeps the name, and the recorded-path disagreement already makes that an
  update that repairs the path (`client.go`, the path-disagreement rule).
- **`attachfile.Resolve` and `convert.sourceFor` stop disagreeing.** They
  disagree today: `Resolve` uses `a.Title` verbatim (`attachfile.go:99`,
  deliberately -- a hand-uploaded `a%2Fb.png` is indistinguishable from a
  published one) while `sourceFor` decodes it (`storage_to_md.go:117`). A page
  whose attachment is encoded but whose comment is missing therefore exports to
  `dest/assets%2Fbrand.png` with markdown saying `assets/brand.png`: a broken
  image, today, in single-page export. Deleting the decode retires the class.
- **Names become readable in Confluence's own UI**, which is where everyone
  other than markfluence sees them.

What it costs, stated so nobody discovers it later:

1. **A new publish-time failure.** One file referencing `arch/diagram.png` and
   `deploy/diagram.png` has no valid naming. Refused, not warned: publishing
   would render one image in both places and record only one path.
2. **Path recovery rests entirely on the comment.** A markfluence-published
   attachment whose comment is gone (an older format, a hand edit) is
   indistinguishable from a Confluence-native one, so it takes the unsourced
   rule -- `dest/<page dir>/<name>` -- rather than returning to the directory it
   was published from.
3. **Switching schemes orphans once.** Every already-published attachment whose
   path has a directory component re-uploads under its basename, leaving the
   encoded original behind. One time, on markfluence's own pages. Orphan cleanup
   is a follow-up (bound by **S4**-**S6**, all three currently Vacuous).
4. **An attachment's version history can mix two assets.** A page that used to
   reference `assets/flow.png` and now references `img/flow.png` updates the one
   `flow.png` attachment in place. The rendering is right and the recorded path
   is right; only the history is mixed.
5. **A hand-uploaded attachment is clobbered on a far wider surface.** An
   unmanaged attachment has `SHA256 == ""`, which never equals a real sum, so it
   is silently overwritten as an update (`client.go:1052`). That is today's
   behaviour, but today it takes a hand-upload named like an encoded path;
   under basenames, any hand-uploaded `logo.png` on a page is taken over the
   moment that page references any `**/logo.png`. Versioned and recoverable, and
   adjacent in spirit to **S5** without breaching it -- markfluence is writing a
   new version of something it did not create.
6. **Two same-basename assets can still collide through paths the converter does
   not see**: a batch of `attachment-upload` FILEs, and an `ri:filename` inside
   raw storage the shield passes through. Both are closed in commits 2 and 4
   rather than accepted; they are listed here because neither is covered by the
   one refusal in `images.go` that the argument above rests on.

## Decisions

### Surface

**`export --depth`**, a string vocabulary: a non-negative number or `all`,
default `0`. Mirrors `children --depth`'s precedent, with one deliberate
divergence to state in the code: `children` refuses `0` because there it is a
request for nothing, while here it is the named page alone -- real, useful, and
the current default. Depth counts levels *below* the target, so `--depth 1` is
the page plus its direct children, and a folder counts as a level exactly as it
does in `children`.

**`--space KEY`** lists a whole space instead of a page, via
`pagetree.WalkSpace`, resolved through `ResolveSpaceID` first so an unknown key
fails as a typo (exit 2) rather than as a 404 indistinguishable from a rejected
credential. Exactly one of `PAGE` and `--space`, checked before credentials.

**`--space` requires an explicit `--depth`** (`Flags().Changed("depth")`, the
mechanism `children`'s hint and `search --cql` already use). A whole-space export
is thousands of requests and a large tree; it should be asked for, not be what a
bare typo produces. The error names `--depth all`. `--depth 0` with `--space`
is explicit but still a request for nothing, and is refused with the same
message -- the target of a space walk is its pages, and the space itself is not
a thing that can be written.

**`--file` is refused when multi-page** -- it names one file, while the page's
directory name comes from the slug regardless, so allowing it would let the two
disagree. `--all-attachments` and `--skip-attachments` apply per page, unchanged.

### Layout

A page is `<slug>.md`, gaining a `<slug>/` beside it when it has children or
attachments. `slugify` (`cmd/export/export.go`) is reused unchanged, for folder
directories as well as page files: lowercase and trim, drop everything outside
`[\p{L}\p{N}_\s-]`, collapse each whitespace run to one `-`, trim the ends, cap
at 80 runes, and fall back to the id when nothing survives. Two properties the
pre-flight leans on: it lowercases, so `Deploy` and `deploy` already collide and
get refused; and it drops `/`, so no title can inject a path separator.

Punctuation is deleted rather than separated, so equivalence runs through
whitespace alone -- `Title:1` yields `title1` while `Title 1` and `Title: 1` both
yield `title-1`. **Known limit:** the slug keeps non-ASCII deliberately
(`über-café`), and NFD and NFC spellings of one title are different Go strings,
so the pre-flight groups them separately where a normalizing filesystem (APFS)
sees one filename. That pair gets no suffix and the second write falls through to
the **S3** exists-skip. Normalizing before comparison would fix it and needs
`golang.org/x/text`, a new direct dependency for a case nobody has hit -- so it
is named here rather than solved. A **folder** shapes the path and nothing else: no result row, no
directory unless something lands inside (an empty folder is unrepresentable on
disk anyway -- `create` cannot make one), and as the *named root* the folder is
`dest` itself, its children landing at the top level exactly as a space's roots
do, since there is no `dest/<folder>.md` to hang a directory off. A folder title
slugging to nothing falls back to its id, as `pageFilename` already does.

Attachments follow provenance, per 025: a recorded `path=` goes to
`dest/<recorded path>`, and one with none goes to `dest/<page dir>/<name>`.

**The markdown moves with the placement, and for *both* provenances.** This is
the second coincidence 025's worked example rests on, and it is not the fallback
root the `markfluence.yaml` section below deals with. `sourceFor`
(`storage_to_md.go:113`) writes a recorded path **verbatim**, and a recorded path
is root-relative (026 commit 4) -- but publish-time image resolution is
**page-relative**: `images.go:65` calls `rootRelative(r.root.Dir, r.baseDir,
fsPath)`, joining the src onto the *referencing file's own directory*. The two
agree only while the `.md` sits at the dest root, which is precisely what
mirroring ends. Left alone, `dest/home/child.md` would carry
`![](assets/brand.png)`, resolve it to `dest/home/assets/brand.png`, find
nothing, and republish as `IMAGE BROKEN` -- for every sourced asset on every
page below the root, which is most of a real tree and includes this plan's own
live fixture.

So `StorageOptions` gains the page's **dest-relative directory**, not an
unsourced-only prefix, and `sourceFor` positions both cases against it:

| the attachment | placed at | markdown written |
|---|---|---|
| recorded `path=assets/brand.png`, page at `dest/home/child.md` | `dest/assets/brand.png` | `../assets/brand.png` |
| recorded `path=assets/brand.png`, page at `dest/home.md` | `dest/assets/brand.png` | `assets/brand.png` |
| no recorded path, page at `dest/home/child.md` | `dest/home/child/diagram.png` | `child/diagram.png` |
| no recorded path, page at `dest/home.md` | `dest/home/diagram.png` | `home/diagram.png` |

The fourth row is the one an earlier draft left undefined, and defining it is
what settles the rest. **Page-scoping applies to every page, the named export
target included**: `export PAGE` with no `--depth` places an unsourced
attachment at `dest/<slug>/<name>`, not at `dest/<name>`. 025's reason holds --
without it the same native page exports as different markdown depending on how
many pages were asked for, the invocation-dependence **L2** forbids for names
and that nothing should reintroduce for placement.

So single-page export is **not** byte-identical to today for a page with a
comment-less attachment: it moves from `dest/diagram.png` to
`dest/<slug>/diagram.png`, deliberately. A **sourced** attachment does get the
identity transform on a root page (`rel(".", recorded)` = `recorded`), so that
half is unchanged. The next section is what keeps `read` and
`attachment-download` in step with this rather than stranding them on the old
one.

Implementation note: Go's `path` package has no `Rel`, so this is
`filepath.Rel` bracketed by `FromSlash`/`ToSlash` -- the precedent is
`rootRelative` itself (`images.go:223`), which already does the same
conversion. And the relative form is what **C1** wants anyway: it is what
resolves in a GitHub preview, and what the author originally wrote.

### One placement rule, three commands

An attachment with no recorded path is page-scoped by **`read`,
`attachment-download` and `export` alike** -- not by `export` only. `read`
derives the position from the page's own title, `attachment-download` writes
`dest/<slug>/<name>`, and `--flat` (which download already has) is the
documented opt-out meaning "bare name, straight in `--dest`".

An earlier draft scoped `export` alone and left the other two flat, on the
reasoning that `read` prints to stdout and has no directory to position against.
That produced two conventions and cost `pagedoc.go:1-8`'s stated property, that
`read` and `export` emit byte-identical markdown. One rule keeps it, and is
easier to explain than what we have today: an attachment already lands at its
recorded path, directories and all, so page-scoping makes the *unsourced* case
behave the same way instead of differently.

It also keeps the pairing that made the flat option attractive. `read`'s output
resolves against what `attachment-download` writes, because both moved, rather
than because both stayed.

**Byte-identity survives for a page at the dest top level whose slug is unique
among its siblings** -- a single-page export, a space's root pages, the named
export target. That is the comparison anyone actually runs, and it is the one
the two-convention draft broke.

It does *not* survive deeper in the tree, and the reason is structural rather
than fixable: three things `read` cannot know.

- **A sourced destination is depth-dependent.** `dest/home/child.md` carries
  `../assets/brand.png`, computed as `rel("home", recorded)`. `read` has no tree
  and no dest, so its position is the top level and it emits the recorded path
  verbatim. The unsourced case escapes this only by cancellation -- both sides
  prefix the same page directory -- which is why the difference is easy to miss.
- **A `-<id>` suffix comes from a sibling.** A page in a collision group exports
  as `deploy-prod-123456/diagram.png` where `read` says `deploy-prod/`.
- **`parent:`** is a path in an exported tree and an id everywhere else. Note
  this difference does not exist *today* -- `export` gets its frontmatter from
  the same `pagedoc.Frontmatter` `read` uses (`export.go:153` →
  `pagedoc.Render`) -- so it is introduced by commit 16, not inherited.

So the property `pagedoc`'s package comment should state, and what commit 23
writes there, is: **one conversion, parameterized by position and parent form,
identical output whenever those parameters are** -- with top-level placement
called out as the case where they always are.

`pagedoc.Options` carries the position and the parent form, and all three
commands go through it, so they cannot drift by accident -- only by argument.

### `parent:`

An in-set parent becomes a relative `.md` path (`parent: ../home.md`), which
`create` already resolves relative to the referring file (`create.go:630`). Three
exceptions, each forced:

- the **export root** keeps its live parent id -- truthful, and unchanged from
  today's single-page export;
- a page whose parent is an exported **folder** keeps the folder id, since a
  folder has no `.md` to point at and `create` accepts a folder id (#68);
- under `--space`, a root page gets `parent: null`, which is what it is.

Two notes on what reads these. `update` never looks at `parent` at all, so the
`markfluence.yaml` marker is load-bearing for images and nothing else. And `fix`
reconciles frontmatter from the live page, so running it on an exported tree
rewrites `parent: ../home.md` back to a numeric id -- consistent with what `fix`
is for, and surprising enough to warrant a sentence in its help.

`page_id` is kept in every file, so the tree republishes to the pages it came
from. Retargeting a tree at fresh pages would mean stripping ids: out of scope.

### `markfluence.yaml` at `dest`

Written for a multi-page export when `dest` has none, reported on its own line,
honoured by `--dry-run`, never overwritten (**S3**). The envelope's `roots`
becomes `[dest]`.

**Written before the first page**, not after the last. A run that dies partway
is the case the retry story is built around, and a partial tree with no marker
is a tree whose every shared asset republishes as `IMAGE BROKEN` -- so writing
it last would hand the user exactly the broken artefact the rest of this section
exists to prevent.

This is load-bearing, not tidiness. 025's worked example for single-page export
notes the republish works because the file lands at `out/onboarding.md`, so the
no-config fallback root -- the file's own directory -- happens to *be* `out/`. In
a mirrored tree that coincidence dies: `out/home/onboarding.md`'s fallback root is
`out/home/`, so a shared asset reconstructed at `out/assets/brand.png` sits above
its root and republishes as `IMAGE BROKEN`. Without the marker, a recursive
export is not republishable at all.

### Collisions, refusals and failures

**Slug collisions are disambiguated, not refused.** A pre-flight over the whole
walked set groups nodes by slug per directory -- the namespace covers page files,
page directories and folder directories together, so a page "Team" beside a
folder "Team" is one group -- and every member of a group larger than one takes a
`-<id>` suffix on both its file and its directory. Reported as a warning naming
each page, exit 0, and the check needs only the walk's titles, so it costs no
bodies.

It buys two things, and the second is easy to miss: sibling filenames stop
colliding, *and* every page directory in the tree becomes unique -- which is
what makes page-scoped attachment placement collision-free by construction, and
lets the conflict rule below stay as narrow as it is.

025 and #59's comment both say refuse, and the round of grilling that produced
this plan agreed. That was wrong, on three counts:

- **The recourse does not exist.** 025's remedy is retitling in Confluence or
  exporting subtrees separately. `--space` exists to export spaces the caller
  does not own, where retitling is not available -- so refusal makes such a
  space permanently unexportable over a punctuation variant.
- **L2 does not cover an exported filename.** It constrains reference
  resolution and attachment naming, both of which are identity. 025 invoked
  "L2 in the export direction" by analogy. Per **L8** identity and hierarchy
  are never inferred from disk layout -- `page_id` in the frontmatter carries
  both -- so an exported filename is ergonomic, and set-dependence in an
  ergonomic name costs nothing an attachment name's would.
- **"Depends on walk order" is not true.** The walk is position-ordered and
  deterministic, and suffixing *every* member of the group rather than the
  second one encountered removes ordering from the question entirely: no member
  holds a privileged unsuffixed name.

Suffixing also retires the argument for making this atomic in the first place. A
refusal had to abort the whole export because a refused page leaves its
children's `parent:` paths dangling; nothing is refused now. And 025's objection
to "skipping with a warning" -- that it is a partial export exiting 0 -- does not
reach this, because a suffixed export is complete. The one cost is that a later
subtree export containing only one of the pair writes the unsuffixed name.

The `-<id>` form is also already the established fallback: a title that slugs to
nothing becomes `<id>.md` today (`pageFilename`). The ordering this implies is worth spelling out, because naming one consumer
invites an implementation that special-cases one consumer. The suffix pass runs
over the walk's titles and ids before any per-page work, and **everything
downstream reads its output**: the `parent:` path a child writes
(`../deploy-prod-123456.md`, not the name the page would otherwise have had),
the position handed to the renderer, the `dest/<page dir>/` an unsourced
attachment is placed in, the pre-fetch exists-stat the retry story depends on,
and the `--dry-run` preview. `pagetree.Walk`/`WalkSpace` return the complete
slice before any page is fetched (`pagetree.go:42`), so this is a free in-memory
pass and one processing pass after it -- not two passes over the network.

**A walk that fails is not a page that fails.** `pagetree.Walk`/`WalkSpace`
abort on the first child-listing error (`pagetree.go:42`), before the per-page
phase exists, so the rule below does not reach it. For a `PAGE` target that is
`operationalFail` against the named id; for `--space` there is no page id to
name, so it takes the stderr `errorObject` shape `children --space` established
for exactly this -- which `find` and `search` share.

**A page failing mid-run skips its subtree**, one failed result each, wording
from `create`'s existing precedent for the same shape (`create.go:374`, "parent
page was not created; skipping"). Same invariant: no emitted file points at a
missing parent. They count as failed, so the summary and exit status are honest.

**Two pages writing one destination with different content fails the second**:
the attachment's status is `failed` (page-level status keeps its existing
`wrote`/`skipped`/`""` vocabulary, so the schema's enum is untouched), code
`VALIDATION`, error naming the other page. Not overridable by `--force`, which
is about local files rather than about picking a winner between two pages.
Identical bytes skip, which is 025's success case reached by **S3**.

**The rule is deliberately narrow: it compares two recorded paths, and nothing
else.** Comparing bytes without downloading them means comparing the checksums
in the attachment comments (`parseAttachmentComment`, `client.go:394`) -- the
listing carries no server-side digest, only `Extensions.FileSize`, which can
prove difference and never identity. Two recorded paths are decidable, because a
recorded path means a managed attachment and both sides truncate their sum
identically (`client.go:1037`). Every other way two writes can meet takes the
**S3** exists-skip instead, and that is a narrowing rather than a hole:

- **Two unsourced attachments cannot collide**, with one named exception. Each
  is page-scoped into `dest/<page dir>/`, and the slug suffix pass makes page
  directories unique by construction -- the second thing that pass buys. The
  exception is the normalization limit in §Layout: an NFD/NFC title pair gets no
  suffix, so on APFS the two share one directory and their same-named
  attachments meet there. That pair takes the **S3** skip like everything else
  here, which is the same answer, reached without the guarantee.
- **Unsourced meeting a recorded path** needs a recorded `path=` equal to another
  page's slug directory -- `home/diagram.png` in a tree that also has a page
  titled "Home". It is the same shape as an attachment colliding with a page
  file, and gets the same answer.

The alternative was to report a conflict whenever a checksum is missing on
either side. Rejected: it fires almost only on the case above, and it makes the
rule read differently depending on provenance for no gain in what the caller can
actually do about it.

**An attachment colliding with a page file** gets no special handling: attachment
names are unknown until each page is fetched, so pre-flighting them would double
the walk's cost for a case measured in zero occurrences. **S3** skips it.

### Retry cost

The walk supplies the title, so a page's destination is known before any per-page
request. When the file exists and `--force` is absent, the page reports `skipped`
and its render is skipped -- saving the `page_width` read and every `<ac:link>`
title lookup -- while its attachments are still listed and the missing ones still
downloaded, which is what makes a retry resume a run that died partway through
attachments rather than pages. With `--skip-attachments` a complete tree costs
only the walk. `--force` redoes everything, and is also how a tree whose pages
changed upstream is refreshed: export never overwrites on its own.

Serial, no concurrency: parallel fetches against a shared instance is how a rate
limit gets provoked. Cost is documented in the help the way `children`'s is.

### Output

Human output keeps today's `wrote`/`skipped`/`failed` line format verbatim --
the paths and names inside those lines do change, per §Layout and the naming
switch -- and adds a trailing count for a multi-page run.

`--json` needs no envelope change: the `export` branch is already
`oneOf[exportResult, singleOpFailure]`, `roots` is already required, and a
mid-run page failure fits `singleOpFailure`, which carries a `page_id`. Three
things inside the branch do change, and "one added field" was too optimistic:

- **`exportResult` gains `parent_file`** (required, nullable): the relative path
  written into the frontmatter, null when the parent stayed an id. `create`
  already uses that name for the concept.
- **`basicSummary` is replaced by an `exportSummary` carrying `skipped`.**
  `basicSummary` is `total`/`succeeded`/`failed` only, and skip-and-resume is
  this feature's whole retry story -- a summary that cannot say "40 skipped"
  contradicts the section above it. A separate def rather than a new key on
  `basicSummary`, which `children`/`find`/`search` share.
- **The `markfluence.yaml` write gets a `project_file` field on that summary**,
  reversing the earlier decision that `roots` was signal enough. `roots` cannot
  distinguish a marker that was found from one that was created, and a file
  written by the command that appears nowhere in `--json` -- including in its
  `--dry-run` preview -- is precisely the invisible write **S3**-adjacent
  reporting exists to prevent.

## Guarantee changes

| id | change |
|---|---|
| **L3** | Note rewritten. Both clauses become false: an asset that moves keeping its basename no longer changes identity, and tree reconstruction now rests on the comment rather than on the name. |
| **L5**, **L6** | Partial to **Holds**, but only on the back of a property test (commit 24). With basename naming a native page republishes to the same attachment, so the round-trip is a fixed point in both directions -- and that is the *argument*, not the evidence. `260585a` corrected this file to say L5 has no property test and that its status should be read "as an assertion about known constructs rather than a property", after #125 turned out to be a measured single-page counterexample to a claim made here. Flipping the status on another unverified argument would repeat exactly that. |
| **R2** | Note widened to cover naming as well as placement -- a basename collision is the same obligation one step earlier in the pipeline. **The label stays `report-unplaceable-attachments`**: `docs/guarantees.md:27` makes labels as permanent as ids, since a renamed label makes an old citation silently wrong. An earlier draft of this plan proposed renaming it, which that rule forbids. |
| **S4**-**S6** | Unchanged, still Vacuous. Orphan cleanup is a follow-up issue. |

## Commit sequence

The naming change comes first and stays separable in history, even though it
ships in one PR with the tree feature.

**Naming**

1. `feat(convert)`: name an attachment by its basename. `attachname.go` keeps the
   file and its rationale, rewritten -- it still owns the mapping, now a
   projection rather than a bijection.
2. `feat(convert)`: refuse two same-basename assets in one file, reported through
   `Broken` with `nodeLine`'s `"line %d: "` prefix, naming both paths.

   Two constraints on the implementation. The rekeyed `r.seen` compares
   `rootRel`, not the raw src, or `./a/x.png` and `a/x.png` read as a collision
   with themselves -- `rootRelative` (`images.go:223`) is what cleans them into
   one path. And the check covers `ri:filename` inside the raw storage the
   shield passes through (`convert.go:69`), which never enters `r.seen` at all:
   `export`'s own `referencedNames` (`cmd/export/export.go:224`) scans for
   exactly that construct, which is the proof it occurs. Missing it would let a
   converted image silently rebind a pasted reference.
3. `feat(check)`: report a basename collision offline -- no network needed to see
   it, which is exactly what `check` is for.
4. `refactor(attachmentupload)`: `--name` records the path, names by basename,
   and the same refusal applies across a batch of FILEs. `localAttachments`
   (`attachmentupload.go:152`) has no in-batch duplicate check, and
   `planAttachments` builds its `remote` map once and never updates it inside
   the loop (`client.go:1026`), so `a/x.png b/x.png` would plan two `created`
   calls for one title, or two `updated` calls against one `existingID` --
   last-write-wins, both reported as success.
5. `refactor(convert)`: stop interpreting a stored name. `sourceFor` becomes
   recorded-path-else-verbatim and `AttachmentSource` is deleted, which is what
   makes `Resolve` and `sourceFor` agree by construction. `client.go:1062`'s
   comment ("The name is the encoding of the path, so the two move together")
   becomes false here and is rewritten in this commit, not left for the docs one.

   `TestRoundTripEncodedImageSources` goes with it, in this commit rather than
   the next: it asserts the property being removed -- "every destination
   `read`/`export` writes must decode back to exactly the path `update`
   published from" -- for exactly the reason this plan cites, that otherwise
   "re-publishing the export would upload them again under new attachment
   names". It is **rewritten, not regenerated**, into the property that replaces
   it: a destination round-trips through the recorded `path=`, and a stored name
   is never decoded.
6. `test`: the golden churn -- eight files carry `%2F`, and they split two ways.
   Five `regression/*/test.output` goldens regenerate (`make
   regen-regressions`); `regression/images-encoded-src/main.md` and the two
   hand-authored `storage2md/{images,images-encoded-src}/input.storage` inputs do
   not, and need editing by hand. Both `images-encoded-src` cases want renaming
   as well, since the encoding stops being the thing they exercise. Beyond
   `internal/convert`, `%2F` fixtures also live in `cmd/{export,attachmentupload,
   attachmentlist,attachmentdownload}` and `internal/{attachfile,client,pagedoc}`
   tests -- all of them fail loudly rather than silently, so this is unbudgeted
   work rather than a risk.
7. `docs(confluence)`: `attachments.md`'s framing. Its opening states that
   markfluence percent-encodes `%`→`%25` then `/`→`%2F` and calls it bijective;
   `:58`'s name-length math is expressed in slashes-per-255-characters; `:123`
   says "the name is the encoding of the path, so the two move together". All
   three go. **The Verified probes stay** -- that a `%2F` name resolves and
   re-escapes to `%252F` in the image URL, and that form fields decode as
   Latin-1, are facts about Confluence and remain true whether or not
   markfluence produces such a name. Separating the two is the whole job in this
   file; do not delete a measurement because we stopped relying on it.
8. `docs`: the guarantees. L3's note (both clauses false), R2's *note* widened
   to cover naming as well as placement with its label left alone
   (`report-unplaceable-attachments` -- see §Guarantee changes for why a rename
   is not available), and `root-model.md:114`'s "what its Confluence attachment
   name encodes".
9. `docs`: README and CLAUDE.md for the naming change. README's four regions --
   the attachment-naming paragraph at `:1189`, `--name` "which markfluence
   encodes for you" at `:749`, the `attachment-list` SOURCE example at `:718`,
   and §`export`'s no-`--attachments-dir` rationale. CLAUDE.md's `internal/
   convert` (`attachname.go`'s clause), `attachment{list,upload,download}`
   ("never a decode of the stored name"), and `internal/attachfile` paragraphs.

   Two things to get right here. **The `--attachments-dir` rationale is now
   void**: it rejects the flag because rewriting an image's `src` would change
   its attachment name, and under basenames moving `assets/x.png` to
   `attachments/x.png` keeps the name `x.png`. Rewrite the reasoning; the flag
   stays unimplemented, but it is no longer impossible and the paragraph must
   not claim otherwise. And **README `:730` already documents this orphan
   class** -- "attachments left behind by the encoding change" found via
   `attachment-list` -- so cost 3 has precedent to point at rather than an
   argument to make.
10. `docs(plans)`: a correction note on `_plans/025`, which is where "nothing
    depends on today's flat-in-the-root behaviour" and the collision refusal are
    asserted. Amending a landed plan is within convention here (026 has six
    commits, 021-023 two each), and leaving the document that states the wrong
    thing unmarked is how the next reader re-derives it.

**Tree export**

11. `refactor`: `slugify` moves out of `cmd/export` into a package all three
    commands can reach. It stops being export's private helper the moment
    `read` and `attachment-download` position attachments by the same rule.
12. `feat(pagedoc)`: the page's position through `Options`/`StorageOptions`,
    applied by `sourceFor` to sourced and unsourced attachments alike. This is
    the commit the L5 flip depends on; building anything downstream on an
    unsourced-only prefix bakes in `IMAGE BROKEN` for every shared asset below
    the root.
13. `feat(attachfile)`: page-scope an attachment with no recorded path. `--flat`
    is unchanged and becomes the documented opt-out.
14. `feat(read)`: page-scoped positions, from the page's own slug.
15. `feat(attachment-download)`: page-scoped by default. The behaviour change to
    a shipped command gets its own commit rather than riding inside an export
    one, and it is not free: the command fetches only `ListAttachments` today
    (`attachmentdownload.go:76`) and a slug needs the page's title, so this adds
    a `GetPageOrNil`. If that fetch fails the run fails before anything is
    written (`operationalFail`) -- half the attachments scoped and half not is
    worse than none. A folder id, which `pageref.Resolve` accepts, has a title
    too and scopes the same way.
16. `feat(export)`: `--depth`, the walk, the mirrored layout, `parent:` paths,
    the `--file` refusal when multi-page, `Args` relaxed from `ExactArgs(1)` to
    `MaximumNArgs(1)` for `--space`, and completion for both new flags
    (`completion.Values` for `--depth`, `cobra.NoFileCompletions` for `--space`,
    exactly as `cmd/children/children.go:57` does) -- without which
    `TestSubcommandsCompleteArgs` in `cmd` fails --
    including the sentence in `fix`'s help that an exported `parent:` path is
    reconciled back to a numeric id, which belongs with the commit that starts
    emitting such paths rather than with a docs commit.
17. `feat(export)`: `--space`, requiring an explicit `--depth`.
18. `feat(export)`: `markfluence.yaml` at `dest`, and `roots`.
19. `feat(export)`: destination-conflict detection across pages.
20. `feat(export)`: the slug-collision pre-flight and its `-<id>` suffixing.
21. `feat(export)`: skip a rendered page whose file already exists.
22. `feat(export)`: the multi-page summary, `parent_file`, and the schema. The
    summary stops being the `map[string]int` at `export.go:311` and becomes a
    typed struct, since `project_file` is not an int -- `Envelope.Summary` is
    `any` (`jsonout.go:53`), so this costs nothing but must be said, and
    `schematest`'s no-`omitempty`, every-field-typed rules apply to it.
23. `docs`: the tree feature. L5/L6 to Holds (gated on commit 24), README's
    §`export` for `--depth`/`--space`/the layout/the suffix rule, its §`read`
    and §`attachment-download` for the shared placement rule and `--flat`,
    CLAUDE.md's `cmd/export`, `internal/pagetree` and
    `attachment{list,upload,download}` paragraphs, and `pagedoc`'s package
    comment -- where byte-identity is *narrowed* to a unique-slug page, not
    retired.

    Code comments travel with their commits rather than landing here:
    `attachname.go`'s file rationale in commit 1, `client.go:1062` in commit 5,
    `attachfile.go`'s package comment and `Resolve` in commit 13,
    `storage_to_md.go`'s `sourceFor` in commit 12. A doc commit that sweeps up
    comments for code changed eight commits earlier is a doc commit nobody can
    review.
24. `test(convert)`: the L5 property test the status flip rests on. Last, so it
    is written against the finished behaviour, but it gates commit 23 rather
    than decorating it -- if it cannot be made to pass, L5/L6 stay Partial and
    commit 23 says so instead.

## Verification

Commits 14 and 15 change two shipped commands, so they get their own coverage
rather than riding on export's: a `read` output assertion showing an unsourced
attachment's destination is now page-scoped, and an `attachment-download`
path-set case covering both the scoped default and `--flat`. The live pass runs
all three commands against the fixture, since the point of the one-rule change
is that their outputs agree.

A **path-set test** per fixture tree in `cmd/export` against a fake server:
assert the complete set of written paths, so a placement regression reads as a
diff of the tree rather than as one changed string. Plus the usual unit coverage
for the depth vocabulary, the pre-flight, the conflict rule, and `parent:`
emission.

Then **live**, against the personal space (76646426): a standing three-level
fixture -- a root page, a child page under a folder, a grandchild; one native
attachment with no recorded path; one shared asset referenced from two pages at
the same recorded path; a deliberate near-collision pair of titles ("Deploy:
Prod" and "Deploy Prod", which must come out as two suffixed files rather than
one file or an error). Kept standing
so later work re-verifies against it. Folder children and attachment comment
shapes are exactly where fake servers have been wrong before.

The end-to-end check is L5 itself: export the fixture subtree, then `update` it
back unedited and confirm no change -- which also proves the marker file is doing
its job, since without it every shared asset would come back `IMAGE BROKEN`.

Expect one exception on the first cycle, or the verification reads as a failure:
an unsourced native attachment is unmanaged, so `meta.SHA256 ""` never equals a
real sum (`client.go:1052`) and the first `update` restamps it once with a
comment. Cycle two is the fixed point. "No change" is the claim about cycle two
onward, and the run that proves L5 is the second one.

### The L5 property test (commit 24)

A live pass proves the fixture round-trips; it does not prove the Law, and
`docs/guarantees.md` now says so explicitly. So the status flip needs a test
that generates rather than enumerates: storage in, `StorageToMarkdown`, then
`MdToConfluence`, asserting the storage that comes back is semantically what
went in.

`internal/convert/storage_to_md_test.go` already has four narrower versions of
this shape to build on -- `TestRoundTripStableCallouts`, `TableAlignment`,
`TableCellBG`, and `TestRoundTripPassthrough`. The last one is the closest and
also the warning: `d86bec4` had to add a case to its hardcoded list because a
construct that existed was simply absent from it, which is the enumeration
failure #125 came through. The new test takes its corpus from the regression
suite's own cases rather than a hand-kept list, so a case added anywhere is
covered here by construction.

Known exclusions belong in the test as named skips with reasons, not as silent
gaps: a table cell background outside the twenty-one swatches, and a column
alignment that is per-paragraph in storage but per-column in GFM.

## Out of scope

- **`--clean`**, for a tree whose pages were deleted upstream. Removal is bound by
  **S4**-**S6**; making them non-vacuous is its own work.
- **Orphan cleanup** after the naming switch. Same reason, and it wants the same
  spec.
- **A warning when a pre-existing local file differs from the recorded checksum.**
  The local file is the user's working copy; export is not a sync tool.
- **Stripping `page_id`** to republish a tree elsewhere.
- **Concurrency.**
