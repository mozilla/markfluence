# Plan: `children --space`

Let `children` list a whole space, given its key. Closes #98.

## Current state of the codebase

#98 reports that there is no first-class way to enumerate what a space
contains. Every existing path is blocked or indirect:

- `children PAGE` goes through `internal/pageref.Resolve`, which accepts a
  numeric id, a page/folder URL, or a `.md` file with a `page_id`. A space key
  is none of those, so `children ENG` fails with pageref's own "not a numeric
  id, a Confluence page or folder URL, or a markdown file with a page_id".
- `search --space ENG --limit all ""` is refused locally: an empty query is a
  validation error, because the API answers one with a 500
  (`docs/confluence/search.md`).
- `find` needs an exact title, so it cannot enumerate anything.

The only working invocation is the raw-CQL escape hatch:

```
markfluence search --cql 'space = "ENG" and type = page' --limit all
```

which is flat, folder-blind, and requires knowing CQL — for the single most
common browse task a Confluence wrapper has.

What already exists and is reusable:

- `internal/pagetree.Walk(c, rootID, maxDepth)` walks pages *and* folders
  under one node, depth-first, siblings merged by `extensions.position`,
  guarded by a visited set. It is already a package rather than command-local
  because listing and exporting a subtree must share it.
- `internal/pagetree.siblings` gets a node's children from the two v1 routes
  (`/child/page`, `/child/folder`) and sorts the merged slice by position.
- `client.ChildNode` is the row shape both routes return, needing no `expand`:
  `id`, `type`, `title`, `status`, `extensions.position`, `_links.webui`.
- `client.listV1` pages through a v1 collection by `start`/`limit` offset.
- `client.ResolveSpaceID` maps a space key to a space id, or `""` for unknown;
  `client.ErrSpaceNotFound` is the sentinel `find` raises and `search` maps to
  `space %q not found`.
- `cmd/children` renders the walk as a `TYPE`/`ID`/`TITLE` table indented by
  depth, and emits one `jsonChildResult` per node under `--json`.

## What was verified live (2026-08-31)

Against `mozilla-hub.atlassian.net`, basic auth on the site domain. The
evidence goes into `docs/confluence/spaces.md`; the summary here is what the
design rests on.

1. **`GET /wiki/rest/api/space/{key}/content/page?depth=root`** returns the
   space's root pages, and a bare row carries exactly the `ChildNode` fields —
   `id`, `type`, `title`, `status`, `extensions.position`, `_links.webui`. So
   `listV1[ChildNode]` works on it unchanged.
2. **A space can have more than one root page.** `AIM` has two: the homepage
   (`Africa Innovation Mradi Home`) and `What is Africa Mradi?`. This is the
   finding that decides the design: "the space root is its homepage, so walk
   from `homepageId`" would have silently dropped a whole subtree, which is the
   same class of wrong answer as v2 `/children` omitting folders.
3. **Archived root pages are excluded** from that route. The personal space has
   an archived parentless page (`Some Archived Page`); `depth=root` reports
   only the homepage. That matches how v1 child listings already behave, so no
   status filter is needed.
4. **An unknown key answers 404**, body
   `No space found with key : NOSUCHSPACEXYZ`.
5. **There is no route for a root-level folder** — `/space/{key}/content/folder`
   answers 500 (`NullPointerException: PageRequest should not be null`), there
   is no `/api/v2/spaces/{id}/folders`, and `/api/v2/folders?space-id=` is a
   500. And it does not matter: **`POST /wiki/api/v2/folders` with a `spaceId`
   and no `parentId` puts the folder under the space homepage**, reporting
   `parentId: <homepageId>`, `parentType: "page"`. A folder appears not to be
   able to sit at a space root at all. (Probe folder created and trashed.)
   This resolves the "a folder at a space root" bullet under *Unverified* in
   `docs/confluence/folders.md`.
6. **`GET /wiki/api/v2/spaces/{id}/pages`** is a flat cursor-paginated list of
   every page in a space carrying `parentId`/`parentType` — one request for the
   34-page personal space, where a walk needs a request pair per node. It is
   *not* used, for two reasons: it lists no folders (so a folder's title is
   unrecoverable and a subtree under a folder cannot be placed), and it
   includes archived pages by default.
