# Plan: gateway base URL for scoped service-account API tokens

Let markfluence authenticate as an Atlassian **service account** using a **scoped
API token**, so publishing from CI runs as the service account rather than as a
person.

The blocker is not authentication. A scoped token still uses basic auth with
`email:token` — Atlassian's own [401 KB][401kb] shows exactly that. What breaks is
the **URL**: scoped tokens are rejected (401) against the site domain and must go
through the platform API gateway:

```
https://api.atlassian.com/ex/confluence/{cloudId}/wiki/api/v2/pages
```

The path suffix is unchanged, so every existing `baseURL + "/wiki/..."` call site
works verbatim once the base moves. The work is therefore: **split the one base URL
markfluence has today into a request base and a site base**, because the site URL is
still needed for content markfluence *writes* (link rewriting) and for URLs it
prints.

Confirmation this is the right shape: [`pchuri/confluence-cli`][ccli] supports
scoped tokens with no dedicated credential type at all — you just point its
`--domain`/`--api-path` at the gateway and keep basic auth.

[401kb]: https://support.atlassian.com/atlassian-cloud/kb/401-unauthorized-error-when-service-account-accesses-jira-or-confluence-api/
[ccli]: https://github.com/pchuri/confluence-cli

## Out of scope (deliberately)

- **OAuth 2.0.** Atlassian's service-account OAuth is `client_credentials` (2LO), so
  it *is* CI-usable — but the `client_secret` that mints the 60-minute token is
  itself long-lived, so it shortens no secret we store in GitHub. It adds a token
  endpoint and expiry handling and requires this same gateway split anyway. No
  benefit here.
- **Bearer auth.** The bearer value *is* the scoped API token — same secret, different
  header. It grants no capability basic auth lacks. Its only real payoff is not
  needing the service account's email address. Worth doing later as a ~5-line
  `switch` in `send`, but it is not part of what unblocks the service account, so it
  lands separately once this is proven against the real token.
- **Cloud-ID auto-discovery.** `/_edge/tenant_info` returns the cloud ID but is not a
  supported Atlassian API. Keep the cloud ID explicit config.

## Decisions locked

### `--url` stays the site URL; the gateway is derived from a new cloud ID

The gateway URL cannot simply *replace* `CONFLUENCE_URL`, because the site URL is
still needed for two things — so it stays, and the only new fact is the cloud ID:

| Setting | Flag | Env / `.env` |
|---|---|---|
| site URL | `--url` | `CONFLUENCE_URL` |
| username | `--username` | `CONFLUENCE_USERNAME` |
| API token | *(none — never a flag)* | `CONFLUENCE_TOKEN` |
| **cloud ID** | **`--cloud-id`** | **`CONFLUENCE_CLOUD_ID`** |

- With a cloud ID set → requests go to `https://api.atlassian.com/ex/confluence/{cloudId}`.
- With it empty → **behavior is byte-identical to today**. This is the back-compat
  guarantee: existing personal-token users and every current test are unaffected.
- **The cloud ID is not a secret.** `https://<site>.atlassian.net/_edge/tenant_info`
  returns it unauthenticated (the supported route is
  `GET https://api.atlassian.com/oauth/token/accessible-resources`, whose `id` is the
  cloud ID). So in CI it belongs in a repo **variable**, not a secret — and it's why it
  can be a `--cloud-id` flag while the token deliberately cannot.
- Only **Cloud** sites have a cloud ID, and it identifies the *site*, not the product —
  one ID serves Confluence and Jira on the same site. Data Center/Server has neither a
  cloud ID nor the gateway, which is a second reason the setting must stay optional.

Rejected: confluence-cli's flat `--api-path`. It works for them because they are
v1-only; markfluence mixes v1 (`/wiki/rest/api/content/.../child/attachment`,
`/wiki/rest/api/user`) and v2 (`/wiki/api/v2/pages`, `.../properties`), and one path
string cannot express both.

### The client carries two bases

`ConfluenceClient` (`internal/client/client.go:53`) gains `siteURL` alongside
`baseURL`, where **`baseURL` becomes strictly the request base**:

- `baseURL` = gateway when a cloud ID is set, else the site URL.
- `siteURL` = always the site URL.
- New accessor `SiteURL()` beside the existing `BaseURL()`.

`New` moves from positional args to a `Config` struct (`SiteURL`, `CloudID`,
`Username`, `Token`). Five string positional params — two of them URLs, one a secret
— transpose too easily to leave unnamed, and the struct extends cleanly when bearer
lands. Contained change: `New` has exactly **one** production caller (`Resolve`) and
two test helpers (`internal/client/client_test.go:47,140`).

`Resolve` likewise moves to a named-field `Options` struct (`URL`, `Username`,
`CloudID`, `EnvFile`) rather than growing to a fourth positional string. Its five
call sites each pull persistent flags by name from cobra (`cmd/update/update.go:67`,
`cmd/create/create.go:94`, `cmd/fix/fix.go:43`, `cmd/info/info.go:44`,
`cmd/read/read.go:62`) and gain a `--cloud-id` pull.

`send` (`internal/client/client.go:207`) is **untouched** — basic auth is correct for
scoped tokens.

### Which call sites switch to `SiteURL()`

**Content-facing — the correctness-critical one.** `MdToConfluence` receives
`c.SiteURL()` at `cmd/update/update.go:159` and `cmd/create/create.go:386`. Nothing
inside `internal/convert` changes; its `baseURL` param just receives the right value.
Getting this wrong writes `api.atlassian.com` links into **published pages** — a
silent, persistent defect that outlives the run, unlike a wrong printed URL.

