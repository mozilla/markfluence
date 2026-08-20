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

Which is worth knowing, because 401 is also what a bad password, a revoked
token, and a scoped token pointed at the site domain all return. The three are
distinguishable only by the body: this JSON, a JSON `Unauthorized` without the
scope clause, and Tomcat HTML respectively. A 403 would mean the token is
scoped for the call but the account lacks Confluence permission on that
content — a different fix entirely (grant the service account space access,
not a new token).

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

A scoped token needs:

* `read:confluence-content.all`
* `write:confluence-content`
* `read:confluence-space.summary`
* `read:confluence-props`
* `write:confluence-props`
* `write:confluence-file`
* `read:confluence-user`

**Transcribed.** No delete scope appears because (currently) markfluence never
deletes anything.

> [!WARNING]
> **This list is incomplete, and the omission is not a typo.** These are the
> *classic* scope names, which reach the **v1** routes only. markfluence reads
> and writes pages through **v2**, and a classic grant does not authorize a v2
> route. A token holding exactly this list still cannot run `read`.

**Verified 2026-08-20**, with a scoped token holding classic
`read:confluence-user` and `read:confluence-space.summary` but no content
scope. The same permission, asked for over each API version:

| granted scope | v1 route | | v2 route | |
|---|---|---|---|---|
| `read:confluence-user` | `GET /wiki/rest/api/user/current` | **200** | `GET /wiki/api/v2/users/me` | **401** |
| `read:confluence-space.summary` | `GET /wiki/rest/api/space/{key}` | **200** | `GET /wiki/api/v2/spaces/{id}` | **401** |

Same token, same resource, same request otherwise. The classic grant is real —
it returns data over v1 — and it is still rejected over v2. So classic and
granular are granted separately and neither implies the other.

Which splits what markfluence needs along the version line it already uses:

| markfluence call | version | scope flavor needed |
|---|---|---|
| pages read/create/update | v2 | granular |
| page content properties (page width) | v2 | granular |
| spaces | v2 | granular |
| folder lookup | v2 | granular |
| attachment list/upload/download | v1 | classic |
| CQL search (`find`, `search`) | v1 | classic |
| current user (`info`) | v1 | classic |

The granular names are **not recorded here on purpose**: naming them from
Atlassian's docs without a token to test would be transcription presented as
fact, which is the mistake this file exists to avoid. Determining them needs a
token that actually holds them. Until then, a working service-account token
needs the classic list above *plus* the granular equivalents for the v2 half.
