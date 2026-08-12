# The REST API

## Two API versions, and why both

Pages and content properties use **v2** (`/wiki/api/v2/...`). Attachment writes
and the user lookup use **v1** (`/wiki/rest/api/...`) because v2 does not cover
them.

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
