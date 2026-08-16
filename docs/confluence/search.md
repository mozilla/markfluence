# Search: finding content

Two questions, two commands, and — for one of them — two APIs.

`find` resolves an **exact title** to an id. `search` answers **full text**:
"which page is about deploys?", where the title is not known. Both end up at
`/wiki/rest/api/search` for at least part of the answer, so the pagination,
injection and index-lag facts below are shared. The parts that differ are in
[Full text](#full-text-verified-2026-08-14).

Resolving a title to an id needs two different APIs, because neither one can see
everything markfluence cares about.

| | current pages | archived pages | folders |
|---|---|---|---|
| **v2** `GET /wiki/api/v2/pages?title=` | yes | yes, with `status=archived` | no — every v2 page route 404s a folder id |
| **v1 CQL** `GET /wiki/rest/api/search?cql=` | yes | **no** | yes, `type=folder` |

That split is the whole reason `find` issues two requests. Everything below was
established against a live Cloud site through the gateway.

## Verified 2026-08-14

### v2 `title=` is an exact, case-insensitive match

Against a page titled `Strategy & Insights Homepage [re-write in progress, 2018/04]`:

| `title=` | hits |
|---|---|
| the full title | 1 |
| the full title lower-cased | 1 |
| the full title UPPER-CASED | 1 |
| `Strateg` (a prefix) | 0 |
| `trategy` (an infix) | 0 |
| `Strategy` | 2 — two pages genuinely titled exactly that |
| *(empty)* | 0 |

So it is exact and case-insensitive, not a substring search. An empty `title=`
matches nothing rather than everything, but `find` rejects a blank title before
sending anything anyway — the CQL half of the same query **400s** on `title=""`.

This matters beyond `find`: it is the same match `create`'s duplicate check has
always relied on, and the CQL form below agrees with it, so the two halves of a
search cannot disagree about what counts as a match.

### Space keys are case-insensitive on both sides

`GET /wiki/api/v2/spaces?keys=` resolved `webplatforms`, `WEBPLATFORMS` and
`WebPlatforms` to the same space, and CQL's `space =` behaves the same way (see
below).

That the *two* agree is the point. `find` resolves `--space` through the v2
lookup purely to reject an unknown key up front, then hands the key itself to
CQL — so if the v2 lookup were the stricter of the two, that guard would refuse
keys the search would have matched. It isn't, so it can't.

### A v2 `/pages` list row carries `_links.webui`

Each row's `_links` holds `editui`, `edituiv2`, `tinyui`, and **`webui`**; the
per-row `_links` has no `base`, while the collection's top-level `_links` has
`base` and `next`.

So a space key and a page URL come free from a search result, with no follow-up
request and no reverse lookup — `SpaceKeyFromWebUI` works on the row as-is. Note
that a space homepage reports `webui` as `/spaces/{key}/overview`, with no
`/pages/` segment, and a personal space key looks like `~712020abc…`; the
`^/spaces/([^/]+)/` pattern handles both.

### v2 `status` accepts four values, and repeats

`current`, `archived`, `trashed`, and `deleted` are accepted; `draft` is a
**400**. The parameter repeats, so `?status=current&status=archived` returns
both in one request — which is how `find` reports an archived clash alongside
live pages.

### v2 `/pages` paginates with a cursor, in the form `resolveNext` already handles

With `limit=1` against a 2-hit title, `_links.next` is
`/wiki/api/v2/pages?limit=1&title=…&status=current&cursor=…` — a site-relative
absolute path including the `/wiki` prefix. That is exactly the shape
`resolveNext` was written for, so `listV2` needs no special case.

### CQL indexes folders — and it is the only thing that does

`cql=type=folder` returns rows with `content.type: "folder"`, several hundred on
the site tested. An untyped `title = "…"` query returns pages **and** folders
mixed.

The alternative was checked and does not exist: `GET /wiki/rest/api/content?type=folder`
returns **501**, `Cannot fetch folders with ContentFinder`. There is no non-CQL
route to a folder by title.

markfluence still asks for `type = folder` explicitly rather than leaving the
query untyped. Attachments, comments, spaces and users share that index and all
have titles; the sampled titles happened to return only pages and folders, which
is luck rather than a guarantee. An attachment id is not something you can pass
to `--parent`.

### CQL cannot see archived pages at all

A page archived in a live space is returned by v2 with `status=archived` and by
CQL not at all — `totalSize=0`, `results` empty, for both a freshly archived page
and one archived long ago. `includeArchivedSpaces=true` makes no difference: that
parameter is about content in archived **spaces**, which comes back with
`status: "current"`, and it is a different question entirely.