**Human-facing.** These already prefer the API's `page.Links.Base` and only fall back
to `BaseURL()`, so they largely self-heal; the fallbacks still move to `SiteURL()`:
`cmd/update/update.go:284,288`, `cmd/create/create.go:485,489`,
`cmd/info/info.go:144,146`, `cmd/fix/fix.go:177`.

### `_links.next` must not double the gateway prefix

`ListContentProperties` builds the next page as `c.baseURL + out.Links.Next`
(`internal/client/client.go:659`) — the only pagination site in the client. `next` is
a site-relative absolute path (`/wiki/api/v2/...`), so concatenation is what
*preserves* the `/ex/confluence/{cloudId}` prefix and is correct today.

The risk is only if the gateway echoes `next` already carrying that prefix, which
would double it. Rather than guess the live behavior, add a small
`resolveNext(base, next)` helper handling three cases:

1. `next` is absolute (has a scheme) → use as-is.
2. `next` already starts with `base`'s path prefix → prepend scheme+host only.
3. otherwise → concatenate onto `base` (today's behavior).

Note `url.ResolveReference` is **wrong** here: an absolute-path reference replaces the
whole path, silently dropping `/ex/confluence/{cloudId}`.

## Verified against the live API

Smoke-tested with `info` through the gateway using an existing *personal* token
(gateway routing turns out to accept one, which made this testable before the service
account exists):

- **`_links.base` returns the site URL under the gateway** — confirmed
  `https://mozilla-hub.atlassian.net/wiki` from a v2 page fetch via
  `api.atlassian.com`. So **no inversion is needed**: the `Links.Base`-preferring
  logic in `cmd/info/info.go`, `cmd/update/update.go`, and `cmd/create/create.go`
  stays as-is, and printed URLs are correct in both modes.
- **v1 endpoints work through the gateway** — `info` resolved author display names,
  which is the v1 `/wiki/rest/api/user` call.

Still open, and **only answerable with the scoped token**:

- **The v1 attachment upload.** Atlassian documents classic scopes for v1 vs granular
  for v2 while warning against mixing sets, so a scope gap would surface there, and it
  is load-bearing for any page with images. A personal token is unscoped, so testing
  this now would prove nothing about scopes — it has to wait for the real credential.
  First real run should be an `update --dry-run` on a page with an image, then a live
  one.

### Scopes to request for the service account

markfluence never deletes, so no delete scopes. Request the **classic** set (Atlassian's
recommendation):

| Calls | Classic | Granular |
|---|---|---|
| GET/POST/PUT `/api/v2/pages` | `read:confluence-content.all`, `write:confluence-content` | `read:page:confluence`, `write:page:confluence` |
| GET `/api/v2/spaces` | `read:confluence-space.summary` | `read:space:confluence` |
| `/api/v2/pages/{id}/properties` (page width) | `read:confluence-props`, `write:confluence-props` | `read:content.property:confluence`, `write:content.property:confluence` |
| `/rest/api/content/{id}/child/attachment` (v1, images) | `write:confluence-file` | `read:attachment:confluence`, `write:attachment:confluence` |
| GET `/rest/api/user` (v1) | `read:confluence-user` | `read:user:confluence` |

## Schema (`schema/json-output/v1.json`) — no change

`url` fields already exist (lines 151, 198, 235); only their *values* could change, not
the document shape. `TestSchemaConformance` should stay green — confirm rather than
assume.

## Testing

- `internal/client/config_test.go`: cloud-ID resolution through the existing
  flag > env > `.env` precedence; a cloud ID containing a slash or a full URL is
  rejected with a useful message (guards against a pasted
  `https://api.atlassian.com/ex/confluence/...`, which would otherwise 404 opaquely).
- `internal/client/client_test.go`: `New` with a cloud ID targets the gateway path
  while `SiteURL()` stays the site; with no cloud ID both equal the site URL
  (back-compat).
- `resolveNext`: unit-test all three branches, including the prefix-already-present
  case that would otherwise double.
- **A `cmd`-level test that the converter receives the site URL in gateway mode.**
  This is the one regression that silently publishes broken links, so it gets a
  direct test rather than relying on client-level coverage.

## Docs

- `README.md`: config table (31–33), `.env` block (41–43), env-var list (~300), and a
  service-account example in the GitHub Actions section (~338) — including that the
  gateway needs `CONFLUENCE_CLOUD_ID` while `CONFLUENCE_URL` stays the site URL,
  since that pairing is the non-obvious part. In that example `CONFLUENCE_CLOUD_ID`
  must be a `vars.` entry, **not** grouped with the `secrets.` ones: it isn't
  sensitive, and showing it as a secret teaches the wrong thing to anyone copying the
  snippet. Document how to look it up (`_edge/tenant_info`, or
  `accessible-resources` for the supported route).
- `.env.example`.
- `CLAUDE.md`: Configuration section, keeping the "token is deliberately never a
  flag" note (still true), and record the scope table above so it is not re-derived.

## Commits

1. `feat(client): route requests through the API gateway when a cloud ID is set` —
   config, two bases, `SiteURL()` at all call sites, `resolveNext`, tests.
2. `docs: document scoped service-account tokens` — README, `.env.example`, CLAUDE.md.
