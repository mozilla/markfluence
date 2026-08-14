# Search: finding content by title

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

### `/search` pagination: cursor-only, and three traps

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

## What this means for markfluence

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
