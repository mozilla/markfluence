# Page status

The coloured lozenge Confluence shows next to a page title — `Rough draft`,
`In progress`, `Ready for review`, `Verified` out of the box, plus whatever a
space has been configured with. Atlassian's own name for it in the API is a
**content state**, which is the first thing to keep straight, because the word
"status" is already taken twice over:

| the word | what it means | where it lives |
|---|---|---|
| **content state** | the lozenge next to the title | `/content/{id}/state`, v1 only |
| **content status** | `current` / `archived` / `trashed` / `deleted` | `Page.status` in v2, and the `?status=` query parameter |

The `?status=` parameter on the state routes is the **second** of those, not the
first: `PUT /content/{id}/state?status=current` means "set the lozenge on the
current version of this page", not "set the lozenge to current". Reading it the
other way is the obvious mistake and the API will not stop you making it.

## Routes

Nothing about this is in v2. Every route is v1, `/wiki/rest/api/...`, and the
scopes below come from Atlassian's own swagger document the way
[api.md](api.md#scopes) derives the rest.

| method + path | scope | notes |
|---|---|---|
| `GET /content/{id}/state` | `read:confluence-content.summary` | the page's current state, if any |
| `PUT /content/{id}/state?status=current` | `write:confluence-content` | set it |
| `DELETE /content/{id}/state?status=current` | `write:confluence-content` | clear it |
| `GET /content/{id}/state/available` | **`write:confluence-content`** | the vocabulary — see below |
| `GET /space/{key}/state` | `read:confluence-space.summary` | **do not use**, see below |
| `GET /space/{key}/state/settings` | `read:confluence-space.summary` | space admin only in practice |
| `GET /space/{key}/state/content` | `read:confluence-content.all` | pages carrying a given state; unprobed |

Both scopes markfluence needs here are **already in its union** —
`read:confluence-content.summary` and `write:confluence-content`, the latter
bought for the two label writes (#138). So supporting page status adds no scope
to what a token must carry, and the stale half of api.md's note that
`write:confluence-content` "buys nothing but the two label writes" is worth
fixing when it does.

Note the oddity in that table: **`state/available` is a read route behind a
write scope.** A read-only command that lists the vocabulary therefore requires
a token that can write content. Since markfluence's union already has it, this
costs nothing today, but a future read-only-token story cannot include it.

## Verified 2026-09-18

Against `mozilla-hub.atlassian.net` through the API gateway with a personal
(unscoped) token, using the `markfluence roots smoke test` fixture
(`3022914511`) in willkg's personal space and, for the cross-space checks, pages
in `AGILE` — a space readable but not administered by the account used.

### The `id` is authoritative and is the only field that needs to be sent

`PUT` with a body of `{"id": 3086974986}` alone returns 200 and sets the state.
When an `id` is present the body's `name` and `color` are **ignored**: sending
the right id with a deliberately lowercased name echoed back the canonical
`{"id":3086974986,"color":"#57d9a3","name":"Ready for review"}` and changed
nothing else.

An unknown id is a clean 404 that names the problem, with no side effect:

```
404 Content state with 999999999 id does not exist, or is disabled
```

### Sending a name instead of an id can create a state, permanently

The `id` is optional, and omitting it puts the request on a *create* path that
is indistinguishable from the set path in shape and status code:

| body (no `id`) | result |
|---|---|
| `{"name":"Ready for review","color":"#57d9a3"}` | 200, matched the existing space state `3086974986` |
| `{"name":"ready for review","color":"#57d9a3"}` | 200, **created** a custom state `ready for review` (`3087138829`) |
| `{"name":"Totally made up","color":"#ff0000"}` | 200, **created** a custom state (`3087237122`) |

So **the name match is case-sensitive**, and a case mismatch is not an error —
it is a new state. There is no route that deletes one:
`DELETE /rest/api/content-states/{id}` is a 404 and the collection is a 405, so
the only cleanup is the UI. Two states created by these probes are still on the
account that ran them.

The `color` requirement is part of the same story. Omitting it with no `id` is a
400 — `color in body of content state must be a 6 hex digit color like #04df03!`
— because that is the create path validating a new state. With an `id` present,
`color` is not required at all. A 400 demanding a colour is therefore a signal
that the request was about to invent a state.

**The rule this implies: always write by `id`, never by name.** Resolving a name
to an id against `state/available` and refusing an unmatched name makes creating
a custom state structurally impossible rather than something validation has to
remember to prevent.

### Custom states are scoped to the account, not the space

`GET /rest/api/content-states` — no space in the path — returned `[]` before
these probes and exactly the two states they created afterwards. Both then
appeared in the `customContentStates` array of
`GET /content/{id}/state/available` for pages in **`AGILE`**, a space the
account does not own.

So the set of names a page will accept is *space states, which are per space*,
plus *custom states, which follow the caller*. Validating a committed
frontmatter field against the union would make the same file valid for one
person and invalid for another.

### The vocabulary is per (caller, page) — not per space

**Verified 2026-09-18**, with a scoped service-account token given collaborator
access to a personal space, alongside that space owner's own token. This
supersedes the "any page in the space is a probe" reading of the section below:
that held across four pages *for one account*, and does not hold across
accounts or across pages with different permissions.

`GET /content/{id}/state/available`, same space, same account (the service
account), two pages:

| page | `spaceContentStates` |
|---|---|
| one the account had created | Rough draft, In progress, Ready for review, **Verified** |
| one it had not (owned by the space owner) | Rough draft, In progress, Ready for review |

The space owner's own token saw all four on both. So `Verified` is gated on
something about the caller's relationship to the individual page, not on space
configuration — and `GET /space/{key}/state` reported all four to the service
account throughout, which is a second way that route misleads.

**The filter is enforced on write, not merely on display.** `PUT` of the hidden
status's id against a page that did not offer it:

```
403 PermissionException: User is not permitted to use this ContentState on this content.
```

Three consequences, all of which markfluence now depends on:

- The page to ask is **the page the status will be written to**. `update` does
  exactly that. Nothing may cache one page's answer for another, which is why
  there is no vocabulary cache.
- `create` cannot validate a name up front at all: the only authoritative page
  is the one it has not made yet. Probing the parent or the space homepage
  answers a different question — measured refusing `Verified` for a page that
  then accepted it.
- An error message must say "this page can be given …", never "this space
  offers …", or it sends an author to space settings for a difference that is
  not there.

### `state/available` needs **edit** permission on the page it is asked about

Same probes. An account with read access but no edit gets:

```
403 PermissionException: User does not have Page edit permission.
```

which is consistent with the route sitting behind `write:confluence-content`.
Two things follow. It is a free, write-free "can this account edit this page?"
probe, which is how the permission half of the testing above was done without
creating anything. And the space **homepage** is the worst possible probe for a
collaborator account: a space can grant page creation while keeping its
homepage restricted, which is exactly what was measured.

### `space/{key}/state` returns the product defaults, not the space's list

This is the trap. For `AGILE`, the two routes disagree:

```
GET /space/AGILE/state
[{"id":0,"name":"Rough draft",…},{"id":1,"name":"In progress",…},
 {"id":2,"name":"Ready for review",…},{"id":3,"name":"Verified",…}]

GET /content/1703972/state/available
{"spaceContentStates":[{"id":0,…"Rough draft"},{"id":1,…"In progress"},
                       {"id":2,…"Ready for review"}], "customContentStates":[…]}
```

Four against three — no `Verified`. Reproduced on four different pages in that
space, and stable across repeated calls. In willkg's personal space, where the
states have been materialised (real ids rather than `0`-`3`), both routes agree
on four.

`state/available`'s ids are array indices in the unmaterialised case, which is
consistent with it returning the real list while `space/{key}/state` returns the
hardcoded four. Whichever way round the implementation is, `space/{key}/state`
demonstrably reports a state the caller cannot use, so it cannot be used to
validate anything. This is the README's first trap in
miniature: the cheap route answers plausibly and is wrong, and only comparing it
against another answer shows it.

`state/settings` *is* authoritative — it returns the same list plus
`contentStatesAllowed`, `spaceContentStatesAllowed` and
`customContentStatesAllowed` — but it 403s for anyone who is not a space admin:

```
403 User does not have permission to retrieve content state settings.
```

which makes it useless for a tool run by an ordinary author. **Validate against
`content/{pageId}/state/available`,** which needs a page id rather than a space
key.

### A state write bumps the page version

The opposite of labels, where [labels.md](labels.md) records that a write does
not. Each state change is a full page version:

| action | version |
|---|---|
| before | 5 |
| `PUT .../state` | 6 |
| `DELETE .../state` | 7 |

`minorEdit` is `false` and the version message is empty, so it presents in page
history as an ordinary edit. Anything that applies a state on every publish must
therefore compare before writing, or a no-op run adds a version to every page it
touches.

### "No state" is a missing key, not an empty one

Before any state was set, `GET /content/{id}/state` returned `{}`. After a
`DELETE`, it returned `{"lastUpdated":"2026-09-18T11:01:06.372Z"}` — the
timestamp survives, the `contentState` object does not. So absence is tested by
the absence of `contentState`, and a decoded struct must not treat a zero-valued
state as a real one.

### v2 exposes nothing

`GET /wiki/api/v2/pages/{id}` carries no state field in any form — the key set
is `_links`, `authorId`, `body`, `createdAt`, `id`, `lastOwnerId`, `ownerId`,
`parentId`, `parentType`, `position`, `spaceId`, `status`, `title`, `version`.
There is no expansion that adds it. A page's lozenge is one extra v1 request per
page, always.

## What is not verified

- **Whether a *scoped* token can do any of this.** The probes used an unscoped
  personal token. The scopes in the table are Atlassian's documented ones, not
  observed behaviour, and api.md records that the three v1 child-listing routes
  document no scope at all — so the swagger document is not a reliable guide to
  what the gateway enforces.
- **`GET /space/{key}/state/content`**, which by its operation id
  (`getContentsWithState`) lists the pages carrying a state. Not probed; nothing
  needs it yet.
- **Draft pages.** Every probe used `?status=current`. Whether `status=draft`
  behaves the same is untested, and markfluence has no draft story.
- **What happens to a page whose state a space admin later removes** — whether
  the page keeps a dangling lozenge, and whether an id-only `PUT` naming a
  now-disabled state 404s. The 404 text for an unknown id says "does not exist,
  **or is disabled**", which hints that disabled states are rejected, but this
  was not tested.
- **Whether two accounts can create same-named custom states**, and what
  `state/available` then returns. Would need a second account.