`archivedResultCount` was `0` on every query sampled, including ones where
`includeArchivedSpaces=true` added 151 results. Whatever it counts, it is not
"archived matches you cannot see" — do not build on it.

### CQL has no `status` field

`title = "…" and status = current` is a **400**, and the error names the fields
that do exist:

```
No field exists with the name: 'status'
Did you mean one of : space, subtype, space.key, siteSearch, space.type
```

So a CQL query cannot be restricted to current content, or widened to archived
content. It returns what it returns. This is why the page half of `find` is v2
rather than CQL, even though CQL can also return pages.

### CQL `=` is exact, `~` is fuzzy

For one folder title: `title = "…"` → 1, the same title lower-cased → 1, and
`title ~ "Onboarding"` → 304 against `title = "Onboarding"` → 12. Use `=`.

`space = KEY` works quoted or unquoted and is case-insensitive. An **unknown**
space key returns 0 results rather than an error, so a typo is indistinguishable
from an empty result — `find` resolves the key up front and refuses an unknown
one instead.

### CQL is an injection surface

```
cql=title="a" or type=page   →   totalSize=20508
```

A title containing `" or …` rewrites the query. Escaping is not optional. The
parser accepts backslash escapes — `title = "a\"b"` returns 200 — so escaping `\`
then `"` closes it.

### `/search` pagination: cursor-only, and four traps

This is the part most likely to produce a silently truncated answer. `/search`
is a v1 collection that does **not** follow the v1 offset scheme in
[api.md](api.md).

**1. `start` is silently ignored.** `start=0`, `start=5` and `start=250` returned
byte-identical rows. Offset paging is not merely discouraged here, it does
nothing — a loop built on it either spins on page one forever or, with a
short-page termination rule, quietly returns only the first page.

**2. A short page does not mean the end.** Following the cursor with `limit=100`
returned 100, then **98**, then 91 rows, and the 98-row page still carried a
`next`. `listV1`'s "fewer rows than asked for, so we are done" rule would have
stopped at 198 of 289. Terminate on a **missing `next`**, never on a short page.

**3. `totalSize` is an estimate, and can exceed what exists.** Across those three
pages it read 294, then 292, then 291, against 289 rows actually collected. A
single `limit=250` request returned 245 rows while reporting `totalSize=289`.
Most sharply: immediately after a page was archived, `title = "…"` reported
`totalSize=1` with an **empty** `results` array. Branch on `len(results)`. Never
on `totalSize`, and never report it as a count.

`_links.next` here is `/rest/api/search?next=true&cursor=…` — **context-relative**,
like the rest of v1, so it needs the `/wiki` prefix. `resolveNext` appends to the
base without that prefix, so it is wrong for this endpoint.

**4. The next link does not carry every parameter you sent.** It reproduces `cql`,
`limit`, the cursor, and a `start` that does nothing — but **not `excerpt`**. So a
walk that sets `excerpt=` on the first request and then follows the cursor asks
for page one and page two differently, and only the first page's excerpts are the
ones that were asked for. It is currently invisible because the server's default
happens to equal `highlight`; it stops being invisible the moment that default
changes or a different value is wanted.

Re-attaching it means editing the *parsed* next URL rather than appending, since
the link already carries a query string — appending `?excerpt=…` to it produces a
second `?` and a malformed request. Do not assume any other parameter survives the
cursor either; only `cql` and `limit` were confirmed to.

### The CQL index lags; v2 does not

Immediately after archiving a page, v2 reported `status: "archived"` while CQL
still returned it as current, and `totalSize` disagreed with an empty `results`
array. A minute later CQL had caught up and reported nothing.

So CQL answers a question about the index, not about the content. Anything that
must be correct *now* — above all a pre-flight duplicate check, where being
stale means publishing a conflict — has to use v2.

### What holds a title, and what does not

Both established by attempting the create:

| a new page clashing with | result |
|---|---|
| an **archived page** in the same space | **400** `A page with this title already exists: A page already exists with the same TITLE in this space` |
| a **folder** in the same space | **200** — the page is created, the two coexist |

An archived page keeps its title reserved even though it is invisible in the
page tree. A folder does not reserve anything.

Note also that archiving, unlike trashing, does **not** null `parentId`: sampled
archived pages kept `parentId` and `parentType: "page"`. Compare
[folders.md](folders.md), where a trashed node reports `parentId: null`.

## Full text: verified 2026-08-14

Everything above is about matching a title exactly. Full text is the same
endpoint and a different set of traps.

### `siteSearch ~` ranks; `text ~` does not

