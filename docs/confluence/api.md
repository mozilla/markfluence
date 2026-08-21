# The REST API

## Two API versions, and why both

Pages and content properties use **v2** (`/wiki/api/v2/...`). Attachment writes
and the user lookup use **v1** (`/wiki/rest/api/...`) because v2 does not cover
them.

Child enumeration is the third v1 exception, and the least obvious one, since v2
does have children routes. It cannot be used: v2 refuses a folder id on every
page route, so there is no way to list what is inside a folder, and
`/pages/{id}/children` silently omits folders from a page's children — returning
a wrong answer rather than a partial one. See [folders.md](folders.md).

**Verified 2026-08-07.** Against the gateway, `GET /wiki/api/v2/pages`,
`GET /wiki/rest/api/content/{id}/child/attachment`, and
`GET /wiki/rest/api/user/current` all return 200. The path suffixes are
identical whether the request goes to the site domain or the gateway, which is
why every call in `internal/client` is written against one `baseURL`.

## The platform API gateway

With a cloud ID configured, requests go to
`https://api.atlassian.com/ex/confluence/{cloudId}` instead of the site domain.
Basic auth is unchanged; only the base URL moves.

**The cloud ID is not a secret. Verified 2026-08-07:**

```console
$ curl -s https://YOUR-SITE.atlassian.net/_edge/tenant_info
{"cloudId":"<a uuid>"}
```

No credentials. That is why it can be a `--cloud-id` flag while the token cannot.

**A scoped token requires the gateway. Verified 2026-08-20** with a scoped
service-account token — the credential the earlier attempt lacked. An unscoped
personal token returns 200 against *both* the site domain and the gateway, so it
cannot distinguish the two cases; a scoped one can.

Two calls the token *is* scoped for, each sent twice with the same basic auth:

| request | gateway | site domain |
|---|---|---|
| `GET /wiki/rest/api/user/current` | **200** | **401** |
| `GET /wiki/rest/api/space/{key}` | **200** | **401** |

The site-domain 401 is a Tomcat HTML error page, not a JSON API error — the
request is rejected before it reaches Confluence's API layer. Nothing about the
scopes changed between the two columns, so the base URL is the only variable.

### A scope failure is a 401, not a 403

**Verified 2026-08-20.** Through the gateway, a request the token is *not*
scoped for returns:

```json
{"code":401,"message":"Unauthorized; scope does not match"}
```

The status alone does not identify it, and neither does the status of the other
auth failures. **Corrected and re-measured 2026-08-21** — an earlier version of
this section guessed that a bad password also returns 401, and it does not:

| what is wrong | route | status | body |
|---|---|---|---|
| token lacks the scope | v1 or v2 via gateway | **401** | `{"code":401,"message":"Unauthorized; scope does not match"}` |
| scoped token sent to the site domain | site domain | **401** | Tomcat HTML, no JSON at all |
| credentials wrong, revoked, or absent | **v1** | **403** | `caller cannot access Confluence` (`/user/current`) or `Current user not permitted to use Confluence` (`/search`) — the two phrasings differ |
| credentials wrong, revoked, or absent | **v2** | **404** | `{"errors":[{"status":404,"code":"NOT_FOUND","title":"Not Found","detail":null}]}` |

Two of these are worth staring at.

**A rejected credential is a 404 on every v2 route.** Not 401, not 403. Since
markfluence reads pages over v2, a revoked token made `read` answer
`page 2848423944 not found` for a page that exists — and the obvious next move,
go and check the page id, is wrong for every id. It is distinguishable, but only
by the title: **every genuine v2 404 names what it could not find**, and the
authentication one does not.

| request | title |
|---|---|
| missing page, good credentials | `Cannot find a page with id [999999999999]` |
| missing folder, good credentials | `Content with id: [999999999999] not found` |
| missing page's properties, good credentials | `Could not find page with id [999999999999]` |
| **existing page, bad credentials** | **`Not Found`** |

`client.HTTPError.RejectedCredential` matches on that bare title, and `notFound`
uses it so the `…OrNil` helpers stop reading a rejected credential as "absent".

**A 403 does not mean what it looks like it means.** It is the *credential*
failure on v1, not a permission failure — sending no `Authorization` header at
all returns exactly the same body. A genuine permission denial is a different
403, which is why the hint fires only on the two measured phrasings and stays
silent otherwise.

Scopes are fixed when a token is issued, so a missing one needs a *new* token,
not an edited one.

Two bases exist for a reason: `BaseURL()` is where requests go, `SiteURL()` is
always the site. Anything a human will see — printed URLs, and the `baseURL`
handed to the converter, since rewritten links get published *into* the page —
must use `SiteURL()`, or readers get gateway URLs they cannot open.

