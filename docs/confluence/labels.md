# Labels

A **label** is a short tag on a page. Labels are how Confluence content is
actually organized and searched — the SRE space carries 94 distinct labels
across 1123 pages — and `search --cql 'label = "runbook"'` is only useful for
pages someone labeled.

Reads are v2; **writes are v1**, like attachments — but *not* at the path the
attachment routes would lead you to guess. The one thing to read before touching
any of this:

| | route |
|---|---|
| read | `GET /wiki/api/v2/pages/{id}/labels` |
| add | `POST /wiki/rest/api/content/{id}/label` |
| remove | `DELETE /wiki/rest/api/content/{id}/label?name=…` |

**Not `child/label`** — that collection is read-only for labels and answers a
write with a 405. And **not** a name in the path — that breaks on `ci/cd`. Both
are measured below.

## Verified 2026-09-08

Probed against `mozilla-hub.atlassian.net` with a scratch page in a personal
space: names POSTed one at a time via v1, read back through both v1 and v2,
both delete forms exercised, then the page purged.

### A space or a comma is a separator, not a character

This is the finding everything else here defends against.

| POSTed name | labels on the page afterward |
|---|---|
| `Runbook Two` | `runbook`, `two` |
| `a,b` | `a`, `b` |
| `trail ` | `trail` |

HTTP 200 every time, with no warning of any kind. The name is split first, then
each piece is validated.

Under an assert-exactly rule this does not merely mis-tag a page, it never
converges: `labels: [Runbook Two]` publishes as two labels, reads back as
neither, and so is re-added on **every** run, forever, with no spelling of the
frontmatter that can remove it. Two rows in the SRE space look like this having
already happened to a person — `continuous` + `delivery` and `url` +
`shortener`, each appearing exactly once, on pages where a human plainly typed a
two-word label.

### A colon is refused, so there is no prefix syntax

| POSTed name | result |
|---|---|
| `my:foo` | 400 `label.contains.invalid.chars` |
| `global:x` | 400, same |
| `team:eng` | 400, same |

The 400 body names the reject set:

```
space ! # & ( ) * , . : ; < > ? @ [ ] ^
```

No silent prefix-splitting, and no way for an author to write a prefix at all.
A tab is refused too, and is not in that list.

Note what `.` being invalid costs: a version-shaped label (`v1.2`) is
impossible. That is Confluence's rule, not markfluence's, which is why the
error message quotes the set rather than making an author guess.

### Names are lowercased, and the cap is UTF-16 code units

| input | result |
|---|---|
| `UPPER` | `upper` |
| `HÉLLO` | `héllo` — Unicode-aware, not ASCII-only |
| 255 × `é` (510 bytes) | 200 |
| 256 × `é` | 400 `label.name.is.too.long` |
| 128 emoji (128 code points, 256 UTF-16 units) | 400 `label.name.is.too.long` |
| empty | 400, a parse error rather than a validation one |

So the cap is **255 UTF-16 code units** — not bytes and not runes, and the Go
check is `len(utf16.Encode([]rune(s))) <= 255`. A byte-based check would refuse
a legal 255-character accented label; a rune-based one would accept an emoji
label the server rejects.

Accepted verbatim: `/`, `"`, `'`, `+`, `_`, `-`, digits, non-ASCII
(`héllo-wörld`), emoji. The character inventory in real use across the SRE
space is lowercase alphanumerics, `-`, `_`, and a single `/`.

### The write route is `/content/{id}/label` — **not** `child/label`

This one cost a working implementation: the first version of markfluence's label
support used `child/label`, passed every unit test against a fake, and failed on
the first real page.

**Verified 2026-09-11.** The `child/` collection — the one attachments, pages
and comments hang off — is **read-only for labels**:

| request | result |
|---|---|
| `POST …/content/{id}/child/label` | **405**, `allow: HEAD,GET,OPTIONS` |
| `DELETE …/content/{id}/child/label?name=x` | **405**, same |
| `POST …/content/{id}/label` | 200 |
| `DELETE …/content/{id}/label?name=x` | 204 |

Identical on the gateway (`api.atlassian.com/ex/confluence/{cloudId}`) and on
the site domain, so it is the route and not the base URL. The 405 body is
empty; the `allow` header is the only thing that says what happened.

The attachment routes make the wrong guess an easy one — those really are
`child/attachment`, and labels look like they should match.

### Adding is additive and idempotent; there is no bulk-set route

`POST /wiki/rest/api/content/{id}/label` takes an array and returns the page's
whole label list. Re-POSTing a name the page already carries is a clean 200.
Nothing sets a page's labels to a given set in one call, so "assert exactly this
set" is add-the-missing plus remove-the-extra, computed client-side.

