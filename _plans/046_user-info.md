# 046 — `markfluence user-info [ID]`

Closes #171.

markfluence can tell you nothing about the credentials it is using. When a
publish goes somewhere unexpected, or a token is refused, or a page's history
names an account id you do not recognise, the tool that made the request has no
answer.

This plan covers creating a `user-info` command that can tell you about the
credentials you're using as well as other users.

```
$ markfluence user-info
account id:      712020:a5694ddc-4f77-4146-8dab-07e3349147ed
name:            SRE API Token
email:           sre-api-token-nh3g9hhet0@serviceaccount.atlassian.com
type:            app
status:          active
external:        no
guest:           no
personal space:  (none)

$ markfluence user-info 60c36d0718e9f60071326951
account id:      60c36d0718e9f60071326951
name:            William Kahn-Greene
email:           wkahngreene@mozilla.com
type:            atlassian
status:          active
personal space:  ~60c36d0718e9f60071326951  https://mozilla-hub.atlassian.net/wiki/spaces/~60c36d0718e9f60071326951

$ markfluence user-info --spaces
...
visible spaces:  525
write access:    97 (--json lists them)
admin access:    2 -- airmoArchive, CD
```

Read-only. No markdown file, nothing written.

## Why this earns a command

#143 built `user-find`, which resolves a *name* to an account id. This is the
other three questions, and they are not the same question:

**"Who is this token?"** — the no-argument form, and the reason the command is
worth having. It is the first thing anyone debugging credentials wants, and
`space-info` (#170) answers only the second half of it: that tells you what you
can do *in one space*, this tells you who you are. Four fields earn their place
from things that actually bit while testing #168: `accountType` separates a
service account from a human, which is most of why permissions surprise people;
`isExternalCollaborator`/`isGuest` name the restricted-access shape directly;
and `accountId` is the only way to match yourself against a page's
`version.authorId` — "did *I* publish that?".

**"Who is this id?"** — and this one `user-find` structurally cannot answer.
`/search/user` cannot see a **deactivated** account under any
`sitePermissionTypeFilter` value ([users.md](../docs/confluence/users.md)),
while `/user?accountId=` returns one. So "who was this person on a three-year-old
page?" has exactly one route and no command exposes it.

**"Where can this account write?"** — answerable, and cheaply (below).

A fourth reason that is not about features: both user routes need only
`read:confluence-user`, which every working token already carries. `user-find`
needs the granular `read:content-details:confluence`, which #168 measured as
implied by nothing. So on a token that cannot run `user-find` at all — the one
in this repo's `.env` — `user-info` works.

## What the probe established — verified 2026-09-18

### The two identity routes, and what they carry

`GET /wiki/rest/api/user/current` and `GET /wiki/rest/api/user?accountId=`,
both `read:confluence-user`, both measured returning: `accountId`,
`displayName`, `publicName`, `email`, `accountType` (`atlassian` for a person,
`app` for a service account), `accountStatus`, `isExternalCollaborator`,
`isGuest`, `type`.

### The personal space comes from `?expand=personalSpace`, and cannot be constructed

`?expand=personalSpace` returns the space's `id`, `key`, `name`, `type` and
`status` — everything a URL needs.

**Do not build the key from the account id.** `~{accountId}` is right for some
accounts (`~60c36d0718e9f60071326951`) and wrong for others: this instance has
personal spaces keyed by **email** (`~aalexander@mozilla.com`,
`~amuntner@mozilla.com`) in the same directory. Both forms are live, so the key
is a lookup, not a format.

It is also genuinely **optional**: the `app` account returned
`personalSpace: null` while a person returned a space. So the field is absent
for a real and common case — every service-account token — rather than only in
theory.

### Write and admin access is one walk, because the space *collection* carries operations

`GET /wiki/rest/api/space?expand=operations` returns per-space operations on
every row, so the survey is a paged walk rather than a request per space. The
mapping is unambiguous:

| question | operation |
|---|---|
| can create pages | `create:page` |
| administers the space | `administer:space` |

An admin's row is unmistakable — `administer:space`, `archive:space`,
`delete:space`, `export:space`, `manage_content:space`, `manage_users:space`
and six more — so `administer:space` is the single predicate rather than a
heuristic over the set.

Measured with the scoped service-account token: **97 of 525 visible spaces**
with `create:page`, **2** with `administer:space`.

Neither is page *edit*, which is not a space property at all (#168): `create:page`
is a space grant and page `update` appears only on a page's own operations. The
output must say "write access" meaning *create pages here*, and the `Long` has
to be explicit, or this becomes the command that promised you could edit.

### **`/rest/api/space` returns short pages that are not the end**

The finding that has to be recorded before anyone writes this. `limit=250&start=0`
returned **200** rows — and `start=200` returned **250** more, with
`limit=500&start=0` returning 500. The real total is 525.

`listV1` terminates on exactly that short page, so pointing this route at it
would have reported **200 spaces, 97 → 39 writable**, silently, with no error.
That is not hypothetical: the first version of this probe did precisely that and
produced a confident wrong answer, which is [README.md](../docs/confluence/README.md)'s
opening trap arriving from a new direction.

So this is a **fifth pagination case** for `api.md`: a v1 offset collection that
behaves like `/wiki/rest/api/search`, where a short page means nothing and only
an empty page (or a missing `_links.next`) ends the walk. It does not go through
`listV1`, and `listV1`'s doc comment should say why — the existing note explains
that `_links.next` is absent when results fit one page, which is exactly the
reasoning that makes this route unsafe under it.

The `GET` on `/rest/api/space` is **undocumented** in Atlassian's swagger (only
`POST` is), so its scope is observed rather than derived — the same footing as
the three v1 child-listing routes [api.md](../docs/confluence/api.md#scopes)
already records that way. It works with markfluence's existing union.

## Decisions

**The argument is optional and defaults to self.** `user-info` is "me" as
determined by the account credentials being used, `user-info ID` is "them",
which is `git config`'s convention and reads better than a separate `whoami`.
The cost is that the name suggests a sibling of `page-info`/`space-info` that
describes *a user*, when its primary use describes *you*; the `Long` leads with
the no-argument form for that reason.

**An account id only, never a name.** Resolving a name is `user-find`, and
accepting both would make one command two with a guess in the middle — and the
guess would be wrong in the interesting direction, since a deactivated person
*has* an id that works here and a name that finds nothing there. The `Long`
cross-references it in both directions.

**The space survey is `--spaces`, not default.** The identity half is one
request; the survey is 3+ and answers a different question. `info --properties`
is the precedent, and it is the same trade: the expensive half is opt-in so the
cheap half stays instant.

**Every optional field degrades independently.** Only the identity lookup is
fatal. `personalSpace` absent is a *real answer* (`(none)`), not a failure —
which is the distinction `--json` has to preserve: `null` for "no personal
space" and a missing survey is `null` too, so the two cannot share a field.

**A deactivated account is reported, not hidden.** It is half the reason the
one-argument form exists. `accountStatus` is the field; **unverified** whether a
deactivated account reports something other than `active` there, since the only
deactivated ids to hand are on pages and the probe used live accounts. What *is*
verified ([users.md](../docs/confluence/users.md)) is that the route resolves
one at all and that `displayName` carries `(Deactivated)`. Worth checking before
the human output claims a status it may not get.

## Implementation

### `internal/client`

```go
type User struct {
    AccountID, DisplayName, PublicName, Email string
    AccountType, AccountStatus                string
    IsExternalCollaborator, IsGuest           bool
    PersonalSpace *SpaceRef // nil when the account has none
}

func (c *ConfluenceClient) CurrentUser() (*User, error)
func (c *ConfluenceClient) UserInfo(accountID string) (*User, error)
func (c *ConfluenceClient) WalkSpaceOperations(visit func(SpaceRef, []string) error) error
```

`LookupUser` already exists and returns a display name only; `UserInfo` is the
full shape. They should not be merged: `LookupUser` is called per mention in a
render loop and wants the narrowest possible result, and #168's `UserCache`
stores exactly that.

`WalkSpaceOperations` gets its own pager, terminating on an **empty** page, with
the measurement in a comment beside the loop. It must not call `listV1`.

### `cmd/userinfo/`

`Cmd`, `run`, a `report` feeding both renderers — `cmd/info`'s shape.
Completion offers nothing: an account id is server-side, and
`internal/completion` may not call Confluence.

### `--json` and the schema

New `command` enum entry **plus** an `if/then` branch constraining
`results.items` and `summary`, per `internal/schematest` — the enum entry alone
is how a new command's conformance test goes green while validating nothing.

```json
{ "ok": true, "account_id": "…", "display_name": "…", "public_name": "…",
  "email": "…", "account_type": "atlassian", "account_status": "active",
  "external_collaborator": false, "guest": false,
  "personal_space": { "key": "~…", "id": "…", "name": "…", "url": "…" },
  "spaces": { "visible": 525, "write": ["ENG", "…"], "admin": ["CD"] } }
```

`personal_space` is `null` for an account without one. `spaces` is `null`
without `--spaces`, and its lists are keys rather than counts, since a consumer
asking the question wants to know *which*.

## Files

| file | change |
|---|---|
| `cmd/userinfo/{userinfo,json}.go` | new |
| `cmd/root.go` | register it |
| `internal/client/user.go` | `CurrentUser`, `UserInfo`, `WalkSpaceOperations` |
| `schema/json-output/v1.json` | enum entry, result `$def`, `if/then` branch |
| `docs/confluence/users.md` | the two identity routes' fields, `personalSpace`, the key-format finding |
| `docs/confluence/api.md` | **the fifth pagination case**, the three routes and their scopes |
| `docs/json-output.md`, `README.md`, `CLAUDE.md` | the command |

## Tests

- **A short page is not the end**: the stub serves 200 rows for `start=0` and
  250 for `start=200`, and the walk must collect 450. This is the regression for
  the trap above and the reason this route has its own pager.
- `create:page` maps to write and `administer:space` to admin; a row with
  `read:space` alone is neither, and `export:space` is not admin.
- `personal_space` is `null` for an account that has none, and populated from
  the expansion otherwise -- never built from the account id.
- The no-argument and one-argument forms hit `/user/current` and
  `/user?accountId=` respectively, and neither touches `/search/user`.
- Without `--spaces`, exactly one request is made.
- Schema conformance, built with the command's own builder.

## Not in scope

- **Resolving a name.** That is `user-find`.
- **Per-page editability.** Not a space property (#168); `space-info`'s row has
  the same caveat.
- **Group membership.** `/user/current` does not carry it and the group routes
  are admin-gated. "Which groups am I in" is a plausible next question and a
  different one.
- **Listing other people.** There is no user directory walk here; `user-find`
  searches and this one looks up.
- **Anything that writes.** Including `--dry-run`, which has nothing to preview.