7. `/wiki/api/v2/spaces/{id}/direct-children` answers 400 with
   `Provided value {spaces} for 'hierarchical-content-type' is not the correct
   type`, which says the route is `/{hierarchical-content-type}/{id}/direct-children`
   and there is no spaces variant. Recorded so it is not tried again.

## Decisions

**Surface: `children --space KEY`, with `PAGE` becoming optional.** Not a new
command: the output shape, the `--depth` vocabulary, the `--json` result type
and the schema branch are all identical, so a second command would buy a
duplicate. Not a fourth `pageref` spelling either: `pageref.Resolve` returns a
*page id* and holds no client, so it cannot resolve a key at all, and a bare
word is exactly what it currently rejects cleanly.

`Args` becomes `cobra.MaximumNArgs(1)`, and exactly one of `PAGE` / `--space`
is required. Both, or neither, is a validation-fatal error (exit 2) raised
before credentials are resolved.

**Depth 1 is the space's root pages.** The homepage is normally the only row
at depth 1 and its children are depth 2. The alternative — starting one level
down so the default is immediately useful — was rejected: it never lists the
homepage at all, it cannot represent AIM's second root, and its depth numbers
disagree with `children <homepage-id>`.

**A hint line when `--depth` was left alone.** Since most spaces have a single
root page, the default invocation prints one row, which reads like the whole
answer. When `--space` was given and `--depth` was not (`Flags().Changed`),
human output gets a trailing `ui.Info` line naming `--depth 2` and
`--depth all`. It costs no extra request and is absent from `--json`, where a
consumer is not reading prose.

**An unknown key is a hard error, via `ResolveSpaceID`.** The space id is then
unused — the v1 root route takes the key — so this is one deliberately wasted
request. It buys the same wording a mistyped key already gets from `find` and
`search`, and it keeps the auth-404 trap where it is already handled: a
rejected credential answers 404 on every v2 route, and translating the v1
404's Spring exception text into "no such space" would reintroduce exactly the
misreport `client.RejectedCredential` exists to prevent.

**`--space` is a key, not a URL.** `--space` means a space key on `find` and
`search`; making it mean two things on one command out of three is worse than
rejecting a pasted URL with `space "…" not found`.

**`parent_id` is `null` for a root page.** `childrenResult.parent_id` widens
from `string` to the existing `stringOrNull` `$def`. `pagetree.WalkSpace`
passes `""` as the parent of a root page and `jsonChildResult`'s existing
`nullable()` turns it into JSON `null`. The space id was rejected: that field
has only ever held a content id, and nothing in the row would say which id
namespace it came from.

**Nothing new about walk cost.** `--space KEY --depth all` is a request pair
per node, which `children ID --depth all` already is; retry/backoff and the
retry logger already cover the failure modes, and any cap would be a number
invented here. The help text says walking a whole space is one request pair
per node.

**No new guarantee id.** "A listing never silently omits a node kind" is
already doctrine in `docs/confluence/folders.md` and `CLAUDE.md` and is
enforced by the walk. `guarantees.md` ids are permanent, so minting one is its
own decision, not a rider on this feature.

## Implementation

### `internal/client`

```go
// ListSpaceRootPages lists the pages at the root of a space, by key.
func (c *ConfluenceClient) ListSpaceRootPages(spaceKey string) ([]ChildNode, error)
```

`listV1[ChildNode](c, "/wiki/rest/api/space/"+url.PathEscape(spaceKey)+"/content/page", url.Values{"depth": {"root"}})`.

Notes to carry as comments: the `/content/page` path form rather than
`/content`, because the latter also returns blogposts and `children` is about
the page tree; a space can have several root pages, so this is not a
homepage lookup by another name; and there is deliberately no folder companion
call, since a folder cannot sit at a space root (`docs/confluence/spaces.md`).

### `internal/pagetree`

Extract `Walk`'s inner recursion so both entry points share it, then:

