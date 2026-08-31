# Spaces

What sits at the top of a space, and how to enumerate it. This is the evidence
behind `children --space` (#98).

Everything below was established against `mozilla-hub.atlassian.net`, basic auth
on the site domain, using two spaces:

- a **personal** space (`~60c36d0718e9f60071326951`, id `76646426`, homepage
  `76646878`) holding 34 pages and 3 folders, all of them under the homepage;
- **`AIM`** (id `2097152`, homepage `2097154`), a global space picked because
  it turned out to have *two* pages at its root.

## Verified 2026-08-31

### A space's root pages come from a v1 route, and its rows are already `ChildNode`

`GET /wiki/rest/api/space/{key}/content/page?depth=root` — 200, and a bare row
(no `expand`) carries every field child listing already relies on:

```json
{ "id": "2097154", "type": "page", "status": "current",
  "title": "Africa Innovation Mradi Home",
  "extensions": { "position": 117908152 },
  "_links": { "webui": "/spaces/AIM/overview", "tinyui": "/x/AgAg", ... } }
```

So `listV1[ChildNode]` reads it unchanged, and a root row renders with a URL and
a space key with no follow-up request. Note the homepage's `webui` is
`/spaces/{key}/overview` rather than `/spaces/{key}/pages/...` —
`SpaceKeyFromWebUI` already handles both, as [search.md](search.md) records.

`.../content` without the `/page` suffix answers with both a `page` and a
`blogpost` collection. `children` wants the page tree, so the type belongs in
the path.

### A space can have more than one root page

| space | `depth=root` pages |
|---|---|
| personal | 1 — `Things` (the homepage) |
| `AGILE` | 1 — `Becoming Agile` (the homepage) |
| `AT` | 1 — `Away Team Home` (the homepage) |
| `AMZ` | 1 — `Amazon` (the homepage) |
| **`AIM`** | **2 — `Africa Innovation Mradi Home` (the homepage) and `What is Africa Mradi?`** |

This is the finding that decides how a space is walked. "The space root is the
homepage, so start from `homepageId`" is true of four spaces out of five and
**wrong** on the fifth, where it would drop a root page and everything under it
— the same class of wrong answer as v2 `/children` omitting folders
([folders.md](folders.md)), and just as silent.

`markfluence create` with `parent: null` produces exactly this shape, so a
multi-root space is not an exotic case a user has to go out of their way to
build.

### Archived root pages are not listed

The personal space has an archived page with no parent
(`Some Archived Page`, `2973663237` — it appears in the v2 flat listing below
with `parentId: null`). `depth=root` reports only `Things`. So the route
already behaves like a v1 child listing: current content, no status filter
needed.

### An unknown key answers 404, and that is not the way to detect one

```
GET /wiki/rest/api/space/NOSUCHSPACEXYZ/content/page?depth=root
404 {"statusCode":404,"message":"org.springframework.web.server.ResponseStatusException:
     404 NOT_FOUND \"No space found with key : NOSUCHSPACEXYZ\""}
```

The message names the key, but reading it is the wrong move: **a rejected
credential is also a 404** ([api.md](api.md)), the text is a Spring exception
string rather than a documented body, and misreading an auth failure as "no such
space" is the exact defect `RejectedCredential` exists to prevent. Resolve the
key with `GET /wiki/api/v2/spaces?keys={key}` first, the way `find` and `search`
already do.

### A folder cannot sit at a space root

There is no route that would list one:

| request | result |
|---|---|
| `GET /wiki/rest/api/space/{key}/content/folder?depth=root` | **500** `java.lang.NullPointerException: PageRequest should not be null` |
| `GET /wiki/api/v2/spaces/{id}/folders` | 404, and an HTML error page — not an API route |
| `GET /wiki/api/v2/folders?space-id={id}` | **500** `INTERNAL_SERVER_ERROR` |
| `GET /wiki/api/v2/spaces/{id}/direct-children` | **400** `Provided value {spaces} for 'hierarchical-content-type' is not the correct type` |

That last one is informative rather than merely a failure: the route is
`/wiki/api/v2/{hierarchical-content-type}/{id}/direct-children`, and there is no
`spaces` member of that type. Do not go looking for one again.

None of it matters, because a folder appears to be unable to sit at a root in
the first place:

```
POST /wiki/api/v2/folders   {"spaceId":"76646426","title":"mf-98 root folder probe"}
200 {"id":"3026485251","type":"folder","parentId":"76646878","parentType":"page", ...}
```

**With no `parentId`, the folder was created under the space homepage.** Asking
for a root-level folder does not produce one; it produces a child of the
homepage. (The probe folder was trashed immediately;
`DELETE /wiki/api/v2/folders/{id}` → 204, and the homepage's folder children
went back to 3.)

So enumerating a space root means enumerating its root **pages**. Folders are
found the moment the walk descends into them, which is what
`pagetree.siblings` already does.

### The v2 flat listing exists, and is not usable here

`GET /wiki/api/v2/spaces/{id}/pages` returns every page in the space as a flat
cursor-paginated collection, each row carrying `parentId` and `parentType` — the
whole 34-page personal space in one request, where a walk costs a request pair
per node. Tempting, and rejected twice over:

- **It lists no folders.** A folder id appears as some page's `parentId`, with no
  title and no position, so a subtree hanging off a folder cannot be placed and
  the folder itself cannot be shown. Reconstructing the tree from it would
  reproduce exactly the "wrong answer, not a partial one" that v2
  `/pages/{id}/children` produces.
- **It includes archived pages by default**, as the archived root page above
  demonstrates.

## Unverified

- **Data Center.** All of the above is Cloud. The v1 space content route exists
  there too, but nothing here was tested against a DC instance.
- **Whether the UI can place a folder at a space root** even though the API's
  own create route will not. Only the API path was exercised.
- **A space with no homepage at all** (possible for a space created by an
  import, allegedly). Not observed; `depth=root` would presumably just report
  whatever pages are there, which is what the walk already handles.
