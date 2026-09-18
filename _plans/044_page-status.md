# 044 — `page_status` frontmatter

Closes #168.

Confluence shows a coloured lozenge next to a page title — `Rough draft`,
`In progress`, `Ready for review`, `Verified` by default. markfluence cannot
see it, set it, or report it.

This adds one frontmatter field, `page_status`, that controls the value.

Everything the design rests on was established against the live instance and is
recorded in [docs/confluence/page-status.md](../docs/confluence/page-status.md).
That file is the evidence; this one is the decisions. The three findings that
shaped the whole design, in one line each:

1. **The write takes an id, and a *name* it does not recognise silently creates
   a new state that no API route can delete.**
2. **The only trustworthy vocabulary route is per-page and behind a write
   scope.** The space-scoped one reports a state the space does not offer; the
   authoritative one is space-admin only.
3. **A state write bumps the page version**, unlike a label write.

## The word "status" is already taken twice

"Status" can mean a couple of things:

| the word | what it means | markfluence's name for it |
|---|---|---|
| content **state** (Atlassian's API term) | the lozenge | `page_status` |
| content **status** | `current` / `archived` / `trashed` | `info`'s existing `status` row, `Page.Status` |
| `markfluence status` | the tree-wide "has Confluence moved on" view | #148, unrelated and unbuilt |

We're choosing `page_status` as the field name because markfluence's frontmatter
already has `page_id` and `page_width`, and because "state" in a file
would collide with nothing a reader has ever seen in the UI — the UI calls it a
status. The *internal* package is `internal/pagestatus` for the same reason,
even though the routes say `state`, and its doc comment says so once.

## The field

```yaml
---
title: Deploy Runbook
page_id: 123456
page_status: Ready for review
---
```

A single-line scalar holding the status's **display name**, matched against the
space's own list. `update` and `create` assert it; `read` and `export` emit it;
`info` reports it; `diff` compares it. It may also live in a `pages:` entry in
`markfluence.yaml` (#139), like every other page field.

If it's omitted, then markfluence does nothing — read or write — with the page
status.

## Decisions

**The value is a name; the wire format is an id.** The file says
`Ready for review`, and markfluence resolves that against
`GET /content/{pageId}/state/available`, then `PUT`s `{"id": 3086974986}` and
nothing else. This is the single most important decision in the plan, and it is
a safety property rather than an optimisation: a `PUT` carrying a `name` the
server does not recognise **creates a custom state**, returns 200, and cannot be
undone through any API. Writing by id makes that outcome unreachable rather than
something validation has to remember to prevent. A name that resolves to nothing
is a local failure, before any request that could write.

Two smaller consequences of the same rule, both worth pinning with tests: the
`color` field is never sent (with an `id` present it is ignored, and its absence
is a 400 only on the create path — so a 400 mentioning colour means the id was
dropped), and an id that has gone away is a clean 404 naming it.

**The vocabulary comes from the page, not the space.** The two space-scoped
routes are the two that do not work: `GET /space/{key}/state` returned four
states for `AGILE` where the real answer is three, and
`GET /space/{key}/state/settings` — which is correct — 403s for anyone who is
not a space admin. So validation reads `GET /content/{pageId}/state/available`,
which an ordinary author can call, and which must be asked of the page the
status is going on.

**Superseded after implementation, by live testing with a second account**
(2026-09-18): this said "verified stable across four pages in one space, which
is what makes any page in the space a legitimate probe". That held for one
account and does not generalise. The list is per (caller, page) — the same
account saw four statuses on a page it had created and three on one in the same
space that it had not, and the write enforces it — and the route needs *edit*
permission on the page it is asked about. What changed as a result: there is no
vocabulary cache, and `create` resolves the name after the page exists rather
than against a probe. See [docs/confluence/page-status.md](../docs/confluence/page-status.md).

**Only `spaceContentStates` are valid; `customContentStates` are refused.** The
custom half of that response follows the **account**, not the space: a state
created by one person shows up as available on pages in spaces they do not own.
Validating against the union would make a committed file valid for its author
and invalid for a colleague — the same class of defect as a cache that changes
output (L2), arrived at from the other direction. So the valid set is the space
half, and a name matching only a custom state is refused *with that as the
reason*, since "it works for me" is otherwise an unresolvable bug report.

**The match is case-insensitive, and nothing is rewritten.** `ready for review`
resolves to `Ready for review`'s id. This departs from `labels`, where case is
repaired *and warned about* — and the reason the two differ is that labels sends
the author's string to the server, while this sends only an id. The file's
spelling is never transmitted, so a case variant cannot reach the page, cannot
fail to converge, and is not worth a warning. Two space states differing only in
case are refused as ambiguous, naming both.

Rejected: requiring an exact match. It buys nothing once the id is what travels,
and it costs an author who typed a plausible spelling an error instead of a
publish. The cost accepted with it: `read` and `export` emit the **canonical**
spelling, so a file that said `ready for review` gains a cosmetic difference
from a file markfluence generated. `diff` does not report it, because it
compares resolved ids (below).

**There is no way to clear a status from frontmatter, and that is deliberate for
now.** `labels: []` can mean "remove them all" because a sequence has an empty
spelling that is distinguishable from a null scalar. A scalar field has no such
spelling: `page_status:`, `page_status: ~` and `page_status: null` all read as
`""` (the frontmatter reader collapses every null form), which is
indistinguishable from an author who typed a key and stopped — the exact input
`labels` refuses a scalar for. So an empty value is a **validation failure**, not
an instruction, and clearing a status is done in the UI. A follow-up may add an
explicit spelling; inventing one now would be guessing at a need nobody has
stated.

**No project-level default.** `page_width` has one (#100) because a width is
house style — a whole tree wants the same one. A status is a claim about one
page's maturity, and a `markfluence.yaml` that set every page in the repo to
`Rough draft` would be asserting something false about most of them on every
publish. `Config` gains nothing; the field exists only per page.

**It is applied after the body, the width and the labels**, non-fatally: a
failure is a warning on an `ok: true` result, exactly as `pagewidth.Apply` and
`labels.Apply` failures already are. The page is published by then, so failing
the result would report a publish that happened as one that did not. Last in the
order because it is the newest and the ordering is otherwise arbitrary; the
human and `--json` field orders follow suit.

**The apply pass reads before it writes, and this is not an optimisation.** A
state write bumps the page version — `minorEdit: false`, empty message, an
ordinary-looking edit in page history. A pass that `PUT`s unconditionally would
add a version to every page on every run, for a field that had not changed.
`Apply` therefore reads `GET /content/{id}/state`, compares ids, and writes only
on a difference. Since "no state" arrives as a *missing* `contentState` key
rather than an empty one, absence is tested by presence of that object, not by a
zero value.

**Every request is gated on the field being declared.** A file with no
`page_status` produces **no state request at all** — no write and no read. This
is the same property `labels` pins for an absent key, and it is what keeps this
free for the trees that do not use it. Declared, it costs two GETs and at most
one PUT per page; the `state/available` read is cached per space for the run.

**`create` validates against the space homepage.** *Superseded — see the note
under "The vocabulary comes from the page" above.* The reasoning below was
right about the problem (#127: validating after the page exists leaves a
created page with no status) and wrong about the remedy, because it assumed the
answer was a property of the space. It is not, so there is no probe to use and
`create` resolves after the page exists, warning rather than refusing.

> Preflight has no page id yet, and validating after the page exists is the
> #127 failure mode. The space's `homepageId` is a page id in the target space
> that always exists, so preflight resolves the name there.

**`page_status` is a visible field in `pagemeta`, not a coordinate.** A
disagreement between frontmatter and a `pages:` entry warns and frontmatter
wins, like `title`/`page_width`/`labels`. It cannot publish over the wrong page,
which is what `coordinates` is for.

**`check` can validate its shape and not its value, and says so.** This is the
first frontmatter field whose vocabulary is per-space server state, and `check`
has no client, no credentials and no network by construction (#42). It reports a
**present-but-empty** `page_status` — a guaranteed publish failure under the
rule above, which is exactly the reasoning that makes present-but-empty `title`
its one exception — and reports nothing else. `check`'s `Long` gains a sentence
saying the name is unvalidated, because a green `check` otherwise implies the
status will publish.

**`info` grows a row and `diff` compares ids.** `info` is the browse path for
"what can I write here": it reports the page's current status and the space's
available ones, both best-effort like the width and labels rows it already has.
The human row is `page status` and the `--json` field is `page_status`, keeping
the existing `status` row on the lifecycle meaning. `diff` compares the
**resolved id**, not the spelling — `parent`'s precedent, for the same reason:
two spellings of one target are not a difference. A page-side read that fails is
`comparable: false` rather than a difference, per that command's rule.

**`read` and `export` omit the key when there is no status *or* the fetch
failed.** The labels rule, and here it needs no separate argument: with no
clearing spelling, an emitted empty value would not parse as an instruction at
all.

## Implementation

### `internal/client` — `state.go` (new)

A new file rather than a home in `client.go`, following `users.go`: the routes
are v1, the response shapes are theirs alone, and the per-page-vocabulary
finding belongs beside the code that depends on it.

```go
type ContentState struct {
    ID    int64  `json:"id"`
    Name  string `json:"name"`
    Color string `json:"color"`
}

// PageState reports the page's current state, or nil when it has none.
func (c *ConfluenceClient) PageState(pageID string) (*ContentState, error)

// AvailableStates reports the states the page's space offers. The response's
// custom states follow the account rather than the space and are dropped here.
func (c *ConfluenceClient) AvailableStates(pageID string) ([]ContentState, error)

// SetPageState sets the page's state by id. Nothing else is sent: a name the
// server does not recognise creates a state no route can delete.
func (c *ConfluenceClient) SetPageState(pageID string, stateID int64) error
```

No `ClearPageState`. The `DELETE` route exists and is documented, but nothing
can reach it until a clearing spelling does, and an unused write method is a
loaded gun.

`PageState` decodes into a struct with a `*ContentState` field so a missing
`contentState` key stays distinguishable from a zero-valued one. The `PUT` is
retryable as an ordinary idempotent method — unlike `UpdatePage` it carries no
version, so a re-sent request after a lost response sets the same id again and
needs none of `updateLanded`'s machinery.

Scopes: `read:confluence-content.summary` and `write:confluence-content`, both
**already in markfluence's union**, so a working token needs nothing new. The
stale sentence in `api.md` claiming `write:confluence-content` "buys nothing but
the two label writes" gets fixed in the same change.

### `internal/pagestatus` (new)

Modelled on `internal/pagewidth`, minus the vocabulary constants, which are
per space and therefore not constants:

```go
func Declared(fields map[string]string) (name string, declared bool, err error)
func Resolve(c *client.ConfluenceClient, pageID, name string) (client.ContentState, error)
func Read(c *client.ConfluenceClient, pageID string) (*client.ContentState, error)
func Apply(c *client.ConfluenceClient, pageID string, state client.ContentState) ([]Action, error)
```

`Declared` is offline: it reports the trimmed name, refuses a present-but-empty
value, and is what `check` calls. `Resolve` does the name→state lookup, owns the
case-insensitive match, the ambiguity refusal, and the error message carrying
the available names — the message is here, in one place, because `update`,
`create` and `check`'s documentation all quote the same list. `Apply` is the
read-compare-write pass and reports an empty `[]Action` when the state already
matches, which is what the callers report as "unchanged".

A `Cache` for the per-space vocabulary, threaded from the caller the way
`project.Cache` and `pagedoc.UserCache` are, so a batch resolves each space once.
Keyed by space id, not page id — the answer is a property of the space, and
keying by page would re-request for every file in a tree.

### Commands

- `update`: `Declared` → `Resolve` → `Apply`, after labels, warnings on failure.
- `create`: `Resolve` in preflight against the space homepage, `Apply` in the
  publish phase. A preflight failure is `CodeValidation` — a bad name is a
  property of the file, not of the conversion.
- `check`: the present-but-empty report only, plus the `Long` sentence.
- `info`: the current state and the available list, best-effort.
- `read`, `export`: emit `page_status:` via `pagedoc`, omitted on none or error.
- `diff`: compare resolved ids, `comparable: false` on a failed read.

### `internal/frontmatter`, `internal/project`, `internal/pagemeta`

- `scalarFields` gains `page_status` (it needs the single-line scalar guarantee
  like the other five). `fieldOrder` does **not** change — it holds only the four
  coordinates, and `page_status` sorts alphabetically after them, landing just
  before `page_width`.
- `project.entryFields` gains `page_status` as `kindScalar`, and
  `IsPageField` follows. Structure only: #139 requires a semantically bad entry
  to be reported when its file is an argument, and this package cannot know
  which files those are — nor can it import `pagestatus`, which imports
  `client`, which imports `project`. Same wall `pagewidth` is on the far side of.
- `pagemeta` grades it as visible: warn, frontmatter wins.

### `--json` and the schema

`update`/`create`/`info`/`read` results gain a `page_status` field, `null` when
the file declares none or the page has none — the `labels` convention. `info`
also gains `page_status_available`, an array, `null` when the fetch failed.
Every field lands on the existing typed structs with no `omitempty`, per
`internal/schematest`'s rule, and each command's conformance test builds its
document with the command's own builder.

## Files

| file | change |
|---|---|
| `internal/client/state.go` | new |
| `internal/pagestatus/pagestatus.go` | new |
| `cmd/{update,create,check,info,read,export,diff}/` | field handling and `--json` |
| `internal/frontmatter/frontmatter.go` | `scalarFields` |
| `internal/project/{config,pages}.go` | `entryFields`, `IsPageField` |
| `internal/pagemeta/pagemeta.go` | visible-field grading |
| `internal/pagedoc/pagedoc.go` | emit the key |
| `internal/jsonout/types.go`, `schema/json-output/v1.json` | the new fields |
| `docs/confluence/page-status.md` | **written with this plan** |
| `docs/confluence/{README,api}.md` | index entry; the stale scope sentence |
| `docs/markdown_file.md`, `docs/json-output.md`, `docs/guarantees.md` | the field, the fields, L9's note |
| `CLAUDE.md` | the new package and client file |

## Tests

- **`Resolve` never sends a name.** The stub server fails the test if a `PUT`
  body carries anything but `id`. This is the safety property; it gets a test of
  its own with the finding quoted in a comment.
- An unmatched name fails **before any request that could write**, and the error
  carries the space's available names.
- A name matching only a custom state is refused, with the account-scoped reason.
- Case-insensitive resolution; two states differing only in case are ambiguous.
- A present-but-empty value is refused, by `Declared` and through `check`.
- **An absent `page_status` makes no state request at all** — asserted on the
  stub's recorded request list, not on the absence of a write.
- `Apply` with the state already set records **no `PUT`**, which is the
  version-bump property.
- A missing `contentState` key reads as no state, distinct from a zero value.
- `create` resolves against the space homepage, and a bad name aborts the batch
  with nothing reserved.
- `pagemeta`: a frontmatter/entry disagreement warns and frontmatter wins.
- `diff`: a case variant is not a difference; a failed read is `comparable:
  false` and not a difference.
- `read`/`export` omit the key for no status and for a failed fetch.
- Schema conformance for every command whose result grew a field.

## Guarantees

**L9** (`declared-metadata-is-asserted`) covers the new field as written:
declared is asserted, omitted leaves the page alone. Its status does not change
— it is `Partial` for `create`'s default `page_width`, and nothing here touches
that.

The honest gap to record under it: `page_status` can be **set and changed but
not cleared** from a file, because a scalar has no unambiguous empty spelling.
That is a missing *declaration*, not an unasserted one, so L9 is not weakened —
but a reader comparing it against `labels: []` deserves to find the reason
written down rather than infer it.

## Not in scope

- **A command that lists the available statuses.** The validation error carries
  the list and `info` shows it, which covers the two ways an author actually
  hits the question. A dedicated command would have to be named around #148's
  `status`, and would take a PAGE to answer a question about a SPACE, because
  the space-scoped routes are the broken ones. The trigger to revisit: authors
  first meeting the field while writing a *new* file for `create`, where there
  is no page to inspect and `info` on the parent is the awkward workaround.
- **A spelling that clears a status.** Above.
- **A project-level default.** Above.
- **Draft pages.** Every probe used `?status=current`; markfluence has no draft
  story and adding one is not this change.
- **`GET /space/{key}/state/content`** (pages carrying a given state). A real
  query — "what is still a rough draft" — and a plausible fit for #148's
  tree-wide view, which is where it should be considered rather than here.
- **Creating or configuring space states.** markfluence asserts a page's status;
  administering a space's vocabulary is a space admin's job, and the one route
  for it is admin-gated anyway.
