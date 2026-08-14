# Plan: the `search` command

Add full-text page discovery. Closes #44.

## What the issue asked, and what it turned out to be

#44 proposes `markfluence search QUERY [--space KEY] [--limit N] [--cql]` and
hands over a recipe borrowed from confluence-cli: wrap the query as
`text ~ "<escaped>"` and send it to v1 `GET /wiki/rest/api/search`. The issue
calls that recipe de-risking, and for the transport it was — #77 already landed
`SearchCQL` (the cursor pager) and `escapeCQL` while building `find`, and a
[comment on #44](https://github.com/mozilla/markfluence/issues/44) already
corrected the issue's claims about offset pagination, `totalSize`, the response
shape, and the gateway.

Probing the endpoint for *ranking* rather than transport turned up the thing none
of that covered: **`text ~` produces a search that does not work.** For the query
`deploy runbook`:

| | `text ~ "deploy runbook"` | `siteSearch ~ "deploy runbook"` |
|---|---|---|
| 1 | Monitor Base Load Engineer Runbook | **Deployment runbook** |
| 2 | Base Load Engineer's Hand-off Log | **Runbook: Grafana deploys** |
| 3 | Local Models Selection | **Runbook: Prod deployment** |
| 4 | Merino Curated Recommendations | **Runbook: Stage deployment** |
| 5 | LCM Team Resources — Global Operations and Governance | **Runbook: deploying changes** |
| 6 | Treeherder | **SubPlat: Runbook** |

Same corpus, near-identical totals (`type = page` and `text ~` reported 3060,
`siteSearch ~` 3071–3073), completely different ordering. And **`score` is `0.0`
on every row of every query sampled**, so there is nothing to re-rank with
client-side. The server's order is the only order there is, which makes picking
the right field the whole ballgame rather than a preference.

So the shape of the work is close to what the issue describes and the single most
important decision in it is the one the issue got wrong.

## What was probed, and what it established

Read-only, against the live Cloud site through the gateway, small page sizes.
Everything here goes into [docs/confluence/search.md](../docs/confluence/search.md).

### `siteSearch ~` ranks; `text ~` does not

The table above. `siteSearch` is not in Atlassian's CQL field documentation but
the 400 from an invalid field lists it as valid (already recorded in search.md),
and it is what the site's own search UI behaves like. Both accept a `space`
clause and both work through the gateway.

The cost of `siteSearch` is that it decorates: see the markers below.

### Multi-word is AND, order-independent — not a phrase match

| query | totalSize |
|---|---|
| `text ~ "deploy"` | 3329 |
| `text ~ "runbook"` | 751 |
| `text ~ "deploy runbook"` | 304 |
| `text ~ "runbook deploy"` | 304 |
| `text ~ "deploy AND runbook"` | 304 |
| `text ~ "zzzzq deploy"` | **0** |

Reversing the words changes nothing, so it is not adjacency. Adding a term that
matches nothing collapses the result to zero, so every term is required. `AND`
appears to be ignored as a token rather than honored as an operator — either way
it makes no difference. This needs to be in the help text: a user who adds a word
to narrow a search is doing the right thing, and a user expecting a quoted phrase
is not getting one.

### An untyped query returns things markfluence cannot use

`text ~ "deploy"`, first 100 rows: **94 page, 3 attachment, 2 database, 1
comment**. An attachment id and a comment id are dead ends for every markfluence
verb.

`siteSearch ~ "deploy"` returned 99 pages out of the first 99 — its ranking
floats pages up on its own. That is luck at the top of the list, not a filter:
untyped reported 3358 against 3073 for `type = page`, so roughly 285 non-page
hits are in there, further down. Type pinning matters for `--limit all` and deep
paging even where the first page looks clean.

Valid `type` values, with `siteSearch ~ "deploy"`:

| type | hits |
|---|---|
| `page` | 3073 |
| `attachment` | 127 |
| `comment` | 144 |
| `blogpost` | 6 |
| `database` | 2 |
| `whiteboard` | 2 |
| `folder` | **0** |
| `embed` | 0 |
| `space` | 0 |
| `bogustype` | **400** `Unsupported value for type` |

**Full text cannot see folders.** Structurally, not incidentally — a folder has
no body. Unlike `find`, `search` therefore has no folder half: one request, no
merge, no `extensions.position` ordering to preserve. It also cannot see archived
pages, which search.md already establishes.

`type in (page, folder)` parses, so a list is available if a `--type` list is
ever wanted.

### Use `content.title`; the row-level `title` is unusable

The row's own `title` is **HTML-escaped and decorated with highlight markers**:

```
row.title      'Base Load Engineer&#39;s Hand-off Log'
content.title  "Base Load Engineer's Hand-off Log"

row.title      'Deployment @@@hl@@@runbook@@@endhl@@@'
content.title  'Deployment runbook'
```

`content.title` was clean for both, on every row sampled, in both modes. Also
checked against 245 folder rows: 9 had `&` in the row title (`Apps &amp;
Actions`, `Fastly Developers&#39; guide`) and `content.title` was correct in
every case, matching what v2 `/folders/{id}` returns for the same id.

**This means shipped `find` is already right** — `findFoldersByTitle` reads
`h.Content.Title`. No fix needed, and it is worth writing down so nobody
"simplifies" it to `h.Title` later.

`content._links.webui` was byte-identical to the row's `url` on all 50 rows
sampled, so `find`'s use of `h.URL` is equally correct. `search` will read
`content._links.webui` for consistency with the rule above.

### The excerpt needs cleaning, demonstrably

Only the row level has an `excerpt`, so there is no clean variant to prefer. Over
50 rows with `excerpt=highlight`:

- **40 carried `@@@hl@@@` / `@@@endhl@@@`**
- 40 contained newlines
- 2 contained HTML entities
- 0 were empty

So stripping and unescaping are not defensive coding, they are required for the
common case.

The `excerpt` parameter:

| value | result |
|---|---|
| *(unset)* | 150–450 chars — same as `highlight` |
| `highlight` | 150–450 chars, markers present |
| `indexed` | truncated to **50 chars** |
| `none` | empty string, 200 |
| `bogus` | **empty string, 200** |

An unrecognized value is not an error. That cuts both ways and is the reason to
pass `excerpt=highlight` explicitly anyway: it pins the behavior actually tested,
at the cost that a future rename by Atlassian would silently mean no excerpts
rather than a failure. Recorded in search.md as the thing to watch.

### An empty query is a 500, not a 400

```
type = page and siteSearch ~ ""
→ 500 java.lang.NullPointerException: query parameter is required.
```

`find` already rejects a blank title before sending anything, for tidiness. Here
it is load-bearing: without the guard, a usage error surfaces as an operational
failure with a Java stack-trace message and the wrong exit code.

### Fields that look useful and are not

- **`score`** — `0.0` on every row of every query. Do not expose it, do not sort
  on it.
- **`breadcrumbs`** — empty array on all 50 rows sampled. There is no free
  ancestor path here; `children` is the command that knows about hierarchy.
- **`resultGlobalContainer`** — populated on all 50, as
  `{title: "Ecosystems", displayUrl: "/spaces/PXI"}`. This is the space's
  *display name*, which `SpaceKeyFromWebUI` cannot give us. Not used — the key is
  the handle every other command takes — but recorded, since it is the only place
  the human-readable space name comes free.
- **`content.status`** — `current` on all 50, necessarily, because CQL cannot see
  archived content.

## Decisions locked

### `siteSearch ~` always, with no flag to choose `text ~`

The ranking evidence above. A `--text` escape hatch was considered and dropped:
its only honest help text is "ranks worse", and `--cql` already lets anyone who
wants `text ~ "..."` write it.

### `--type` is a vocabulary of `page` (default), `blogpost`, `all`

`page` is the default because every markfluence verb operates on a page.
`blogpost` is the only other authored document. `all` drops the type clause
entirely.

`folder` is **refused with an explanatory error** rather than accepted as a value
that always returns nothing — full text cannot see folders, and the remedy is
`find`. This follows `children --depth`, which refuses `0` rather than reading it
as "unlimited" on exactly the same reasoning: a silently useless value is worse
than an error that names the alternative.

`attachment`, `comment`, `database` and `whiteboard` are left out even though
they match. None is something a markfluence command accepts, and the set will
keep growing as Atlassian adds content types. `--cql` covers them.

Single value, not a comma list, for 1.0 — one `completion.Values` set, one schema
field.

### `--limit` is a positive number or `all`, default `25`

A **string** vocabulary, exactly like `children --depth`, and `0` is refused
rather than read as unlimited for the reason CLAUDE.md already records there.

This contradicts the issue, which asks for unlimited by default. The issue's
argument — "silently returning 10 of 40 matches is a trap" — is correct for
`find`, where the match set is small and every row is load-bearing. It does not
transfer to full text: `siteSearch ~ "deploy"` matches 3073 pages, and a
relevance-ranked top-N *is* the answer rather than a truncation of it. Unlimited
by default would mean 13 sequential gateway requests for a one-word query typed
by someone who wanted to see if a page existed, on a shared corporate instance.

The issue's actual concern is honored in full: **nothing is ever truncated
quietly.** With a bound of N, the pager asks for N+1 rows; if it gets them, the
extra is dropped and the command says so:

```
Showing 25 matches; more exist (use --limit all).
```

Presence, not a count. `totalSize` cannot supply a count — it drifted 294 → 292 →
291 against 289 rows actually collected, and reported `1` against an empty
`results` array right after a page was archived.

`--limit all` keeps the existing `maxSearchPages` guard. It is not a new hazard:
the worst case reachable through `--cql` (`type = page`, 20506 hits) is 83
requests, inside the 200-page bound, and `all` is an explicit opt-in.

### The client returns a cleaned struct, not raw rows

`FindByTitle` returns `TitleMatch` rather than `SearchResult`. `search` gets the
same treatment: a `SearchMatch` assembled from `content.*`, with the excerpt
cleaned. Raw `SearchResult` stays raw, so the two paths cannot disagree about
what a title is.

```go
type SearchMatch struct {
    ID      string
    Type    string
    Title   string
    Space   string
    URL     string
    Excerpt string
}

// Matches is in the server's relevance order. More reports that the bound was
// hit, without claiming a count. Skipped counts index rows with no addressable
// content object.
type SearchResults struct {
    Matches []SearchMatch
    More    bool
    Skipped int
}

func (c *ConfluenceClient) SearchText(query, spaceKey, contentType string, limit int) (SearchResults, error)
func (c *ConfluenceClient) SearchRawCQL(cql string, limit int) (SearchResults, error)
```

A result **struct** rather than `(matches, more, skipped, error)`, for the reason
021 gives for `RetryEvent`: fields can be added without breaking the signature.
Three positional returns plus an error was already at the edge, and `skipped` is
the second field this design grew after the first draft.

`SearchText` owns the query construction: the `siteSearch ~` clause with
`escapeCQL` applied, the type clause, and the space clause.

`Type` is a plain string, not a Go or JSON enum. `--type all` and `--cql` can
return `whiteboard`, `database`, or whatever Atlassian adds next, and a closed
set would turn a new content type into a validation failure.

**`--space` reuses `ResolveSpaceID` and `ErrSpaceNotFound`,** as `FindByTitle`
does. CQL answers an unknown space key with zero rows, which reads exactly like
"no matches" — and the caller's next move on "no matches" is to create the page.

### The bounded pager, and why `SearchCQL` keeps its signature

New unexported `searchCQLBounded(cql string, max int) ([]SearchResult, bool, error)`.
Existing `SearchCQL(cql)` becomes a call to it with `max == 0`, so `find` and its
tests are untouched.

Per-request page size becomes `min(max+1, searchPageSize)`, so `--limit 5` is one
request for 6 rows rather than a 250-row fetch that throws 245 away.

The three termination rules from search.md are unchanged and non-negotiable:
terminate on a **missing `next`** and never on a short page (100, then 98 — still
carrying a `next` — then 91, against 289 total), ignore `start`, and branch on
nothing derived from `totalSize`.

### Excerpt cleaning: strip, unescape, collapse — once, in the client

In that order:

1. Remove `@@@hl@@@` and `@@@endhl@@@`.
2. `html.UnescapeString`, exactly once.
3. Collapse every run of whitespace — including the newlines 40 of 50 rows carry
   — to a single space, then trim.

Unescape after stripping because an entity-encoded marker is not a thing that has
been observed, while an entity that decodes to whitespace is handled correctly by
collapsing last. A page whose body literally contains the text `@@@hl@@@` loses
it; that is accepted and recorded.

**The collapse happens in the client, so there is one canonical excerpt** shared
by the human and `--json` paths. A single-line excerpt is also the friendlier
`--json` value — a consumer piping through `jq` does not want the index's
arbitrary line breaks.

### Human output is a block per hit, not a table

The excerpt is the point of a full-text hit — it answers "why did this match?" —
and at 150–450 characters no aligned table survives it. `children` already uses
an indented tree rather than a table, so shape-following-meaning is established
rather than novel.

```
Deployment runbook
  page 2064154670  PXI
  https://mozilla-hub.atlassian.net/wiki/spaces/PXI/pages/2064154670/Deployment+runbook
  Deploying to prod requires an approved change request and a green stage deploy.

Runbook: Prod deployment
  page 1210286095  SRE
  https://mozilla-hub.atlassian.net/wiki/spaces/SRE/pages/1210286095/Runbook+Prod+deployment
  Before you start, confirm the on-call has acknowledged the deploy window.

Showing 25 matches; more exist (use --limit all).
```

The excerpt is one indented line and is **not wrapped**. `internal/ui` has no
terminal-width detection and adding it for this is not worth a wrap helper and
its tests; the terminal soft-wraps. A hit with an empty excerpt simply has no
excerpt line.

Server relevance order is preserved. **`search` is the first command whose result
order comes from the server rather than from our own sort** — `sortMatches` must
not be applied, and the schema's result description has to say so, or a consumer
will assume the array is sorted by something it can reproduce.

### `--cql` passes through, and refuses what it would have to rewrite

`--cql` is a boolean toggle; `QUERY` stays positional, per the issue.

With `--cql` the query is sent verbatim: no escaping (the caller owns it), no
type clause, no space clause.

- **`--cql` with `--space` is a usage error.** ANDing a space clause onto a query
  containing `or` would regroup it and silently answer a different question, and
  "raw CQL" that is not raw is worse than a refusal.
- **`--cql` with an explicitly-set `--type` is a usage error** too, detected with
  `cmd.Flags().Changed("type")` since `--type` has a non-empty default. This is
  the first use of `Changed` in `cmd/`.
- `--limit` still applies. It bounds the pager, not the query.
- A blank query is rejected the same way.

Rows with no addressable `content.id` are skipped, counted, and **reported** —
`ui.Warn` in human mode, `summary.skipped` in `--json`. See below for why the
count is not optional.

### A skipped row must be counted, because the alternative is a silent wrong answer

`--cql 'type = space'` returns **388 rows** on the live site, every one with
`entityType: "space"` and **no `content` object at all**. The addressable data
sits in a sibling `space` object instead:

```json
{ "entityType": "space", "title": "Engineering Workflow", "url": "/spaces/EW",
  "space": { "key": "EW", "name": "Engineering Workflow", "status": "current" } }
```

A space has no id in that payload, only a key, so it cannot be represented in a
result shape whose `id` is required — and no markfluence verb takes a space
anyway. Skipping is right. Skipping *quietly* is not: without a count the command
reports `results: []`, `summary.total: 0` and **exit 0** for a query that matched
388 things. That is a complete-looking empty answer, which is the same hazard
CLAUDE.md records for `find` — a caller's next move on "nothing found" is to
create what it thinks is missing.

So `searchSummary` carries `skipped`. The objection considered and rejected was
that it is a field which will usually be `0`: `basicSummary.failed` is *already*
structurally always `0` for both `find` and `search`, since neither has a per-row
failure variant, so this def already pays that cost and paying it once more buys
the removal of a silent wrong answer.

The path is narrow — unreachable from the default flags, since `page` and
`blogpost` always carry a `content` object, and 0 of 99 rows on an untyped
`siteSearch` hit it — but `--cql` is exactly the flag that reaches it.

Note also that `internal/client/search.go`'s existing comment, that the index
returns content-less rows for "spaces and users", is half right and gets
corrected: spaces confirmed, but `type = user` returns ordinary content rows
(90250 hits, i.e. effectively unfiltered), as does `space.type = global`.

### `--json`: a new result def and a new summary def

```json
{"ok": true, "id": "…", "type": "page", "title": "…",
 "space": "PXI|null", "url": "…|null", "excerpt": "…|null"}
```

```json
{"total": 25, "succeeded": 25, "failed": 0, "truncated": true, "skipped": 0}
```

- **No `status` field.** Every CQL hit is `current` because CQL cannot see
  archived content. A field that is always the same value would invite a consumer
  to believe `search` reports archived pages — the one thing it cannot do, and
  the reason to reach for `find`.
- `basicSummary` is `additionalProperties: false`, so `truncated` and `skipped`
  need a new `$defs/searchSummary`.
- `space`, `url` and `excerpt` are `stringOrNull` via the existing `nullable`
  helper pattern.
- The built CQL goes to `ui.Debug`, not into the envelope. `--debug` is where
  "what did you actually ask?" belongs.

### Failure reporting mirrors `find`

- Config, usage, unknown space, blank query, bad `--type`, bad `--limit`,
  conflicting `--cql` → exit **2**, `CodeValidation` / `CodeConfig`.
- The search itself failing → exit **1**, an `errorObject` on **stderr**, not a
  `results[0]` failure. There is no page id to name, and reporting a partial
  answer would read as "nothing found".
- No matches → **exit 0**, `No matches found.`, empty `results`.

`search` becomes the second command whose operational failure is an error object
rather than a result entry, so CLAUDE.md's note that `find` is "the only" such
command needs correcting.

## Out of scope (deliberately)

- **A `--type` comma list.** `type in (page, folder)` parses, so it is available
  later. Nothing asks for it now, and it would complicate completion and the
  schema field.
- **Exposing `score`.** It is `0.0` everywhere.
- **Ancestor/breadcrumb context on a hit.** `breadcrumbs` is empty; producing it
  would mean a request per hit.
- **Wrapping output to the terminal width.** New machinery in `internal/ui`,
  wanted by more than this command if wanted at all.
- **Making CQL see archived pages.** There is no `status` field in CQL. It cannot
  be done from this endpoint.
- **Fixing anything in `find`.** The escaping investigation cleared it.

## Steps

The schema and the command **must land in one commit**: `cmd`'s
`TestCommandEnumMatchesRegisteredCommands` is bidirectional — it fails on an enum
entry that is not a registered command as well as the reverse.

1. `docs(plans): plan the search command` — this file.
2. `docs(confluence): record what /search does for full text` — retitle
   search.md from "finding content by title" and add the ranking comparison, the
   AND semantics, the type table, the row-title escaping and markers, the
   excerpt parameter, the 500 on an empty query, the content-less `entityType:
   "space"` row, and the useless fields. Note that this vindicates `find`'s use
   of `content.title`, and correct the "spaces and users" claim in
   `internal/client/search.go`'s comment — `type = user` returns ordinary content.
3. `feat(client): bound the CQL pager` — `searchCQLBounded`, `SearchCQL`
   delegating, `min(max+1, searchPageSize)` sizing.
4. `feat(client): clean a CQL search excerpt` — strip/unescape/collapse.
5. `feat(client): add SearchText and SearchRawCQL` — `SearchMatch`, query
   construction, the space guard, and the skipped-row count.
6. `feat(search): add the search command` — `cmd/search/{search,json}.go`,
   registration in `root.go`, completion, **and** the schema's `command` enum
   entry, `if/then` branch, `searchResult` and `searchSummary` defs.
7. `docs(readme): document the search command`.
8. `docs: add search to the architecture notes` — CLAUDE.md gains a `cmd/search/`
   bullet, the client additions, and the correction to `find`'s "only command
   whose failure is an errorObject" note.

## Testing

**Client.** `siteSearch ~` query construction with and without space and type;
`escapeCQL` applied to the query text (reuse the pinned injection string);
`ErrSpaceNotFound` on an unknown key; bounded paging stops at `max+1` and reports
`more`; a short page mid-walk does **not** terminate the walk; termination only
on a missing `next`; `totalSize` disagreeing with `len(results)` changes nothing;
page size is `max+1` when small and `searchPageSize` when `max` is 0 or large;
excerpt cleaning over the real observed forms — markers, `&#39;`, embedded
newlines, and all three at once; a content-less row (the real `entityType:
"space"` payload) is skipped and counted, and a mix of content and content-less
rows returns the content ones with the right skipped count.

**Command.** `--type` accepts `page`/`blogpost`/`all` and refuses `folder` with
the message naming `find`; `--limit` accepts a positive number and `all` and
refuses `0` and non-numeric; blank and whitespace-only `QUERY` exit 2 without a
request; `--cql` with `--space` and `--cql` with an explicit `--type` each exit
2; empty results exit 0 with `No matches found.`; a query returning only
content-less rows reports the skipped count rather than a bare empty result; the
truncation line appears only when `more`; a hit with an empty excerpt emits no excerpt line; result order
is the server's, unsorted.

**Schema.** A conformance test built with the command's own builder — never a
hand-copied literal — plus `internal/schematest`'s document checks, which require
the new enum entry to have a branch constraining both `results.items` and
`summary`.
