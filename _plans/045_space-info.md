# 045 — `markfluence space-info KEY`

Closes #170.

There is no way to ask markfluence anything about a *space*. `info` describes a
page, `children --space` lists a space's top level, and `find`/`search` look
inside one — but "what is this space, how big is it, is anything happening in
it, and can this token write to it?" takes four commands and some arithmetic.

This adds one read-only command that answers all of it:

```
$ markfluence space-info SRE
key:             SRE
name:            Site Reliability Engineering
id:              27852802
type:            global (current)
homepage:        27918392  Site Reliability Engineering
your access:     read
page statuses:   (not available -- see below)
pages:           1126 current, 40 archived
root pages:      2
last activity:   2026-09-15 (3 days ago)
pages created:   0 in the last 7 days
pages touched:   5 in the last 7 days (2 of them also created then)
```

Nothing is written, on the server or on disk, and no markdown file is involved.

## Who it is for

Three uses, and they decide what belongs here and how it is ordered:

1. **"Can this account read and write here?"** — the first thing a person or a
   CI job wants to know about a space it has been pointed at, and today the
   only way to find out is to try a publish and read the failure.
2. **"What are this space's own values?"** — page statuses above all, since
   #168 made `page_status` a field an author has to fill in with a name the
   space decides. There is nowhere else to learn one.
3. **Miscellaneous orientation** — how big, how alive, what shape.

So `your access` and `page statuses` come *first* in the output, above the
counts, even though the counts cost the most to produce. The command is
answering a question about permission and vocabulary that happens to also
report size, not a size report that happens to mention permission.

## The name, and renaming `info` to `page-info`