### Removal is one DELETE per label, and must use the `?name=` form

**Verified 2026-09-11** against a live page:

| request | result |
|---|---|
| `DELETE …/label/plain-name` | 204 |
| `DELETE …/label/ci%2Fcd` | **400**, and the body is Tomcat's HTML error page, not JSON |
| `DELETE …/label?name=plain-name` | 204 |
| `DELETE …/label?name=ci/cd` | 204 |
| `DELETE …/label?name=never-existed` | 404, `No label found with name: never-existed` |

The path form works right up until a name contains a `/`, which no amount of
percent-encoding fixes. This is not hypothetical: **`ci/cd` is a real label in
this instance**, verified removable by the query form and not by the path form.
Use the query form only.

A 404 means the label is already gone, which for a removal is the desired state
— but only when it is a genuine 404. A rejected credential answers every v2
route with a 404 too, so that check goes through `notFound` rather than
comparing the status directly (see [api.md](api.md)). That works here because
this 404 *names the label* it could not find, which is what distinguishes it.

### v2 is read-only for labels

| request | result |
|---|---|
| `GET /wiki/api/v2/pages/{id}/labels` | 200 |
| `POST /wiki/api/v2/pages/{id}/labels` | 405 `METHOD_NOT_ALLOWED` |
| `DELETE /wiki/api/v2/pages/{id}/labels` | 405, same |

Hence the split: read v2, write v1.

### Both pagination shapes are ones the client already has

| route | pages by | helper |
|---|---|---|
| v2 `GET /pages/{id}/labels` | the cursor in `_links.next`, a `/wiki`-prefixed absolute path | `listV2`, `resolveNext` unchanged |
| v1 `GET /content/{id}/label` | `start`/`limit` offset | `listV1` |

Neither is the `/wiki/rest/api/search` case, which is neither of these — see
[search.md](search.md).

### Neither GET returns a useful order

v2 sorts by label id; v1 differs again. Neither matches what the UI shows. So
any output has to be sorted locally or it is unstable across runs for no reason
a reader could explain.

### Every label in the SRE space is `global:`

A survey of all 1123 pages found 94 distinct labels and **not one** carrying a
`my:`, `team:`, or `system:` prefix. That is the evidence for markfluence
managing `global:` and nothing else: the other prefixes are read and displayed,
never written and never removed.

`my:` labels are personal (visible only to the account that set them) and
`team:` ones belong to a team space's own vocabulary. Removing either on an
author's behalf, in the course of asserting a frontmatter field that has no
syntax for them, would be deleting data the file could not have expressed.

## End-to-end, against the live instance

**Verified 2026-09-11**, with markfluence itself against a scratch page in a
personal space through the gateway, using an **unscoped personal token**. Every
case below was observed, and the page was purged afterward:

- `create` and `update` apply a declared set, including `ci/cd`.
- `update` asserting a smaller set **removes** the surplus, `ci/cd` included —
  the case the path form cannot do.
- A `my:` label added by hand **survives** every one of those runs.
- `labels: []` removes both managed labels and leaves `my:mine` alone.
- A file with **no** `labels:` key publishes without touching the two labels
  applied by hand, and reports `labels: null`.
- `info` shows `labels:` and `labels/unmanaged:` as separate rows.
- `read` emits `labels: [howto, runbook]` — global-only, sorted, between
  `page_id` and `page_width`.
- `fix` adopts hand-applied labels into a file with no key, and the second run
  reports `already consistent`.
- A block-style list rewritten by `fix` comes back as a block-style list.
- Re-running `update` with an unchanged set makes no label change.

## What is not verified

- **The OAuth scope for the v1 label routes.** An unscoped personal token works
  through the gateway, which says nothing about what scope a scoped token would
  need; [api.md](api.md#scopes) records that v1 scopes cannot be looked up. A
  scoped-token run will settle it.
- **Whether the 255-unit cap is enforced on the v2 read path.** Irrelevant
  unless a label was created by some other client that bypassed it.

## Unrelated bug found while testing this

A **purged** page's v2 404 body is
`{"errors":[{"status":404,"code":"NOT_FOUND","title":"Not Found","detail":null}]}`
— a bare title naming nothing, which is exactly the shape `RejectedCredential`
uses to identify a revoked token. So `info` on a purged page reports "the
credentials were rejected" against credentials that are fine.

`client.go`'s comment claims "every genuine v2 404 *names* what it could not
find". That holds for a page that never existed and for a trashed one; it does
not hold for a purged one. Not a label bug and not fixed here.
