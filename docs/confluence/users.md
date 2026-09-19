# Users

Two routes answer questions about people, in opposite directions, and the
difference between them is the first thing to get straight:

| route | direction | sees a deactivated account? |
|---|---|---|
| `GET /wiki/rest/api/user?accountId=…` | an id → a display name | **yes** |
| `GET /wiki/rest/api/search/user` | a name → account ids | **no** |

markfluence uses the first to render a mention (#91, via `client.LookupUser`)
and the second to find one to write (#143, via `client.SearchUsers`). They share
a noun and nothing else. Sharing code between them would take the second's
blind spot and give it to the first, which resolves a departed colleague's name
perfectly well today.

The profile URL a mention points at, and why it is Atlassian Home rather than
the site, is in [links-and-anchors.md](links-and-anchors.md).

## Verified 2026-09-15

Probed against mozilla-hub with an **unscoped personal token** over the site
domain. Queries below are the `cql` parameter to
`GET /wiki/rest/api/search/user`; every request answered 200 unless stated.

### The route takes user-specific CQL, and ignores everything else silently

| query | result |
|---|---|
| `user.fullname~"kahn"` | 1 row — William Kahn-Greene |
| `user.accountId="60c36d0718e9f60071326951"` | 1 row — the same person |
| `user.fullname~"kahn" or user.fullname~"reid"` | 4 rows, so `or` parses |
| `title~"kahn"` | **0 rows, 200** |

That last one is the trap this directory opens with: a field the route cannot
answer is not an error, it is an empty result, which is indistinguishable from
"nobody by that name". A clause added to a query here must be tested by showing
it returns *fewer* rows than the unclaused query, never merely that the request
succeeded.

Each row carries `user.accountId`, `user.displayName` and `user.type`.

### `~` is a word-prefix match, and the words are ordered

| query | William Kahn-Greene? |
|---|---|
| `user.fullname~"kahn"` | yes |
| `user.fullname~"Kahn-Greene"` | yes |
| `user.fullname~"kahn*"` | yes |
| `user.fullname~"william kahn"` | yes |
| `user.fullname~"ahn"` | **no** |
| `user.fullname~"ahn-greene"` | **no** |
| `user.fullname~"kahn william"` | **no** |

`user.fullname~"*"` matches everybody. A fragment beginning mid-token matches
nothing at all, and the failure is a confident empty result — so a caller has to
be told the rule, which is why `user-find`'s help states it rather than leaving
an author to conclude the person has no account.

### A page caps at 100 rows while echoing back the limit you asked for

| requested `limit` | echoed `limit` | rows returned |
|---|---|---|
| 3 | 3 | 3 |
| 100 | 100 | 100 |
| 101 | 101 | **100** |
| 250 | 250 | **100** |
| 500 | 500 | **100** |

The echo is the dangerous part: nothing in the response distinguishes "your
limit was honoured" from "your limit was clipped to 100".

### Paging is `start`/`limit` offset, and there is no `next` link

`_links` holds only `base` and `context` — never `next`, on any page, including
a full one. `start` is honoured (unlike `/search`, which ignores it). Walking
`user.fullname~"a"` at `limit=100`:

| `start` | rows | first row |
|---|---|---|
| 0 | 100 | `[TEMPLATE] ASCII art generator` |
| 100 | 100 | Amedyne Moya |
| 200 | 100 | Bastien Abadie |
| 300 | 4 | Workato Automation |
| 400 | 0 | — |

So 304 people, and a short page is the only end-of-results signal available.

**This is why the route cannot go through `listV1`.** That helper asks for
`v1PageSize = 250` and terminates when a page is shorter than its request. Here
it would ask for 250, receive 100, and return a truncated set with no error on
any query matching more than 100 people. `client.userPageSize` exists for this,
with the measurement beside it, and `TestPageCapDoesNotTruncate` is the
regression.

### `totalSize` counts the current page, not the result set

| request | `totalSize` | rows | actual matches |
|---|---|---|---|
| `limit=3` | 3 | 3 | 304 |
| `limit=100` | 100 | 100 | 304 |
| `limit=500` | 100 | 100 | 304 |

It is not an estimate, the way `/search`'s is
([search.md](search.md#search-pagination-cursor-only-and-four-traps)) — it is a
different quantity. Nothing may read it, and "more matches exist" has to come
from asking for one row more than the caller wants.

### A deactivated account is absent, and no filter brings it back

`user.fullname~"reid"` returns Ashley Roybal-Reid, Brittany Reid and Kathy Reid,
but not `Mark Reid (Deactivated)`. `user.fullname~"lonnen"` returns nothing
though `. Lonnen (Deactivated)` exists and is mentioned on real pages.

`sitePermissionTypeFilter` cannot lift it:

| `sitePermissionTypeFilter` | `user.fullname~"reid"` |
|---|---|
| *(absent)* | the same three |
| `none` | the same three |
| `all` | the same three |
| `externalCollaborator` | 0 rows |

Meanwhile `GET /wiki/rest/api/user?accountId=…` answers 200 with
`Mark Reid (Deactivated)` for the same person. So the exclusion is a property of
the *directory index*, not of the account, and the asymmetry is the point:
markfluence can render a departed colleague's name but cannot find them by one.

### An empty query is a 500

`user.fullname~""` answers **500** with
`java.lang.reflect.UndeclaredThrowableException: null`. Same shape as
`/search`'s empty query ([search.md](search.md#an-empty-query-is-a-500-not-a-400)),
same remedy: refuse an empty name locally, before any request, so it reports as
the usage error it is rather than as a server failure.

### `user.type` is `known` for everything, people or not

`user.fullname~"a"` returns `[TEMPLATE] ASCII art generator` and
`Workato Automation` alongside people, and every row — those included — reports
`"type": "known"`. The field therefore identifies nothing, which is why
`user-find --json` does not carry it: a field named `type` whose only observed
value is `known` invites a consumer to filter non-humans out with it and get
nothing for the effort.

### CQL injection is live here too

`user.fullname~"kahn" or type=user` parses, and the parser accepts backslash
escapes, so `internal/client`'s existing `escapeCQL` is correct for this route.
Escaping is not optional: an unescaped quote in a name ends the string literal
and the rest of the value becomes query syntax
([search.md](search.md#cql-is-an-injection-surface)).

### The scope is `read:content-details:confluence`, and nothing else implies it

**Verified 2026-09-18** with a scoped service-account token, which is what this
took: the earlier probes all used an unscoped personal token, whose 200 said
nothing about a scoped token's grants.

The token held the nine scopes the README listed at the time plus
`read:confluence-content.all`, `read:confluence-props`,
`write:confluence-props` and `write:confluence-content` — ten of them classic,
five granular — and `user-find` still failed:

```
HTTP 401: {"code":401,"message":"Unauthorized; scope does not match"}
```

Every other command worked with that token. So the route's only `Current`
scope, `read:content-details:confluence`, is required and is implied by nothing
else — not by the classic `read:confluence-content.summary`, not by
`read:confluence-content.all`, and not by `search:confluence`, which covers the
*other* two search routes. This is [api.md](api.md#scopes)'s classic/granular
warning landing exactly where it was predicted to.

It is now in the README's copy-pasteable list. Note the shape of the failure
for anyone debugging one: a 401 whose body says `scope does not match`, which
is markfluence's third auth phrasing and the one that names the cause plainly.

## Verified 2026-09-18 — the identity routes, and the space survey (#171)

### `GET /user/current` and `GET /user?accountId=` carry the same shape

Both `read:confluence-user`, which every working token has — unlike the
directory route above. Measured fields: `accountId`, `displayName`,
`publicName`, `email`, `accountType` (`atlassian` for a person, `app` for a
service account), `accountStatus`, `isExternalCollaborator`, `isGuest`.

`accountType` is the field worth knowing about: an `app` account is a
service-account token, is in none of the groups a person is, and is therefore
the explanation for most permission surprises.

### `?expand=personalSpace` gives a personal space, and the key cannot be constructed

The expansion returns the space's `id`, `key`, `name`, `type` and `status`.

**Do not build the key from the account id.** `~{accountId}` is right for some
accounts (`~60c36d0718e9f60071326951`) and wrong for others: this instance
carries personal spaces keyed by **email** (`~aalexander@mozilla.com`,
`~amuntner@mozilla.com`) in the same directory. Both forms are live, so the key
is a lookup and never a format.

It is also genuinely optional: an `app` account returns `personalSpace: null`
while a person returns a space, so the field is absent for every
service-account token rather than only in theory.

The expansion also carries **`_links.webui`** (`/spaces/~60c36d07…`,
context-relative like the rest of v1), so the browser URL comes from the
response rather than being assembled. That matters for the same reason the key
does: an email-keyed personal space links as `/spaces/~amuntner@mozilla.com`
with the `@` **unescaped**, so building the path locally would mean inventing
an escaping rule, and getting it wrong for exactly the keys that are already
the awkward case.

### `GET /rest/api/space?expand=operations` answers "where can this account write?"

The operations expansion works on the **collection**, not only the single-space
route, so the survey is one walk rather than one request per space. `create:page`
means the account may create pages there; `administer:space` means it
administers the space — an admin's row also carries `archive:space`,
`delete:space`, `export:space` and six `manage_*` operations, so the one
Atlassian names for the permission is the one that will not drift.

Measured with two accounts: a scoped service-account token saw 525 spaces, 97
writable, 2 administered; a personal token saw 533, 102, 11.

**The route answers for the authenticated credentials and takes no account
id**, which is why `user-info --spaces` refuses an `ACCOUNT_ID` rather than
reporting the caller's own access beside somebody else's name.

Asking it for another account is possible and expensive, which is worth
writing down so nobody re-derives it. Both pieces exist and both answer a
non-admin: `GET /rest/api/user/memberof?accountId=` lists an account's groups
(42 for one person here) and `GET /api/v2/spaces/{id}/permissions` lists a
space's grants as principal/operation pairs. But the grants are **per space**,
run **250+ rows each and paginate**, and there are 533 spaces — over a thousand
requests against three for the caller's own answer. It also means resolving
Confluence's permission model locally: group grants, individual grants,
defaults, space versus global. A subtle error there reports another person's
access with full confidence, which is the failure mode this directory exists to
prevent.

Neither is page *edit*, which is not a space property at all
([page-status.md](page-status.md)): `create:page` is a space grant and page
`update` appears only on a page's own operations.

### **`/rest/api/space` returns short pages that are not the end**

The trap, and the reason this route does not go through `listV1`.

```
limit=250&start=0    ->  200 rows
limit=250&start=200  ->  250 rows
limit=500&start=0    ->  500 rows
```

The real total was 525. `listV1` stops when a page comes back shorter than it
asked for, so under it this route reports **200 spaces and 39 writable**,
silently, with no error — which is exactly what the first version of this probe
did before `start=200` was tried. Termination here is an **empty** page and
nothing else, and the offset advances by rows *returned* rather than by the
limit requested, because the two differ.

This is the [README](README.md)'s opening trap arriving from a new direction:
the cheap answer looked right and was not.

## Unverified

- **Whether `sitePermissionTypeFilter` has any effect at all.** Three values were
  measured and none changed a result set. It may matter on an instance with
  external collaborators; this one appears to have none matching the probes.
- **Paging past 304 rows.** The largest result set available here terminated
  well before any plausible server-side ceiling on `start`. `start=1000`
  returned 0 rows rather than an error, which is consistent with "past the end"
  and proves nothing about a deeper limit.

## What this means for markfluence

- **The two user routes stay separate.** `LookupUser` resolves any account;
  `SearchUsers` searches an index that omits deactivated ones. Neither is a
  special case of the other.
- **This route gets its own pager and its own page size.** Not `listV1`, whose
  250-row request the 100-row cap would turn into silent truncation.
- **Nothing reads `totalSize`.** "More exist" is a flag derived from fetching one
  row more than was asked for, and `user-find --json`'s `summary.truncated` is
  that flag.
- **The match semantics are reported, not worked around.** A word-prefix,
  ordered match is the server's behaviour; padding a query with wildcards to
  fake substring matching would change which people a name finds and make the
  command's answer depend on markfluence's guess.