## v1 collections paginate differently from v2

A v1 collection cannot be paged with `_links.next` the way v2 paths can.

> **`/rest/api/search` is the exception, and it fails the other way.** It ignores
> `start` entirely and can only be paged by its cursor, and a short page there
> does *not* mean the end. Applying the offset rule below to it silently
> truncates the results. See [search.md](search.md).

**Verified 2026-08-07** against a page with 3 attachments:

| request | `_links` keys |
|---|---|
| `?limit=50` (all 3 fit) | `base`, `context`, `self` — **no `next`** |
| `?limit=1` | `base`, `context`, `next`, `self` |

Two problems, hence the `start`/`limit` offset paging in `ListAttachments`:

1. `next` is **absent** when the results fit one page, so its absence cannot be
   used as a loop termination signal without special-casing.
2. When present it is `/rest/api/content/…?next=true&limit=1&start=1` — relative
   to the `/wiki` context, not a v2-style path. The same response reports
   `_links.base = https://SITE/wiki` and `_links.context = /wiki`.

## v2 collections paginate with a cursor

`_links.next` on a v2 collection is a site-relative absolute path carrying the
cursor and the limit, e.g. `/wiki/api/v2/pages?limit=1&title=…&cursor=…`.

**Verified 2026-08-14** against `GET /wiki/api/v2/pages?title=…&limit=1` on a
title with two matches. Because the path already includes the `/wiki` prefix,
`resolveNext` appends it to the base unchanged and the gateway's
`/ex/confluence/{cloudId}` segment survives. That is what `listV2` pages with,
and it is why `ListContentProperties` and `SearchPagesByTitle` can share one
helper.

Unlike v1, absence of `next` is a reliable end-of-collection signal here.

## Attachment downloads and the redirect

`_links.download` is an **API** path — `/rest/api/content/{page}/child/attachment/{id}/download`
— not the `/download/attachments/...` UI path. Being an API path, it works
through the gateway.

**Verified 2026-08-07**, following the chain by hand:

```
GET  https://SITE/wiki/rest/api/content/…/download   (Authorization: Basic …)
302  Location: https://api.media.atlassian.com/…?…token=…
GET  that URL with NO Authorization header
200  171 bytes
```

The redirect target is a different host and carries **its own credential in the
query string**. Fetching it with no `Authorization` header at all succeeds. Go's
default redirect policy drops `Authorization` on a cross-host hop, which is
exactly right here.

> **Never install a `CheckRedirect` that forwards headers.** It would send site
> credentials to a host that does not need them and did not ask for them.

Note the join: `_links.download` is context-relative, so the full URL is
`<base>/wiki` + the path. Joining it onto the bare site URL 404s.

## Retries

### What Atlassian says

