# 053: update moves a page to its declared parent

Answers #10. `update` publishes a file's body, title, width, labels and status,
but ignores its `space` and `parent`: it takes the space from the live page and
never looks at the parent. That breaks **L9** (`declared-metadata-is-asserted`)
for the two coordinates, and `diff` has to say so in its help ("update does not
move a page to a different space or parent"). This plan makes `update` assert a
declared `parent` by moving the page, and refuse a declared `space` that the
page is not in.

Two parts of the issue are obsolete: `fix` was removed in #151, and
`update --dry-run` exists.

## What the probe established

**Verified 2026-09-25** on scratch pages under `markfluence roots smoke test`
in the personal space, through the gateway, with a personal token. Every probe
page was trashed afterwards.

| probe | result |
|---|---|
| v2 `PUT /pages/{id}`, version+1, same body, new `parentId` | 200. The page moves, **and its version goes up by one**. It lands last among the new parent's children. |
| the same, version unchanged | 409 `Version must be incremented`. A v2 move always bumps the version. |
| v2 `PUT`, version+1, `parentId: null` | 200, and nothing changes. **v2 cannot move a page to the top of a space.** |
| v2 `PUT`, version+1, body and `parentId` both changed | 200, one version bump, both applied. |
| v2 `PUT`, version+1, nothing changed | 200, and the version does **not** go up. |
| v1 `PUT /rest/api/content/{id}/move/append/{pageId}` | 200 `{"pageId": ...}`. Moves under a page, last. **Version unchanged.** |
| v1 `move/append/{folderId}` | 200. Moves into a folder. Version unchanged. |
| v1 `move/after/{rootPageId}` | 200. Moves to the top of the space (`parentId` null). Version unchanged. |
| a page with a child, moved by either route | The child moves with it. |
| target is the page's own descendant (v2) | 400 `Cannot move the content here as it creates a parent-child loop.` Nothing changes. |
| target does not exist (v2) | 404 `Cannot find content with id [1]`. Nothing changes. |

**Not verified:** whether a move in the Confluence UI bumps the version (it
probably does not, like the v1 route), and whether v1 `move` needs a scope
beyond `write:confluence-content`.

## Decisions

**D1. A space mismatch fails the file, and nothing moves between spaces.** The
declared space is the file's `space:` (frontmatter or `pages:` entry), **or
the project's `space:` default** in `markfluence.yaml` when neither declares
one. If the page is in another space, the file fails before any write, with
`CodeValidation`. The error names the page's space, the declared space, and
which location declared it (`pagemeta.Origin`, or the project default), and
says that markfluence does not move pages between spaces: move the page in
Confluence, or correct the declaration.

The project default counts because a project that says `space: ENG` and a page
somewhere else is a mistake whose intent markfluence cannot know. A move across
spaces is out of scope: it needs write access to both spaces (possibly admin),
it moves the whole subtree, it can change permissions, and the one workflow
raised (drafts in a personal space, then overwriting pages in a team space)
changes the page ids and so is not a move at all.

**D2. A declared `parent` is asserted by moving the page.** The comparison is
of resolved ids, as `diff` already does:

- an id is used as given (a page or a folder);
- a `.md` path resolves to that file's `page_id`, read through the root the way
  `create` reads it; an unpublished parent file fails the file;
- `parent: null` (and every null spelling, `~` and an empty value included)
  means **the top of the space**;
- an **absent** `parent` leaves the page where it is (L9).

A key that is present with a blank value is a declaration. `pagemeta.Resolve`
already keeps it in `Fields` as `""`, so the test is whether the key is
present, not whether it is non-blank. `create` already reads a blank `parent`
as the top level, and `create` writes `parent: null` back for a top-level
page, so the two verbs agree. When the frontmatter says `parent: null` and the
`pages:` entry names an id, the id wins, since `pagemeta` treats a blank as
silence when two locations are compared.

The risk accepted here: a half-typed `parent:` moves the page and its subtree
to the top of the space. A move is easy to undo, unlike the status write L9's
Accepts refuses a blank for.

**D3. Every move uses the v1 move route.** It is a separate write that never
changes the page version, so:

- the moved-page check (S8) and the log are unaffected: the version the body
  `PUT` returns is still where the page ends up;
- the "body unchanged, skip the `PUT`" path (L4) is unaffected: a move-only
  change sends no body;
- it is the only route that reaches the top of the space.

The target:

- a page or folder parent: `move/append/{parentId}`;
- the top of the space: `move/after/{lastRootPage}`, the last of
  `ListSpaceRootPages` in position order. The homepage is a root page, so the
  list is never empty for a page that is not already a root.

**D4. A moved page goes last among its new siblings.** That is what `create`
already does and what the UI does for a new child page, it needs no extra
request for a parent, and it keeps a batch in its own order. markfluence never
reorders siblings after that. The help of `create` and `update`, and the
`parent` section of `docs/markdown-file.md`, say so and point at the
Confluence UI for reordering.

**D5. Order within `update`.** After the page fetch:

1. the space check (D1), before the divergence check, since a page in the
   wrong space is the more basic error;
2. the divergence check (S8), unchanged;
3. resolve the parent (D2) and, when it differs from the live one, check the
   target: it exists, it is a page or folder in the page's space (`create`'s
   `checkParentInSpace`), and it is not the page itself or one of its
   descendants (the target's ancestors, one request, only when moving);
4. the render and the idempotence check, unchanged;
5. `--dry-run` stops here and reports the move;
6. **the move**, before attachments and the body. A failed move fails the
   file, unlike a failed width or label write: those run after the page is
   published, while the move runs first and nothing else has been written.

A parent that cannot be resolved or checked fails the file before any write,
also under `--dry-run`.

**D6. Output.** A human line when a move happens:
`moved under "Runbooks" (placed last; reorder in Confluence if needed)`, or
`moved to the top of the space (placed last; ...)`, with "would move" under
`--dry-run`. `--json` gains a `moved` field on each result: `null`, or
`{"from": id|null, "to": id|null}`, with `null` meaning the top of the space.
The schema gets the field and conformance tests cover both shapes. A new key
is a compatible change since #200 (`_plans/054`, docs/json-output.md), so
`schema_version` stays 1.

**D7. `diff` follows.**

- `parent: null` becomes a declaration it compares (top of the space against
  the live parent), since `update` now acts on it.
- The space comparison includes the project default, to match D1.
- The notes "update does not move a page between spaces / to a new parent" go.
  The space note becomes "update refuses a page in another space". The help
  paragraph about neither field being changed is rewritten.

## Implementation

**`internal/client`:** `MovePage(pageID, position, targetID string) error`, a
v1 `PUT /wiki/rest/api/content/{id}/move/{position}/{targetID}` through
`send`. The positions used are `append` and `after`; the method takes the
position rather than having two methods, and refuses any other value. The
request is safe to retry, since a repeated move to the same place is a no-op.
Plus `Ancestors(pageID)` if nothing already reads them (v2
`/pages/{id}/ancestors`).

**A shared parent resolver.** `create`'s `resolveParent`/`checkParentInSpace`
and `diff`'s `parentPageID` already hold two copies of ".md parent → page id
through the root", and `update` would make a third. Move the shared part into
`internal/parentref`:

- `PageID(root, fileDir, ref) (string, error)`: read a `.md` parent through
  `root.FS` via `pagemeta`, refusing an escape and a symlink, and returning `""`
  for an unpublished parent;
- `Kind(c, id, spaceID) (string, error)`: `checkParentInSpace`, answering
  `"page"` or `"folder"`.

`create` keeps its own in-set and `--parent` handling on top. `diff` keeps its
lenient reporting by turning the errors into notes, as it does now.

**`cmd/update`:** D1, D5, D6. `updateResult` gains the move; `json.go` reports
it.

## Files

| file | change |
|---|---|
| `internal/client/client.go` | `MovePage`; `Ancestors` if needed |
| `internal/parentref/` | new: `PageID`, `Kind` |
| `cmd/create/create.go` | use `parentref`; help note on positioning (D4) |
| `cmd/update/update.go`, `json.go` | space check, parent resolution, move, output; help |
| `cmd/diff/frontmatter.go`, `diff.go` | D7 |
| `schema/json-output/v1.json` | `moved` on update results |
| `docs/confluence/api.md` | the probe table; replace the "Not verified" line about moves; the scope row for `move` |
| `docs/markdown-file.md` | `space` and `parent` rows and the `parent` section: what `update` does, and positioning |
| `docs/json-output.md` | `moved`, if it needs more than the schema says |
| `docs/commands/` | `make docs` |
| `CLAUDE.md` | `internal/parentref` bullet; `MovePage` in the client bullet; "Frontmatter-driven publishing" says where the move sits in `update`'s order |

## Tests

- **Space (D1):** a file declaring another space fails with no write; the same
  for a project default with no file declaration; a file declaring the page's
  own space passes; the error names the declaring location.
- **Parent (D2):** an absent `parent` makes no move request and no parent
  lookup; a matching parent makes no move request; a differing id, a `.md`
  path, a folder, and `null` each make one move request with the right
  position and target; `null` on a page already at the top makes none; an
  unpublished `.md` parent, a parent in another space, a missing parent, and a
  descendant each fail with no write.
- **Order (D3, D5):** a move-only change moves and skips the body `PUT` with a
  base present; the recorded version is unchanged by a move; a failed move
  fails the file with no attachment or body write; `--force` still moves.
- **Dry run:** reports the move, sends no move request.
- **`--json`:** `moved` null and set, validated against the schema.
- **`diff`:** `parent: null` against a page with a parent is a difference; the
  project default space is compared.
- **`parentref`:** the escape, symlink, unpublished and entry-held-`page_id`
  cases that `create` and `diff` each test today, once.

## Not in scope

- **Moving between spaces** (D1). It waits for a use case, and the one raised
  is not a move.
- **Managing sibling order.** It would need a declared order, since L8 rules
  out inferring one from file names or directories, and under L9 every
  declared order is asserted, so a reorder done by hand in the UI would be
  undone by the next publish of any sibling. Nobody has asked for it.
- **Detecting a move done by hand in the UI.** A file with a stale `parent:`
  moves the page back without refusal, because a move (probably) does not bump
  the version the divergence check reads. That is L9 working as written, and
  it is how labels already behave.
- **Moves in `create`.** `create` places a new page where the file says, which
  is already correct.

## Amended during implementation

- **No ancestors route.** D5's loop check was to read the target's ancestors
  (v2 `GET /pages/{id}/ancestors`, or `/folders/{id}/ancestors`). Both need
  `read:content.metadata:confluence`, which no other markfluence call needs
  and the documented token does not carry. `parentref.Within` walks the
  target's `parentId` chain through the page and folder routes instead: a
  request per level, only when a move is about to happen.
- **`parentref` is `Locate`/`PageID`/`Resolve`/`Lookup`/`Within`**, not
  `PageID`/`Kind`. `create` needs to stop between locating a `.md` parent and
  reading it (an in-set parent is known without a read), and `update` needs
  the target's title and parent, not only its kind.
- **`pagemeta.Resolved` gained `Space(root)` and `Parent()`**, so `update` and
  `diff` read the two coordinates, and the project default, through one call.
- **Verified end to end** on 2026-09-25 against the personal space with the
  built binary: a matching parent skipped; a dry run previewed a move and
  wrote nothing; a move under a page, to the top (`parent: null`), and back
  under a `.md` parent each moved the page with the version unchanged and the
  body `PUT` skipped; a parent below the page was refused as a loop; a space
  declared in the file, and one from the project default, were each refused;
  `diff` reported the project-default space.
