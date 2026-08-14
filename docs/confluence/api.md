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

**A scoped token requires the gateway. Unverified** — asserted in
`internal/client/config.go` and the README, and it is why the gateway support
exists at all, but confirming it needs a scoped service-account token. The
credential available on 2026-08-07 was an unscoped personal token, which returns
200 against *both* the site domain and the gateway, so it cannot distinguish the
two cases. To verify: issue a scoped token and expect 401 from the site domain.

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

`send` centralizes retry and backoff: 429 for any method, honoring `Retry-After`;
502/503/504 and network errors for idempotent methods only; exponential backoff
with a cap. `SetContentProperty` retries once on top of that, which recovers a
lost response to a create POST.

**Unverified.** This is markfluence's policy rather than an observation about
Confluence, and confirming the server side means provoking rate limits and
gateway errors deliberately. The claim worth testing someday is narrower: that
Confluence returns 429 with a `Retry-After` header rather than a bare 429.

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
