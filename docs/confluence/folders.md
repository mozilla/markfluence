# Folders

A **folder** is a Confluence Cloud content type that can hold pages, including
being a page's parent. It is not a page, and most of the v2 page routes refuse a
folder id outright. Data Center has no folder type at all.

Everything below was established against one setup, through the gateway with a
scoped token:

- a page (call it the **container page**) with 15 direct children — 12 pages and
  3 folders;
- one of those folders holding 2 pages, one of which markfluence had previously
  published to.

## Verified 2026-08-13

### A folder is not a page, and the v2 page routes say so

| request | result |
|---|---|
| `GET /wiki/api/v2/pages/{folderId}` | 404 `Cannot find a page with id [...]` |
| `GET /wiki/api/v2/pages/{folderId}/children` | 404, same message |
| `GET /wiki/api/v2/pages/{folderId}/direct-children` | 404 `Cannot find [page] entity with id [...]` |
| `GET /wiki/api/v2/folders/{folderId}/children` | 404 — and the body is an HTML error page, not JSON, so this is not an API route at all |

This is the shape of the bug behind a "parent page not found" failure: the id is
real, it just is not a page.

### `GET /wiki/api/v2/folders/{id}` exists, and carries what we need

200, with `id`, `type: "folder"`, `title`, `status`, `spaceId`, `parentId`,
`parentType`, `position`, `authorId`, `ownerId`, `createdAt`, `version`, and
`_links` (`base`, `editui`, `edituiv2`, `tinyui`, `webui`).

Two consequences worth stating, because both remove a design worry:

- a folder reports a **`spaceId`**, so a "is this parent in the target space?"
  check works on a folder exactly as it does on a page;
- a folder reports **`webui`**, so URL construction and space-key derivation need
  no special case.

### A page reports what *kind* of thing its parent is

`GET /wiki/api/v2/pages/{id}` on a page sitting inside a folder returns
`parentId` = the folder's id and **`parentType: "folder"`**. For a page under a
page it is `"page"`.

That is the field that identifies a folder parent without a second request.

### Creating a page under a folder works, unchanged

`POST /wiki/api/v2/pages` with `parentId` = a folder id and `spaceId` = the
folder's space returned 200, and reading the new page back reported
`parentType: "folder"`. No special payload, no extra field.

So a folder parent needs **no API accommodation**. Any failure to publish into a
folder is our own pre-flight refusing to send a request that would have worked.

### v2 `children` hides folders; `direct-children` does not

Against the container page:

| route | results | fields |
|---|---|---|
| `/wiki/api/v2/pages/{id}/children` | **12** — pages only | `id`, `status`, `title`, `spaceId`, `childPosition` |
| `/wiki/api/v2/pages/{id}/direct-children` | **15** — pages *and* folders | `id`, `status`, `title`, `type`, `childPosition` |

Neither carries `webui`, and `direct-children` does not carry `spaceId`.

The 3-result gap is the whole story: **`/children` silently omits folders**, and
with them every page underneath one. A descendant walk built on it does not
return a partial answer, it returns a wrong one, with nothing to indicate that
anything was skipped.

`direct-children` paginates with a genuine cursor. Uncapped, `_links` holds only
`base`; with `?limit=2`, `_links.next` appears carrying a `?cursor=…`.

### v1 enumerates both kinds, and is the only way *into* a folder

| request | result |
|---|---|
| `GET /wiki/rest/api/content/{folderId}/child/page` | **200** — works where every v2 route 404s |
| `GET /wiki/rest/api/content/{id}/child/folder` | 200, rows with `type: "folder"` |
| `GET /wiki/rest/api/content/{id}/child` | no rows; lists the available child types in `_expandable` — `page`, `folder`, `attachment`, `comment`, `database`, `slide`, plus third-party ones |

With `?expand=space,version`, v1 child rows carry `space`, `version`, and
`_links.webui` — so a row can be rendered with a URL and a space key without a
follow-up request per child.

v1 paginates by the `start`/`limit` offset scheme described in
[api.md](api.md): `_links.next` appears only when the results are capped, so its
absence cannot terminate a loop.

## Unverified

- **Whether folders nest.** `/child/folder` inside one folder returned zero rows.
  That is one folder, not a rule; treat nested folders as possible.
- **Data Center.** Asserted to have no folder content type. Not tested — no DC
  instance was available.

## What this means for markfluence

- **A folder id is a legitimate parent.** Validating a parent means trying the
  page route and falling back to `/folders/{id}`; a page's own `parentType` says
  which kind it has. Rejecting a folder id before the POST is the entire defect.
- **Child enumeration is a third v1 exception**, alongside attachment writes and
  the user lookup. Not a preference: v2 cannot enumerate inside a folder at all,
  and its page-children route hides folders. Walking with
  `/child/page` + `/child/folder` also gets `webui` on every row.
- **Publishing needs nothing.** A page whose parent is a folder updates normally;
  only pre-flight parent validation was ever involved.
