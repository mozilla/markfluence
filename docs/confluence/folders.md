# Folders

A **folder** is a Confluence Cloud content type that can hold pages, including
being a page's parent. It is not a page, and most of the v2 page routes refuse a
folder id outright. Data Center has no folder type at all.

Everything below was established against one setup, through the gateway with a
scoped token:

- a page (call it the **container page**) with 15 direct children — 12 pages and
  3 folders;
- one of those folders holding 2 pages, one of which markfluence had previously
  published to;
- another folder under the same container page holding **a second folder**, which
  in turn holds one page — so a page sitting two folders deep, and an outer folder
  with no child pages of its own.

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

### Folders nest, and a folder's parent can be a folder

`GET /wiki/rest/api/content/{folderId}/child/folder` on the outer folder returned
the inner one, and `GET /wiki/api/v2/folders/{innerId}` reported `parentId` = the
outer folder with **`parentType: "folder"`**. So the parent chain is not
page-then-folder-then-pages; folders can stack to arbitrary depth.

Two consequences that matter more than the nesting itself:

- **`parentType: "folder"` says nothing about depth.** The page two folders down
  reports exactly what a page one folder down reports — `parentType: "folder"` and
  its immediate `parentId`. There is no hint in a page's own metadata that a chain
  of folders sits above it.
- **A pages-only listing of that outer folder returns zero rows.**
  `/child/page` on a folder whose children are all folders is an empty
  collection, not an error. So a non-recursive walk does not report "0 pages, 1
  folder" — it reports nothing at all, and looks indistinguishable from an empty
  folder. Descending `/child/folder` is what makes the difference between a
  correct answer and a confidently empty one.

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

- **A folder at a space root.** Every folder observed had a parent — a page in one
  case, a folder in another. What `parentType` reports for a folder created
  directly at the top of a space was not observed.
- **How deep nesting may go**, and whether Confluence enforces a limit. Two levels
  were verified; nothing suggests two is special.
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
- **A child walk has to recurse through folders, not just list them.** Because
  folders nest and a folder may hold no pages at its own level, stopping at
  `/child/page` can report an empty result for a subtree that contains pages. The
  recursion is over folders even when only pages are being reported.
- **Publishing needs nothing.** A page whose parent is a folder updates normally;
  only pre-flight parent validation was ever involved.
