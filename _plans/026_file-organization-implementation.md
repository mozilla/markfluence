# Plan: implementing the root model (025)

[025](025_file-organization.md) is a spec: it establishes what "the root" means,
why one root fixes L1/C1/L2/L3/S2 and partially fixes L5/L6, and what it costs.
This plan sequences that model into landable commits. Everything here was
resolved by interview before writing any code; where this plan disagrees with
025's wording, it's because 025 left the point open (item 10's comment format)
or because implementing it surfaced a distinction 025's prose didn't need to
draw (two discovery passes, not one).

All commits land on `file-org-fixing`, one PR at the end.

## Out of scope (deliberately)

- **Multi-page export** (025 item 16, use case 8). Tracked separately as #59.
  Nothing in this plan depends on it: once a commit here makes an image's
  recorded `Source` root-relative, single-page export's Scenario D already
  round-trips, because `attachfile.Resolve`'s `dest + source` join no longer
  escapes. That's a side effect, not a reason to fold #59 in here.
- **A dedicated `check` command.** 025 gestures at a standalone diagnostic that
  audits a whole tree without publishing and distinguishes *why* a reference
  didn't resolve (missing file vs. no `page_id` vs. outside-root) with tailored
  messages. That's a bigger, separate feature. What is *not* out of scope: see
  R1 below — the existing `r.warnings` mechanism already reports an unresolved
  image, and commit 5 extends it to links too, which is a small addition once
  the link index exists rather than a new subsystem.
- **Any hook/execution system.** 025's "Hooks" section is explicitly future work.
  This plan reads `markfluence.yaml` for its path only; nothing in it is parsed.
- **Generating `markfluence.yaml` for the user.** No `init` subcommand here —
  that's #5. Users create the file by hand for now; its existence is its whole
  meaning.
- **Refusing a batch that spans more than one discovered root.** See below —
  this is allowed, not an edge case to guard against.

## Terminology, settled

025 uses "the root" for one idea that implementation splits into two, because
they're discovered differently and used for different things. Getting this
wrong would mean either weakening S2's fix (Scenario B) or misreporting what a
command actually did.

- **The root.** Discovered *per markdown file*: walk up from that file's own
  directory looking for `markfluence.yaml`; the first hit's directory is the
  root, and reaching the filesystem root with no hit means the file's own
  directory is the root. This is what bounds a file's image and `parent:` reads
  (S1/S2), what its attachment `Source` is recorded relative to, and what the
  link index is built from. It is reported — once per distinct value seen in a
  run, not once per file. When a `markfluence.yaml` exists and every file in a
  batch sits under it, every file resolves to the same root and there is
  exactly one value to report; that's the intended, common case.
- **The `.env` lookup.** A separate, narrower discovery pass: walk up from the
  **working directory** (not a file's directory) looking for
  `markfluence.yaml`; no hit means read `.env` from the working directory
  itself, as today. This exists solely to answer "where is `.env`" before any
  file has been touched — credentials are resolved once, up front, for the
  whole invocation, before per-file root discovery has even run. It is not
  called "root" anywhere and is not reported. `--env-file` still overrides it
  absolutely.

Both passes are one function, `project.Discover(startDir string)`, called with
two different `startDir` values and two different "no hit" fallbacks (the
file's directory vs. the working directory). There are not two algorithms.

**Multi-root batches are allowed.** Nested or sibling `markfluence.yaml` files
mean two files in one invocation can discover different roots (nearest
ancestor wins, the same rule `.editorconfig` uses). This is not special-cased:
each file's link index is scoped to its own root (a cross-root link simply
doesn't resolve — unresolved, not an error), a `parent:` escaping a file's own
root is a hard error even if the target is part of the same batch under a
*different* root, and root reporting naturally shows every distinct value used.
Refusing this outright would be extra code in service of a restriction nothing
requires.

**`--root`** overrides discovery for the whole invocation with one value,
applied uniformly — it is a persistent flag, not a per-file setting.

## Security review (025 item 6)

025 asks specifically to review walk-up discovery's security history before
building it, because "discovering a file must not, by itself, authorise
anything in it to run" is free today and expensive to retrofit. Two families
reviewed, both the same shape: a tool walks up from cwd, finds *something*, and
trusts it without checking who put it there.

- **CVE-2022-24765 / CVE-2022-29187 (git).** Pre-fix Git walked up looking for
  `.git` with no ownership check. On a shared machine, another user could plant
  `C:\.git` (or any ancestor `.git`) and have their config silently adopted by
  everyone's git commands run from below it — including hooks. The fix,
  `safe.directory`, is an ownership allowlist bolted on after the fact, and
  needed a second CVE to close a Windows path-handling bypass of that same
  check. 025's own "Hooks" section names this precedent already; the searches
  in this review confirm it's exactly the shape (walk-up discovery, then blind
  trust) and that the retrofit was not a one-shot fix.
- **Git submodule/hooks CVEs (e.g. CVE-2024-32002).** A related but distinct
  lesson: several git CVEs are about a *write* landing inside `.git/` (a
  submodule checkout escaping into the parent's `.git/`) rather than discovery
  itself, and the payoff is always the same — a hook file that executes on the
  next ordinary git operation. The lesson for markfluence isn't about
  discovery's read side here; it's a second data point that "a file found by
  walking a tree gets executed later" is the recurring failure, which is why
  025's constraint (discovery ≠ authorization) is worth holding even though
  markfluence has no hook system yet.
- **`.editorconfig`.** No CVE turned up specific to its walk-up discovery
  (its published CVEs are memory-safety bugs in `editorconfig-core-c`,
  unrelated). Its discovery model is still the right one to imitate for
  *semantics* — walk up, nearest file wins, an explicit marker
  (`root = true`) stops the walk early — without inheriting a security
  incident, because there is nothing in an `.editorconfig` file that executes.

**What this means for `project.Discover`:** it only ever reads a filename to
decide where the root is; nothing in `markfluence.yaml` is parsed or executed,
matching the `.editorconfig` shape rather than git's pre-fix shape. The root is
always reported (visibility is the mitigation git's fix eventually converged
on anyway with `safe.directory`'s explicit allowlisting), and `.env` — the one
other thing discovery gates — is still read-only. If a hook system is ever
added, it must not be authorized merely by `markfluence.yaml`'s presence; that
already has a placeholder in 025 and isn't re-decided here.

## Commit sequence

### 1. `internal/project`: root discovery

`Discover(startDir string) (*Root, error)`. `Root` carries `Dir` (absolute),
`File` (path to `markfluence.yaml`, empty when none was found), and `FS
*os.Root` opened on `Dir`. Stats a filename at each ancestor rather than
listing a directory (needs only execute permission, which is guaranteed or cwd
itself would be unreachable); `EACCES` keeps walking rather than failing;
reaching the filesystem root with no hit is not an error. `Discover` does not
follow symlinks in the walk (`filepath.Dir` on an absolute, unresolved path);
that both matches 025's non-goal and is the reason discovery cannot be tricked
by a symlinked ancestor.

Tests: nested project files (nearest wins), no project file (fallback to
`startDir`), `EACCES` on an ancestor, filesystem-root termination.

### 2. `.env` location

`internal/client.loadEnvFile` calls `project.Discover(cwd)` (the second,
narrower pass above) instead of reading `./.env` directly. `--env-file`
unchanged — still absolute, still required-if-set. `dotenvPath` constant
becomes the filename joined onto the discovered directory rather than a
literal relative path.

### 3. `--root` flag

Persistent flag on `cmd/root.go`, directory completion via
`internal/completion`. When set, every per-file `project.Discover` call is
skipped in favor of a `Root` constructed directly from the flag value (still
opened as an `os.Root`, still validated to exist and be a directory).

### 4. Thread the root through the converter

- `convert.MdToConfluence` takes a `*project.Root` (per file) instead of
  calling `os.Getwd()`.
- `withinRoot`'s lexical `filepath.Abs`/`filepath.Rel` comparison is replaced
  by a read through `root.FS`. Mirrors `internal/attachfile`'s existing
  `os.Root`-scoped `Write` — that package already has the pattern this item
  copies, not invents.
- The image leaf refuses a symlink (`os.Lstat`, not `os.Stat`; anything that
  isn't a regular file is broken the same way a missing file is).
- `images.go` records `Source` root-relative instead of relative to the
  referencing file (025 item 3) — this is the change that fixes Scenario C and,
  as a side effect, Scenario D for single-page export.
- `cmd/create` and `cmd/update` discover a root per file, caching by resolved
  `Dir` across a batch so files sharing a root don't re-walk. The root actually
  used is reported once per distinct value (human output and `--json`).
- Regression suite: `test.input` gains an explicit root (default: the case's
  own directory, matching the no-config fallback); `images-shared-parent`'s
  golden changes, since that's the case 025 names as "the whole point."

### 5. Root-relative link index

New `internal/linkindex` package. `Build(root *project.Root) (*Index, error)`
walks the tree at and below `root.Dir` once (via `root.FS`, so it cannot
descend a symlink), building the page map and anchor map keyed by root-relative
path instead of by basename in one directory. `Index.SetPage(relPath string,
entry PageEntry)` overrides/injects an entry — the hook commit 8 needs.

`internal/convert`'s `docKey`, `buildPageMap`, `buildAnchorMap`,
`renderLink`/`rewriteDocLink` move from the per-directory, basename-keyed
lookup to a lookup against the passed-in `*linkindex.Index`. `create`/`update`
build (or reuse a cached) index per distinct discovered root and pass it into
every `MdToConfluence` call for files under that root.

**Minimal R1, folded in here.** `rewriteDocLink` already has a clean hit/miss
against the index for anything shaped like a same-tree `.md` reference (it
already exits early with no warning for hrefs that were never meant to
resolve — external URLs, non-`.md` targets, mentions). On a miss, append to
`r.warnings` the same way `images.go` already does for a broken image. This
closes Scenario E's link half (both the missing-file case and the
exists-but-no-`page_id` case, since a `page_id`-less file was never in the
index to begin with) using plumbing that already exists, and — not
incidentally — is what makes commits 4/5/8 verifiable by hand: a test run
against the Scenario A/F fixtures now says which links didn't resolve instead
of requiring an eyeball check of rendered HTML. A dedicated `check` command
(tree-wide audit without publishing, per-reason messages) stays out of scope;
see above.

`docs/guarantees.md`'s R1 moves from **Aspirational** to **Partial** in this
commit — not **Holds**, even though the guarantee's own wording ("every
reference... is reported") would technically be satisfied. Reserving **Holds**
for when a dedicated diagnostic exists, rather than claiming it the moment the
underlying mechanism happens to cover every case today.

Regression suite: cases for Scenario A (cross-directory link, same basename in
two directories) and Scenario F (a link that could traverse above the root,
now resolved as "not found" rather than accidentally safe by basename
flattening), plus Scenario E's two link cases asserting they land in
`r.warnings`.

### 6. S2 completion: the `parent:` read

`cmd/create.resolveParent` currently `os.Stat`s and reads a `parent:` `.md` path
with no bound at all. It now reads through the referencing file's `root.FS`;
a path resolving outside that root is a hard error (not "not found," not an
unresolved-and-reported case — 025 is explicit that a parent is load-bearing).
A symlinked `parent:` target is refused the same way a symlinked image is.
`cmd/create` gains the root handle it has no reason to hold today.

### 7. Attachment identity

- `attachment-upload` records a root-relative source (`internal/project`'s
  discovery from the uploaded file's directory) instead of
  `filepath.Base(f)`.
- Attachment comment format shortens to `markfluence: sha256=<32hex> path=…`
  (58 characters of overhead, 197-character path budget), closing #101 —
  keeps the
  self-describing `markfluence: ` ownership marker (what S5 rests on) and
  spends the bigger, cheaper lever (128-bit truncation of a checksum that only
  needs to detect a byte change, not resist an adversary) rather than the
  smaller, more expensive one (shortening the prefix). `parseAttachmentComment`
  already tolerates the legacy 64-hex form; both are accepted on read, only the
  short form is written going forward.
- `attachfile.Resolve`'s doc comment is reframed: the clamp is a guard against
  a maliciously-edited attachment comment (server data, always worth
  distrusting), not a rule about legitimate layouts, since a root-relative
  `Source` markfluence itself writes can no longer escape by construction. No
  functional change; the existing escape tests stay, because the threat model
  they guard against (a hostile comment) is unrelated to how markfluence's own
  writer behaves.

### 8. `create`'s three-phase restructure

Preflight (today's phase 1, unchanged) → **reserve** → **publish**.

- Reserve creates a content-less stub per file (title, parent, no body) in
  topological order (a page still needs its parent's id to be created), capturing
  each id. Unless `--no-persist`, the id is written back to frontmatter
  immediately, matching today. Each captured id is also fed into the shared
  `linkindex.Index` via `SetPage`, so publish sees ids the disk doesn't have yet
  under `--no-persist`.
- Publish converts every file (now against a fully-seeded index, so link
  direction and cycles both resolve) and updates each page's content, syncing
  attachments.
- `--dry-run` creates nothing in reserve; publish still runs for preview using
  whatever the index has (published parents resolve, in-set siblings don't have
  ids yet — same limitation `--dry-run` already has today, just relocated).

Heaviest test rewrite in this plan: `create_test.go`'s fixtures assume today's
single-pass create.

### 9. Docs

- README: path resolution rules, the root model, `--root`, where `.env` is now
  read from. Also a new subsection (near "Markdown page structure," the
  existing precedent for user-facing mechanics rather than Confluence-API
  evidence) walking through the practical recipes this model changes the
  answer to:
  - **Moving or renaming a markdown file.** Links to it resolve automatically
    via the root-relative index; nothing elsewhere needs editing.
  - **Moving a page's own images along with it (use case 9a).** The gotcha:
    this churns — attachments get re-uploaded under new root-relative names on
    next publish, and the originals strand until #99 (`attachment-prune`)
    exists. Called out explicitly because it inverts what today's behavior
    trained users to expect (today, this variant is the free one).
  - **Renaming or moving a shared asset.** Every page referencing it gets a new
    attachment name on next publish; the old ones strand the same way. This is
    L3 (identity-from-asset-location) directly: identity follows the asset,
    not the page.
  - **Setting up a shared assets directory across many pages.** Needs a
    `markfluence.yaml` to get one stable root shared by every page under it —
    without one, each page's root defaults to its own directory, and an asset
    above it is `IMAGE BROKEN` (025's "no-config default is stricter" behavior,
    Scenario B).
- A new doc (not under `docs/confluence/`, since none of this is
  Confluence-specific) describing the root model itself — discovery, the
  project file, S1/S2, link resolution — for a reader who hasn't seen 025.
  Stays conceptual; links to the README subsection above for the how-tos.

The recipe list above is a starting point, not a ceiling. Commits 1–8 will
surface scenarios worth a recipe that nobody thought of yet while writing this
plan — a fixture in the regression suite that took an extra argument to
explain, a test case for an edge in root/index caching, a failure message that
needed a "here's what to do about it" during manual verification. Flag those
as they come up rather than waiting for commit 9 to invent them from scratch.
- `docs/guarantees.md`: L1, C1, L2, L3, S2 move to their post-fix status in the
  commit that actually makes each one true (not deferred to this commit) —
  this entry is the final sweep, catching anything not already updated
  in-place. L5/L6 stay **Partial** — the single-page round-trip works as a side
  effect of commit 4, but 025's own framing ties full resolution to multi-page
  export (#59), and this plan defers to that framing rather than declaring an
  early win on a guarantee whose scenario table names both roundtrip
  directions. R1 moved to **Partial** already, in commit 5 — this sweep just
  confirms nothing here regresses it back.