The plan is written for **`space-info`**, not `space-status`, because the word
is already overloaded three ways in this codebase — a page's *content status*
(`current`/`archived`), a page's *status* (the #168 lozenge), and `markfluence
status` (#148, the tree-wide local view). A fourth meaning is a cost with no
benefit: what this command reports is `info`'s question asked of a space, its
output is `info`'s label/value block, and `space-info` says exactly that.

**And `info` becomes `page-info` in the same change.** Once `space-info` exists,
a bare `info` is the odd one out: `attachment-list`/`-upload`/`-download` and
`user-find` are already noun-first, so `space-info` alongside `info` reads as
though `info` were the general case and the space one a special case, when they
are siblings. `page-info` also makes `page-info`/`space-info` complete under
`markfluence <TAB>` the way `attachment-<TAB>` already does.

It is a **breaking rename** and deliberately gets no alias. markfluence is
unreleased, there is nothing to be compatible with, and an alias would make the
thing this rename exists to fix — two names for one idea — permanent. What it
touches:

| | |
|---|---|
| `cmd/info/` → `cmd/pageinfo/` | package, `Cmd`, `Use`, registration in `root.go` |
| `schema/json-output/v1.json` | the `command` enum entry **and** its `if/then` branch — renaming the enum value without the branch leaves the result completely unvalidated, which `internal/schematest`'s document tests exist to catch |
| `docs/commands/` | `markfluence_info.md` → `markfluence_page-info.md`, regenerated |
| `README.md`, `CLAUDE.md`, `docs/*.md` | ~a dozen prose references, including several added by #168 |
| `~/.claude/skills/markfluence/SKILL.md` | **outside the repo**, and it invokes the command by name |

That last row is the one worth flagging: the skill is not in this repository and
will not be caught by `make check`, CI, or review. It has to be edited by hand
in the same sitting or it starts telling an agent to run a command that no
longer exists.

Two things this rename should *not* drag in. The `docs/commands/` filename
changes, so the old file must be **deleted** rather than left beside the new one
— `make docs-check` regenerates into a temp dir and diffs, so it will not notice
an orphan. And `_plans/005_info-subcommand.md` and friends keep saying `info`,
because a plan records what was decided when it was written; only living
documentation is rewritten.

Whether this lands in the same PR as `space-info` or its own is a judgement
call. Its blast radius is wider and entirely mechanical, so the argument for
splitting is that a reviewer can check the rename by reading a diff and the new
command by reading its reasoning — and the argument against is that `info` and
`space-info` shipping in one release is what makes the pair legible. One commit
either way, and it should come **first**: writing `space-info` against `info`
and then renaming both is two edits to the same lines.

## What the probe established — verified 2026-09-18

Against mozilla-hub with the scoped service-account token, which is also what
makes the access half testable: that account can read `SRE`, and can read *and
create pages in* the personal space it was given collaborator access to.

### `totalSize` cannot supply the page count, so the count is a walk

The obvious implementation is one CQL request per number:
`type=page and space=SRE` reports `totalSize: 1126`, and
`… and lastmodified >= now("-7d")` reports 5. Both matched a full walk exactly
on the spaces probed.

They cannot be used anyway. [search.md](../docs/confluence/search.md) records
`totalSize` drifting — 294, then 292, then 291, against 289 rows actually
collected — and reporting `totalSize=1` against an **empty** `results` array
right after a page was archived. Its rule is explicit: *never report it as a
count*. A `space-info` whose headline number is occasionally wrong by a few,
with no way to tell when, is worse than no number.

### One v2 walk gives every count exactly, and needs no CQL

`GET /wiki/api/v2/spaces/{id}/pages` returns, per page, `status`, `createdAt`
and `version.createdAt`. So a single cursor walk yields current pages, archived
pages, root pages, pages created in the window, pages *touched* in it, and the
date of the newest edit -- all exact, with no CQL and no date-expression syntax
to get wrong.

**These are page counts, not activity counts, and the labels have to say so.**
`version.createdAt` is the *latest* version's timestamp, so a page edited nine
times this week contributes **1**, not 9. The number of edits is not reachable
at any sane cost: it would need `/pages/{id}/versions` per page, turning a
5-request command into 1,100. So the fields are named for what they are ---
`pages_created` and `pages_touched`, rendered `pages touched: 5 in the last 7
days` --- because "updated (7d): 5" invites reading 5 as edits, and a reader who
makes that mistake has no way to notice.

The two overlap, which is also reported rather than left to be discovered: a
page created inside the window has its v1 in the window too, so it is counted
in **both**. Suppressing that would be a third definition to explain ("touched
but not created"), and silently double-counting is how `pages_created +
pages_touched` gets added up by somebody. The human line names the overlap
inline; `--json` carries it as its own field.

Measured on `SRE`: **5 requests** at `limit=250` for 1126 current + 40 archived,
agreeing with CQL on all three numbers. Cost is `ceil(N/250)` requests and is
the command's whole expense.

Two details the walk depends on: the route's default **includes archived
pages** (the personal space returned 38 = 37 current + 1 archived), so the split
is computed from each row's `status` rather than assumed; and it pages by the
`_links.next` cursor that `listV2` already handles.

Three more fields come out of the same pass for nothing, and each answers a
question the counts do not:

- **`last activity`**, the newest `version.createdAt` across current pages. Two
  zeroes for the 7-day window do not distinguish "quiet week" from "nothing
  since 2023", and this does. For orientation (use case 3) it is the
  highest-value number here, and unlike the two counts it is a fact rather than
  a tally, so it cannot be misread.
- **`root pages`**, rows with no `parentId`. Not trivia:
  [spaces.md](../docs/confluence/spaces.md) records that a space can have
  several roots and that `children --space` and `export --space` seed from
  them rather than from the homepage, so "11 roots" and "1 root" describe very
  different exports.
- **`type` and `status`** (global/personal, current/archived) come from the
  space GET rather than the walk, via one more `expand`. An **archived space**
  is the context that explains every other number on the page, and printing
  the numbers without it invites a wrong conclusion.

`description` and the space's own labels ride the same `expand` and are worth
having for the same reason `info` prints a page's labels; they are orientation
and nothing reads them.

### "The space's available page statuses" is not a thing, so the field asks twice

#168 established that the statuses a page may be given depend on the
**(caller, page)** pair, not on the space: one account was offered four on a
page it had created and three on a page in the same space that it had not, and
the write enforces it ([page-status.md](../docs/confluence/page-status.md)).
So there is no space-wide list to print. The three candidate sources:

| source | problem |
|---|---|
| `GET /space/{key}/state` | returns the four **product defaults** whatever the caller may use — measured claiming `Verified` for a caller who could not set it |
| `GET /space/{key}/state/settings` | authoritative, and **403s for anyone who is not a space admin** — re-confirmed with a collaborator-level account |
| `GET /content/{id}/state/available` | correct, but answers for *that page*, not the space |

Printing the `space/{key}/state` defaults would be a confident wrong answer,
which is the failure mode [docs/confluence/README.md](../docs/confluence/README.md)
opens with. So the field is **two sources tried in order**, each labelled with
what it actually is:

1. `state/settings` — the space's real configuration. Reported as
   `page statuses: Rough draft, In progress, Ready for review, Verified`.
   Only a space admin gets this.
2. `state/available` on the **homepage** — what *this caller* may set on *that
   page*. Reported naming both, because it is not the space's list and saying
   so is the whole point:
   `page statuses (yours, on the homepage): Rough draft, In progress, Ready for review`.
3. Neither — say so and point at `info PAGE`:
   `page statuses: (not available: not a space admin, and the homepage is not editable by you -- markfluence info PAGE shows what a page you can edit allows)`.

The second source is the one that makes this use case work at all, and it is
worth being clear about why the first is not enough: **use case 2 is mostly
non-admins.** An author filling in `page_status:` is exactly the person
`state/settings` 403s. A field that answers only for admins would be a field
that answers for almost nobody who needs it.

Case 3 is not hypothetical and lands on a real population: a collaborator who
can create pages in a space but cannot edit its homepage gets it (measured —
that exact shape is what broke `create`'s original probe in #168). A smarter
fallback is available in principle, since the page walk already sees every
page's `ownerId` and a page the caller owns is almost certainly editable, but
it needs the caller's own account id and it orders the walk before a field that
should not depend on it. **Out of scope until somebody hits case 3 and minds**;
recorded here so the next person does not re-derive it.

### `?expand=operations` is the read/write test, and it is free

`GET /wiki/rest/api/space/{key}?expand=operations` returns what **the calling
account** may do, resolved — no group membership to interpret, no test write,
one request:

| space | operations |
|---|---|
| `SRE` | `read:space` |
| `AGILE` | `read:space`, `create:page`, `create:blogpost`, `create:comment`, `create:attachment` |
| personal (collaborator granted) | same as `AGILE` |
| `AIM` | `read:space`, `create:comment` |

The *collection* carries the same expansion, which is how #171's `user-info
--spaces` answers "where can this account write?" in one walk rather than one
request per space -- and that walk has a trap this single-space lookup does not:
`/rest/api/space` returns short pages that are not the end. See #171.

It discriminates cleanly, and it matches behaviour observed independently: the
account that shows only `read:space` on `SRE` is the one that got
`403 User does not have Page edit permission` against an `SRE` page.

**What it cannot tell you is whether a given page is editable**, because that is
per page — `create:page` is a space grant, page *update* is not in this list at
all. So the row is worded `your access: read, create pages`, never "write", and
the plan does not pretend otherwise. `update`'s own failure remains the place
you learn a page is not editable; #168's `state/available` is the free per-page
probe for anyone who wants to ask first.

The route is **undocumented for `GET`** — Atlassian's swagger carries only
`PUT`/`DELETE` for `/wiki/rest/api/space/{spaceKey}` — so its scope is observed
rather than derived, exactly like the three v1 child-listing routes
[api.md](../docs/confluence/api.md#scopes) already records that way. It works
with the union markfluence already requires.

## Decisions

**Read-only, and no markdown file anywhere.** The argument is a space **key**,
resolved through `ResolveSpaceID` like `find --space` and `children --space`, so
an unknown key is a typo-shaped failure rather than an empty result. No
`internal/pageref` involvement: there is no file that names a space, and
inventing one would make `space-info docs/foo.md` mean "the space that file
publishes to", which is a different command.

**Counts are exact or absent.** The walk is the implementation precisely because
the cheap answer cannot be trusted. If the walk fails partway the counts are
reported `null` rather than partial — a wrong count is worse than no count, and
this is a command whose entire output is counts.

**The window is 7 days and is a flag.** `--since` taking a day count (default
`7`), because "the last week" is one reasonable question among several and the
walk already has the data — the field costs nothing to parameterise. A string
vocabulary is *not* needed here: unlike `children --depth` and `search --limit`
there is no `all`, and `0` is meaningful (today only), so a plain positive
integer with `0` allowed is the right shape and the one place this command
departs from those two.

**Every field degrades independently.** The space lookup is the only fatal one:
without it there is no space. The page walk, the status list and the operations
read each fail to `null` with a note, the way `info`'s width/labels/status rows
already do. A token that can read a space but not walk it still gets a useful
answer.

**Staleness buckets and editor counts are deliberately absent.** Both are free
from the same walk (`version.createdAt` bucketed, distinct `version.authorId`),
and both serve a fourth use case -- auditing which spaces have gone stale --
that is not one of the three above. `last activity` is the one-number version
of staleness and is enough for orientation. Adding the rest now would be
building for a use nobody has stated; the walk that would feed them is already
here if somebody does.

**Both counts are reported, current and archived.** The ask was "pages (not
archived)", and printing one number silently excluding another invites "is that
all of them?" — the split costs nothing since the walk sees both, and it makes
the exclusion legible.

## Implementation

### `internal/client`

```go
// SpaceSummary is a space's identity plus what the calling account may do in it.
type SpaceSummary struct {
    ID, Key, Name, Type, Status, Description, HomepageID string
    Labels     []string
    Operations []SpaceOperation // nil when the expansion failed
}

func (c *ConfluenceClient) GetSpace(spaceKey string) (*SpaceSummary, error)
func (c *ConfluenceClient) WalkSpacePages(spaceID string, visit func(Page) error) error
```

`GetSpace` is the v1 route with
`?expand=operations,description.plain,metadata.labels` — every field above in
one request. The id/name/homepage could come from v2 `/spaces?keys=` instead,
but one request that answers all of it is better than two, and the operations
only exist on v1.

The page-status probe reuses what #168 already built: `pagestatus.Available`
for the homepage case, and a new client method for `state/settings`, which
nothing calls today. Both failing is a normal outcome, not an error.

`WalkSpacePages` takes a callback rather than returning a slice, and rather than
going through `listV2`, which collects every row before returning any. Counting
is the one caller and it needs no row twice, so a 20k-page space would be held
in memory for nothing. A callback is also the idiom already here —
`pagetree.Walk` — where a `range`-over-func iterator would be the first in the
codebase and is not worth introducing for one caller. The cursor handling is
`listV2`'s and should be factored out rather than copied: a second copy of v2
paging is how one of them comes to terminate on a short page.

### `internal/spaceinfo` — **not** a new package

The counting is a dozen lines over one iterator and has exactly one caller.
`cmd/spaceinfo` owns it. A package earns itself when two commands need the same
rules (`internal/pagetree`, `internal/labels`); this does not.

### `cmd/spaceinfo/`

`Cmd`, `run`, a `report` struct feeding both renderers — `cmd/info`'s shape,
deliberately, since the output is the same label/value block and the same
"omit a field that could not be read" rule.

Completion: the argument is a server-side space key, so it completes to
nothing, for `internal/completion`'s standing reason (completion runs on every
keystroke and may not call Confluence). `TestSubcommandsCompleteArgs` still
requires a `ValidArgsFunction`.

### `--json` and the schema

A new `command` enum entry plus an `if/then` branch constraining `results.items`
**and** `summary`, or `internal/schematest`'s document tests fail — adding the
enum entry alone is exactly how a new command's conformance test goes green
while validating nothing.

```json
{ "ok": true, "key": "SRE", "name": "…", "id": "…", "type": "global",
  "status": "current", "description": "…", "labels": ["intranetwiki"],
  "homepage_id": "…", "homepage_title": "…",
  "access": { "read": true, "create_pages": false },
  "page_statuses": { "source": "space" | "page" | null,
                     "probe_page_id": "…" | null,
                     "names": ["Rough draft", "…"] | null },
  "pages": { "current": 1126, "archived": 40, "roots": 2 },
  "recent": { "days": 7, "pages_created": 0, "pages_touched": 5,
              "pages_created_and_touched": 0,
              "last_activity": "2026-09-15T10:04:00.000Z" } }
```

`pages` and `recent` are `null` when the walk failed and `access` is `null`
when the operations expansion failed — the independent degradations above, each
visible. `page_statuses` is always an object so a consumer can tell the three
cases apart without string-matching a human sentence: `source: "space"` is the
admin answer, `source: "page"` names the probe in `probe_page_id`, and
`source: null` with `names: null` is "could not be determined". A consumer that
treats the `page` source as the space's list is wrong, and the field is shaped
to make that a deliberate mistake rather than an easy one.
`summary` is the single-op `{total: 1, succeeded: 1, failed: 0}`. An operational
failure is an `errorObject` on stderr rather than a `results[0]` entry, which is
`find`/`search`/`children --space`'s rule and applies for their reason: there is
no page id to name.

## Files

| file | change |
|---|---|
| `cmd/spaceinfo/{spaceinfo,json}.go` | new |
| `cmd/root.go` | register it |
| `internal/client/space.go` | new: `GetSpace`, `ListSpacePages` |
| `schema/json-output/v1.json` | enum entry, result `$def`, `if/then` branch |
| `docs/confluence/spaces.md` | the walk, the counts, and `?expand=operations` |
| `docs/confluence/page-status.md` | the two-source fallback, and why admin-only is not enough |
| `docs/json-output.md`, `README.md`, `CLAUDE.md` | the command |

## Tests

- The counts are computed from `status` and the two timestamps, not from
  `totalSize` — a stub whose `totalSize` disagrees with its rows must not
  change the answer.
- The walk follows the cursor and counts across pages, and an archived row is
  excluded from `current` and counted in `archived`. A short page mid-walk is
  not the end: `listV2`'s rule, and the regression that matters if its cursor
  handling is refactored to serve both.
- A walk that fails partway reports `null` counts, not partial ones.
- `--since 0` is valid and means today; a negative value is refused.
- Each of the three optional reads failing leaves its own field `null` and the
  rest intact.
- An unknown space key fails as a typo, before the walk.
- `access` maps `create:page` to `create_pages: true` and its absence to false;
  a missing `operations` expansion is `null`, not "no access".
- `roots` counts rows with no `parentId`, and `last_activity` is the newest
  `version.createdAt` across current pages -- not the newest row, and not
  `createdAt`.
- `pages_touched` counts **pages, not edits**: a page whose history holds nine
  versions inside the window counts once. The stub serves exactly that page and
  the test asserts 1.
- A page created inside the window is counted in `pages_created` *and*
  `pages_touched`, and `pages_created_and_touched` reports the overlap, so the
  three are consistent by construction rather than by a reader's arithmetic.
- The page-status field takes the admin source when `state/settings` answers,
  falls back to the homepage probe with `source: "page"` and the probe id set,
  and reports `source: null` when both fail -- three tests, because the whole
  value of the field is that a consumer can tell them apart.
- An archived space still reports its counts; `status` is how a reader knows
  to interpret them.
- Schema conformance, built with the command's own builder.

## Not in scope

- **Blogposts, comments, attachments, folders.** The walk is over pages. A space
  whose value is in blogposts is a different report and nobody has asked.
- **Per-page editability.** Not a space property; see above.
- **Space permissions as a table.** `/api/v2/spaces/{id}/permissions` returns
  principal/operation pairs, and resolving them to "can *I* write" means
  resolving group membership. `?expand=operations` already answers the question
  for the caller, which is the one being asked.
- **A `--space` mode on `info`.** `info` takes a page and reports a page;
  overloading its argument to mean "a space if it looks like a key" is how
  `PAGE` stops being a page.
- **Watching a space over time.** Counts at one moment. Trends are #148's
  neighbourhood if anywhere.
- **Staleness buckets and editor counts.** Free from the walk, but they serve a
  space-auditing use case that is not one of the three this command is for.
  See the decision above.
- **A smarter page-status probe** (a page the caller owns, or a
  `--status-page PAGE` flag) for the case where the homepage is not editable.
  Recorded under "not a thing" above with what it would take.
- **Slug-collision counts** -- how many sibling groups `export` would
  disambiguate with a `-<id>` suffix. The most markfluence-specific thing this
  walk could report, and a fact about an *export* rather than about a space, so
  it belongs in `export --dry-run` where somebody is about to act on it.