```go
// WalkSpace walks a space's tree from its root pages, by space key.
func WalkSpace(c *client.ConfluenceClient, spaceKey string, maxDepth int) ([]Node, error)
```

Root pages become depth-1 `Node`s with `ParentID: ""`, sorted by
`extensions.position` with the same stable sort `siblings` uses (so two roots
come back in the order Confluence shows them), then each is descended exactly
as `Walk` descends a child. The visited set starts empty rather than seeded
with a root id, and root pages are added to it.

### `cmd/children`

- `Use: command + " [PAGE]"`, `Args: cobra.MaximumNArgs(1)`.
- `--space` flag, completion `cobra.NoFileCompletions` (a space key lives on
  the server, and completion may never call Confluence).
- Validation order, all before `client.Resolve`: exactly-one-of PAGE/`--space`,
  then `--depth`.
  - neither: `no page given: pass a PAGE or --space KEY`
  - both: `PAGE and --space cannot be combined: --space lists a whole space`
- On the `--space` path: `ResolveSpaceID` → `""` means
  `space %q not found` (VALIDATION, exit 2); then `pagetree.WalkSpace`.
- The hint line, human output only, when `--space` is set and
  `!cmd.Flags().Changed("depth")` and at least one row was printed.
- `Long` gains the space paragraph: what depth 1 means, that `--depth all`
  walks the whole space at one request pair per node, and that `--space` takes
  a key.

### `schema/json-output/v1.json`

`childrenResult.parent_id` → `{"$ref": "#/$defs/stringOrNull"}`, description
noting `null` for a page at a space root. `jsonChildResult.ParentID` becomes
`*string` through the existing `nullable()`.

## Tests

- `internal/client`: `ListSpaceRootPages` against a `clienttest` server —
  route and `depth=root` asserted, a `~personal` key path-escaped, offset
  pagination followed.
- `internal/pagetree`: `WalkSpace` with **two** root pages (the AIM shape),
  a folder under a root page (so the descend-into-folders rule is exercised
  from a space seed), `ParentID == ""` on roots, depth numbering, and
  `maxDepth` honoured.
- `cmd/children`: `--space` happy path; neither-arg and both-arg validation
  errors (message and exit 2); unknown key → `space "X" not found`; the hint
  line present at default depth and absent with `--depth all` and under
  `--json`; `--json` root row carries `parent_id: null`.
- `internal/schematest` conformance: the `children` envelope with a null
  `parent_id` validates.
- `make check`.
- One live run pasted into the PR body: `--space AIM` (multi-root) and
  `--space '~60c36d0718e9f60071326951' --depth all`.

## Docs

- `docs/confluence/spaces.md` — new, holding the seven findings above with the
  requests and responses that produced them.
- `docs/confluence/folders.md` — the *Unverified* "a folder at a space root"
  bullet becomes a verified statement, pointing at `spaces.md`.
- `README.md` — the `children` section: usage line, `--space` flag, examples
  (`--space ENG`, `--space ENG --depth all`, the `--json`+`jq` one-liner), and
  a note that a space's top level is usually just its homepage.
- `CLAUDE.md` — the `cmd/children/` bullet gains `--space`; the `cmd/search/`
  bullet's claim that `--cql`'s `Flags().Changed` is "the only use of it in
  `cmd/`" stops being true and must be corrected in the same commit.

## Commits

1. `docs(plans): plan children --space` — this file.
2. `docs(confluence): how a space root enumerates, and folders at a root` —
   `spaces.md` + the `folders.md` bullet.
3. `feat(pagetree): walk a space's tree from its root pages` — client +
   pagetree + their tests.
4. `feat(children): add --space to list a space by key` — command + schema +
   tests.
5. `docs: document children --space` — README + CLAUDE.md.

## Out of scope

- `export --space` / a multi-page export, which is #59. `WalkSpace` lands in
  `pagetree` rather than in the command so that work can use it.
- Teaching `find`/`search`/`children` to accept a space URL wherever a key is
  taken. Worth its own issue if the paste is a real annoyance.
- Any filter on what is listed (type, label, updated-since). `--json` plus
  `jq` covers it, and `search --cql` covers the rest.