**Transcribed 2026-08-14** from
[Confluence rate limiting](https://developer.atlassian.com/cloud/confluence/rate-limiting/)
and the identical
[Jira platform](https://developer.atlassian.com/cloud/jira/platform/rate-limiting/)
guidance:

- A 429 carries **`Retry-After`** — "only returned with 429 responses. Indicates
  how many seconds to wait before retrying" — along with `X-RateLimit-Limit`,
  `X-RateLimit-Remaining`, `X-RateLimit-Reset`, `X-RateLimit-NearLimit` and
  `RateLimit-Reason`.
- "Some transient 5xx responses (such as 503) may also include a `Retry-After`
  header. While these are not rate limit responses, you can handle them with
  similar retry logic."
- "Only retry if the API is idempotent and the response includes a `Retry-After`
  header."
- "Use exponential backoff and add random jitter to delays to avoid the
  thundering herd problem."
- Nothing distinguishes **500** from 503.

> **Not verified first-hand, on purpose.** Confirming these against a live site
> means deliberately tripping rate limits, which degrades a shared instance for
> everyone else using it — a poor trade for a header the vendor publishes. If a
> 429 ever happens during ordinary use, `--debug` prints the headers, and this
> section can be upgraded to **Verified** with no artificial load.

### What markfluence does

| response | retried? |
|---|---|
| 429 | always, any method — the request was rejected before processing |
| 502 / 503 / 504 | idempotent methods only |
| any other 5xx **with** `Retry-After` | idempotent methods only — this is how a 500 becomes retryable |
| any other 5xx **without** `Retry-After` | no |
| network error | idempotent methods only |
| anything else | no |

`send` centralizes all of it. Backoff is exponential from `baseBackoff`, jittered
by a random factor in [0.7, 1.3], capped at `maxBackoff`; a server-supplied
`Retry-After` is used as given (still capped) and is **not** jittered, since it
is an instruction rather than a guess.

The 500 rule is Atlassian's own — retry when the server asks to be called back —
and it avoids having to guess whether a given 500 is transient. A bare 500 is
usually a deterministic rejection of that particular request, so retrying it just
buys backoff on something that will not succeed.

502/503/504 stay unconditional rather than also requiring `Retry-After`.
Conforming strictly there would stop retrying a bare 502 from a proxy, which is
transient in practice.

**Worst case is long and, without `--debug`, silent.** Five attempts at the
120-second upload timeout plus backoff is roughly twelve minutes on one
attachment. That is why retry decisions are reported through a hook.

### Retrying a versioned PUT can turn a success into an error

`isIdempotent` counts `PUT` as safe to retry, which is true of HTTP `PUT` in
general and **not** true of a `PUT` carrying an optimistic-concurrency version.

`UpdatePage` sends `version.number = N`. If it lands and the response is lost,
the retry re-sends version N, which Confluence refuses because N already exists —
so a successful update surfaces as a failure. `SetContentProperty` has always had
a bespoke retry-once-that-re-reads-first for exactly this shape.

`UpdatePage` now recovers the same way: on any error it re-reads the page and
treats the write as successful only when the version, the title, **and** the
stored body all match what was sent. Version alone would be wrong — a concurrent
human edit could have created that version, and claiming success over someone
else's content is far worse than reporting a failure that actually succeeded.

**Unobserved:** what status a stale-version PUT actually returns. The recovery
triggers on *any* error specifically so nothing depends on the answer; guessing a
status and getting it wrong would leave the recovery silently never firing.

## Scopes

**Derived 2026-08-20** from Atlassian's own OpenAPI documents, one lookup per
call markfluence actually makes:

- v1: `https://dac-static.atlassian.com/cloud/confluence/swagger.v3.json`
- v2: `https://dac-static.atlassian.com/cloud/confluence/openapi-v2.v3.json`

Each operation carries an `x-atlassian-oauth2-scopes` array whose entries have a
`state`. The table takes the `Current` entries; `Beta` alternatives are noted
below.

| call | ver | method + path | scope |
|---|---|---|---|
| `GetPage`, `GetPageOrNil` | v2 | `GET /pages/{id}` | `read:page:confluence` |
| `SearchPagesByTitle` | v2 | `GET /pages` | `read:page:confluence` |
| `CreatePage` | v2 | `POST /pages` | `write:page:confluence` |
| `UpdatePage` | v2 | `PUT /pages/{id}` | `write:page:confluence` |
| `GetFolderOrNil` | v2 | `GET /folders/{id}` | `read:folder:confluence` |
| space key to id | v2 | `GET /spaces` | `read:space:confluence` |
| `ListContentProperties` | v2 | `GET /pages/{id}/properties` | `read:page:confluence` |
| `SetContentProperty` (create) | v2 | `POST /pages/{id}/properties` | `read:page:confluence`, `write:page:confluence` |
| `SetContentProperty` (update) | v2 | `PUT /pages/{id}/properties/{propId}` | `read:page:confluence`, `write:page:confluence` |
| `GetUser` | v1 | `GET /user` | `read:confluence-user` |
| `searchCQL` | v1 | `GET /search` | `search:confluence` |
| attachment upload | v1 | `POST /content/{id}/child/attachment` | `write:confluence-file` |
| attachment re-upload | v1 | `POST /content/{id}/child/attachment/{attId}/data` | `write:confluence-file` |
| `DownloadAttachment` | v1 | `GET /content/{id}/child/attachment/{attId}/download` | `readonly:content.attachment:confluence` |
| `ListAttachments` | v1 | `GET /content/{id}/child/attachment` | **undocumented, see below** |
| `ListChildPages` | v1 | `GET /content/{id}/child/page` | **undocumented, see below** |
| `ListChildFolders` | v1 | `GET /content/{id}/child/folder` | **undocumented, see below** |

Union, which is what a token needs:

```
read:page:confluence
write:page:confluence
read:space:confluence
read:folder:confluence
search:confluence
read:confluence-user
write:confluence-file
readonly:content.attachment:confluence
read:confluence-content.summary
```

### The list is deliberately mixed, and that is the whole trap

Classic (`read:confluence-user`) and granular (`read:page:confluence`) are
**separate grants**, and neither implies the other. Note which side of the table
each style lands on: every v2 row is granular, every v1 row is classic or a
v1-era granular name. That is not a coincidence, and it is measurable.

**Verified 2026-08-20**, with a scoped token holding classic
`read:confluence-user` and `read:confluence-space.summary` and no content scope.
The same permission, asked for over each API version:

| granted scope | v1 route | | v2 route | |
|---|---|---|---|---|
| `read:confluence-user` | `GET /wiki/rest/api/user/current` | **200** | `GET /wiki/api/v2/users/me` | **401** |
| `read:confluence-space.summary` | `GET /wiki/rest/api/space/{key}` | **200** | `GET /wiki/api/v2/spaces/{id}` | **401** |

Same token, same resource, same request otherwise. The classic grant is real --
it returns data over v1 -- and is still rejected over v2. So a token granted only
the classic names cannot read a page, because pages are v2.

This is why the older version of this list was wrong in a way that looked right.
It named `read:confluence-content.all`, `write:confluence-content`,
`read:confluence-props` and `write:confluence-props`: all classic, all plausible,
and none of them reaching the v2 routes that do the work. The props pair turns
out to be unnecessary outright, since a v2 page property is covered by
`read:page:confluence`/`write:page:confluence` rather than by a
property-specific scope.

### Three calls Atlassian no longer documents

`GET /content/{id}/child/attachment`, `/child/page` and `/child/folder` are
**absent from the v1 OpenAPI document and from the published REST docs**, while
the `PUT`/`POST` operations on the very same attachment path are present. They
still work -- markfluence uses all three -- but their scope cannot be looked up.

`read:confluence-content.summary` is the requirement, from two independent
sources that agree:

1. **Inferred** from the sibling `GET /content/{id}/descendant/{type}`, which
   *is* documented, is the same kind of read, and asks for exactly that
   (granular equivalent: `read:content-details:confluence`).
2. **Corroborated** by [`pchuri/confluence-cli`][ccli], which calls two of these
   same three endpoints (`/child/page`, `/child/attachment`) and lists
   `read:confluence-content.summary` as required. It is not a guess on their
   part either: they added it after a user hit a 401 without it
   ([#76][ccli76], fixed in #78, "add read:confluence-content.summary to
   required scopes documentation").

**Still not verified here.** Confirming it directly needs a token granted that
scope and nothing else adjacent. If a service account can read pages but
`children`, `export` or `attachment-list` still 401, this row is the first
suspect.

[ccli]: https://github.com/pchuri/confluence-cli
[ccli76]: https://github.com/pchuri/confluence-cli/issues/76

### What confluence-cli's scope table can and cannot settle

It is a useful second source, but only for half the list, and the half it
misses is the half that matters most. It does pages over **v1**
(`/rest/api/content`), so its `read:confluence-content.all` and
`write:confluence-content` are the v1 content scopes. markfluence does pages
over v2 and needs `read:page:confluence`/`write:page:confluence` instead.
Copying its table wholesale reproduces exactly the mistake this section
documents.

Where it does agree, it agrees exactly: `search:confluence`,
`readonly:content.attachment:confluence` and `write:confluence-file` match what
the OpenAPI documents say, derived independently.

> **Do not copy its `read:hierarchical-content:confluence`.** That is a real
> scope, but for `GET /pages/{id}/direct-children` (v2), which is how
> confluence-cli lists folders. markfluence does not call it. Their code
> comment justifies the choice by asserting that folders "are not exposed by
> the v1 `/content/{id}/child/*` endpoints" — which contradicts what we
> measured: `GET /wiki/rest/api/content/{id}/child/folder` returns 200 with
> `type: "folder"` rows ([folders.md](folders.md), verified 2026-08-13). Our
> measurement is first-hand and theirs is a comment, so `read:folder:confluence`
> stays on the list and `read:hierarchical-content:confluence` stays off it.

### Reading a token's actual scopes, without the admin UI

There is no introspection endpoint for a scoped API token, but the scope gate
runs *before* routing and validation, which is enough to test one scope at a
time:

- **401** — the scope gate rejected it. The scope is **absent**.
- **any other 4xx** (400, 403, 404, 415) — the request got past the gate and
  failed later. The scope is **present**.

So a request aimed at a deliberately bogus id is a safe probe: it reads
nothing, writes nothing, and creates nothing, while still reporting whether the
scope is held. `POST /wiki/rest/api/content/999999999999/child/attachment`
answering 403 rather than 401 proves `write:confluence-file` is on the token
without uploading a file.

**Verified 2026-08-20** — this is how the missing scope on the first scoped
service-account token was found, and it distinguished six held scopes from one
absent one in a single pass.

### Beta alternatives

The v1 rows also carry `Beta`-state granular scopes:
`read:content-details:confluence` (user lookup, search, attachment writes),
`write:attachment:confluence` (attachment writes) and
`read:attachment:confluence` (download). They are not used above, since
`Current` is what Atlassian recommends, but they are what a granular-only token
would need if Atlassian ever retires the v1 classic names.

No delete scope appears because (currently) markfluence never deletes anything.