This is the one that decides whether the feature works. For the query
`deploy runbook`, `type = page` in both cases:

| # | `text ~ "deploy runbook"` | `siteSearch ~ "deploy runbook"` |
|---|---|---|
| 1 | Monitor Base Load Engineer Runbook | **Deployment runbook** |
| 2 | Base Load Engineer's Hand-off Log | **Runbook: Grafana deploys** |
| 3 | Local Models Selection | **Runbook: Prod deployment** |
| 4 | Merino Curated Recommendations | **Runbook: Stage deployment** |
| 5 | LCM Team Resources — Global Operations and Governance | **Runbook: deploying changes** |
| 6 | Treeherder | **SubPlat: Runbook** |

Same corpus, near-identical totals (`text ~` reported 3060, `siteSearch ~`
3071–3073), completely different ordering. `text ~` is the field Atlassian
documents and the one confluence-cli uses; on this evidence it is the wrong one.

**`siteSearch` is not documented anywhere.** It is absent from Atlassian's [CQL
field reference](https://developer.atlassian.com/cloud/confluence/advanced-searching-using-cql/);
the only reason we know it exists is that Confluence's 400 for an invalid field
volunteers it (`Did you mean one of : space, subtype, space.key, siteSearch,
space.type`). Meanwhile `text` **is** documented — as "a 'master-field' that
allows you to search for text across a number of other text fields. These are the
same fields used by Confluence's search user interface."

That is the irony worth keeping in mind: the documented field claims to be what
the search UI uses, and the undocumented one is what actually behaves like it.

**There is no client-side fallback.** `score` is `0.0` on every row of every
query sampled, so the server's order is the only order there is. Nothing may
re-sort a full-text result set, and `search` deliberately does not.

`siteSearch` costs two things. It decorates the response (see below) — and it has
a parser bug that silently answers a different question.

### `siteSearch` is dropped when it is the middle clause of three

**Verified 2026-08-16**, later than the rest of this section — and not by probing.
It was found by running the shipped command on a real query, after everything else
here had been established and committed.

**This is the worst failure in this file: it turns a scoped search into a listing
of the whole space, and looks like a working search while doing it.**

For a space (`SRE`) holding 1122 pages, of which 15 mention "netlify":

| query | totalSize | first hit |
|---|---|---|
| `type = page and space = "SRE"` | 1122 | BigQuery Reservations (aka Slots) |
| `type = page and siteSearch ~ "netlify" and space = "SRE"` | **1122** | BigQuery Reservations (aka Slots) |
| `siteSearch ~ "netlify" and type = page and space = "SRE"` | 15 | **Netlify** |
| `type = page and space = "SRE" and siteSearch ~ "netlify"` | 15 | **Netlify** |

The second row is byte-identical to the first. The `siteSearch` clause is
**discarded**, not merely deprioritized, and no error is returned.

Position is the whole trigger — first and last are safe, middle is not:

| arrangement | honored? |
|---|---|
| `siteSearch` alone | yes |
| `siteSearch` first of two, either order | yes |
| `siteSearch` first of three | **yes** |
| `siteSearch` last of three | **yes** |
| `siteSearch` middle of three | **no** |
| `siteSearch` first or last of four | yes |

Parenthesizing does not help (`(type = page) and (siteSearch ~ …) and (space = …)`
still returns 1122), nor does `space.key`, `space in (…)`, an unquoted key, or
`type in (page)`. Reproduced against a second space (`CLOUDSERVICES`: 561 pages,
0 mentioning "netlify" — the broken form returned all 561).

**`text ~` is immune.** In the middle of three it returned the honest 14 both
ways, so this is specific to `siteSearch`.

`cqlcontext={"spaceKey":"SRE"}` is **not** a space filter — it returned the full
sitewide 48. Do not reach for it to sidestep this.

So markfluence emits `siteSearch ~ "q" and text ~ "q" and type = … and space = …`:
`siteSearch` leads because it must, and the redundant `text` clause is a floor. If
the clause is ever dropped again — by a reordering here or a change upstream —
`text` still constrains the results to content containing the query, so the
failure degrades to a worse order rather than to every page in the space. It costs
the handful of hits `siteSearch` matches and `text` does not (15 → 14 above);
ranking is unchanged.

### Full text is AND across terms, not a phrase

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
makes no difference either way.

So adding a word narrows the search, and a user who expects a quoted phrase does
not get one. That belongs in the help text, not just here.

### An untyped query returns content markfluence cannot use

`text ~ "deploy"`, first 100 rows: **94 page, 3 attachment, 2 database, 1
comment**. An attachment id and a comment id are dead ends for every markfluence
verb.

`siteSearch ~ "deploy"` returned 99 pages out of the first 99 — its ranking
floats pages up on its own. That is luck at the top of the list and not a filter:
untyped reported 3358 against 3073 for `type = page`, so roughly 285 non-page
hits are further down. Type pinning matters for an unbounded walk even where the
first page looks clean.

Valid `type` values, all with `siteSearch ~ "deploy"`:

| type | hits |
|---|---|
| `page` | 3073 |
| `comment` | 144 |
| `attachment` | 127 |
| `blogpost` | 6 |
| `database` | 2 |
| `whiteboard` | 2 |
| `folder` | **0** |
| `embed` | 0 |
| `space` | 0 |

An invalid value 400s and **enumerates the whole vocabulary**, which is worth more
than the sampling above:

```
Unsupported value for type, got : bogustype, expected one of :
[space, user, page, blogpost, comment, attachment, database, whiteboard,
 slide, embed, folder,
 com.atlassian.confluence.extra.team-calendars:calendar-content-type,
 com.atlassian.confluence.extra.team-calendars:space-calendars-view-content-type,
 ac:com.mxgraph.confluence.plugins.diagramly:drawio-diagram]
```

So the list is open-ended and partly **installation-specific** — the last three
entries are app-provided content types, which is the reason a search result's
`type` must never be validated against a closed set.

Note also that a value being *accepted* does not mean it *filters*: `user` is in
that list, and `type = user` returns ordinary content rather than users — see
[below](#some-rows-have-no-content-object-at-all).

**Full text cannot see folders**, structurally — a folder has no body. So unlike
`find`, a full-text search has no folder half: one request, no merge, no
`extensions.position` ordering to preserve. It cannot see archived pages either,
for the reasons already given above.

`type in (page, folder)` parses, so a list is available if one is ever wanted.

### The row-level `title` is escaped and decorated; `content.title` is not

The row's own `title` is **HTML-escaped and carries highlight markers**:

```
row.title      'Base Load Engineer&#39;s Hand-off Log'
content.title  "Base Load Engineer's Hand-off Log"

row.title      'Deployment @@@hl@@@runbook@@@endhl@@@'
content.title  'Deployment runbook'
```

`content.title` was clean for both, on every row sampled, in both modes. Checked
against 245 folder rows as well: 9 had `&` in the row title (`Apps &amp;
Actions`, `Fastly Developers&#39; guide`) and `content.title` was correct in every
case, matching what v2 `/folders/{id}` returns for the same id.

**Always read `content.title`.** This is why `find`'s folder half is correct as
written, and it must not be "simplified" to the row's `title`.

`content._links.webui` was byte-identical to the row's `url` on all 50 rows
sampled, so either works for a URL; `content._links.webui` is preferred for
consistency with the rule above.

### The excerpt needs cleaning, and `excerpt=` is a live parameter

`excerpt` exists **only** at the row level, so there is no clean variant to
prefer. Over 50 rows fetched with `excerpt=highlight`:

- **40 carried `@@@hl@@@` / `@@@endhl@@@`**
- 40 contained newlines
- 2 contained HTML entities
- 0 were empty

So stripping the markers and unescaping entities is required for the common case,
not defensive coding.

| `excerpt=` | result |
|---|---|
| *(unset)* | 150–450 chars — same as `highlight` |
| `highlight` | 150–450 chars, markers present |
| `indexed` | truncated to **50 chars** |
| `none` | empty string, 200 |
| `bogus` | **empty string, 200** |

An unrecognized value is not an error. markfluence passes `excerpt=highlight`
explicitly anyway, to pin the behavior that was actually tested — with the
consequence that if Atlassian ever renames the value, excerpts silently go empty
rather than failing. **That is the thing to watch here.**

### An empty query is a 500, not a 400

```
type = page and siteSearch ~ ""   →   500
java.lang.NullPointerException: query parameter is required.
```

Compare `title = ""`, which is a 400. So rejecting a blank query before sending
anything is load-bearing rather than tidy: without the guard a usage error
surfaces as an operational failure carrying a Java exception message.

### Some rows have no `content` object at all

`type = space` returns 388 rows on the site tested, and none of them has a
`content` object. The addressable data is in a sibling `space` object:

```json
{ "entityType": "space", "title": "Engineering Workflow", "url": "/spaces/EW",
  "space": { "key": "EW", "name": "Engineering Workflow", "status": "current" } }
```

A space has no id in that payload, only a key, so such a row cannot become a
result whose `id` is required — and no markfluence verb takes a space anyway.
Skipping it is right; skipping it **silently** is not, because a query matching
only these rows would otherwise report a successful empty result. `search` counts
them and reports the count.

Note that `type = user` does **not** return user rows: it returned ordinary
content (90250 hits, i.e. effectively unfiltered), as did `space.type = global`.
Spaces are the confirmed content-less case; users are not reachable this way.

### Fields that look useful and are not

- **`score`** — `0.0` on every row of every query sampled. Not usable for
  ranking, not worth reporting.
- **`breadcrumbs`** — an empty array on all 50 rows sampled. There is no free
  ancestor path here.
- **`content.status`** — `current` on everything, necessarily, since CQL cannot
  see archived content. A search result carrying a status field would imply a
  distinction it cannot make.
- **`archivedResultCount`** — already covered above: `0` everywhere, including
  where archived-space content was demonstrably added.

`resultGlobalContainer` **is** populated, as
`{"title": "Ecosystems", "displayUrl": "/spaces/PXI"}` — the space's *display
name*, which `SpaceKeyFromWebUI` cannot give us. Unused, since the key is the
handle every command takes, but it is the only place the human-readable space
name comes free.

## What this means for markfluence

- **Full text means `siteSearch ~`, and the result order is the server's.** With
  `score` at `0.0` there is nothing to re-rank with, so a truncating `--limit`
  keeps the *top* N and must say when it dropped anything.
- **The `siteSearch` clause must come first, and must be backed by a `text`
  clause.** Middle-of-three gets it silently discarded, which returns the whole
  space dressed up as a search result. The clause order in `buildTextCQL` is
  load-bearing and pinned by a test.
- **Pin the content type.** An untyped full-text query returns attachment,
  comment and database ids, none of which any command accepts.
- **Read `content.title`, never the row's `title`.** The latter is
  entity-escaped and marker-decorated.
- **Clean every excerpt**: strip `@@@hl@@@`/`@@@endhl@@@`, unescape entities
  once, then collapse whitespace. 40 of 50 rows needed it.
- **Reject a blank query locally**, since the server answers with a 500.
- **`find` needs both APIs.** v2 for pages, current and archived; CQL for
  folders. Neither covers the other's blind spot, so dropping one silently drops
  a category of answer.
- **A folder hit is discovery, never duplicate evidence.** Folders cannot clash
  with a page title, so they must stay out of `create`'s duplicate check. They
  are in `find` because a folder id is a legitimate `--parent`.
- **An archived page must be in the duplicate check.** It reserves the title, and
  a check that misses it lets validation pass and the POST fail — which, for a
  batch `create`, means failing in the create phase after earlier pages already
  exist, defeating the two-phase design.
- **The `/search` pager cannot be `listV1`.** Different termination rule,
  different next-link resolution, an ignored `start`, and an unreliable count.
  Three of those fail silently.
- **Escape every user-supplied value going into CQL**, in the client, at the one
  place the query is built.

## Unverified

- **Whether the escaper matches a real title containing `"`.** The escaped form
  parses and returns 200, but no page with a quote in its title existed to match
  against.
- **Whether a folder can be archived**, and what `type = folder` reports if so.
- **Whether trashed content ever reaches CQL.** Not probed; there is no `status`
  field to ask with.
- **What `archivedResultCount` actually counts.** `0` on every sample, including
  ones where archived-space content was demonstrably added to the results.
- **Why `score` is `0.0`.** Whether the field is unpopulated on this endpoint or
  genuinely computed as zero was not established. Either way it is unusable, and
  the ranking is demonstrably not arbitrary — `siteSearch` returns a sensible
  order while reporting the same zero.
- **Whether `siteSearch` is stable.** It is not in Atlassian's CQL field
  reference; it is named as valid only by the error message for an invalid field.
  An undocumented field can change without notice, and this one has already been
  caught being **silently discarded** depending on where it sits in the query, so
  treat any change in its behavior as plausible. The redundant `text ~` clause
  exists to bound the damage.
- **Whether the middle-clause bug has other shapes.** First, last, and
  middle-of-three were each tested, at three and four clauses, but the rule
  "position decides" is an empirical description of a parser bug and not a
  specification. Nothing establishes that first-position is safe in *every*
  arrangement, only in the ones markfluence emits — which is why the emitted forms
  are pinned by test rather than trusted.
- **Whether `text ~` is genuinely immune** or merely was not caught. It returned
  the honest count in both positions of three, which is evidence, not proof.
- **What `siteSearch ~` does differently from `text ~`.** Only that it ranks
  better was established, not why. Totals differ slightly (3071 vs 3060 for
  `type = page`), so the matching is not identical either.
