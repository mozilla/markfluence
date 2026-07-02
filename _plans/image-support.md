# Plan: image support

Support markdown images (`![alt](./assets/screenshot.png)`) in both `update` and
`create`. Local images are uploaded as page attachments and referenced; missing or
unsupported images degrade to an `IMAGE BROKEN:` placeholder; remote URLs render via
`ri:url` without upload.

## API facts

- **Upload is v1 only**: `PUT /wiki/rest/api/content/{pageId}/child/attachment`,
  `multipart/form-data` with a `file` part, header `X-Atlassian-Token: nocheck`. (v2
  attachments are read-only.) This is the one non-v2 call in the client.
- **List attachments (v2)**: `GET /wiki/api/v2/pages/{pageId}/attachments`
  (filterable by filename) — used for the skip-if-present check.
- **Storage reference**:
  `<ac:image ac:alt="..."><ri:attachment ri:filename="NAME" /></ac:image>` for
  attachments; `<ac:image ac:alt="..."><ri:url ri:value="URL" /></ac:image>` for
  remote images.

## Decisions locked (from the interview)

- **Flow:** conversion *collects*, commands *upload*. `md_to_confluence` rewrites
  `<img>` from the filesystem only (no network) and returns the list of local images
  to upload. `update` uploads to the known page_id; `create` uploads right after
  creating the page. Bodies reference attachments by filename, so referencing before
  upload is fine.
- **Local existing + supported type** → upload + `<ac:image><ri:attachment/></ac:image>`.
- **Remote URL** (`http(s)://`) → `<ac:image><ri:url ri:value="..."/></ac:image>`,
  no upload.
- **Supported image types (whitelist):** `png, jpg, jpeg, gif, svg, webp, bmp`.
- **Missing file OR unsupported extension** → replace the image with literal text
  `IMAGE BROKEN: <authored-path> (<reason>)` where reason is `not found` or
  `unsupported type`; also print a warning to stderr. Non-fatal (never aborts
  `create` phase 1 or fails `update`).
- **Attachment name (mark's scheme):** the image's relative path with `/` replaced
  by `_` (leading `./` stripped) — e.g. `assets/screenshot.png` →
  `assets_screenshot.png`. **Path-based and stable** (does not change when the image
  content changes); different directories can't collide.
- **Change detection / re-upload (mark's scheme):** compute a **SHA-256** of the
  file contents and store it in the attachment's **comment** as
  `mzcld:checksum: <hex>`. On each run, list the page's existing attachments and
  match by filename:
  - not present → **create** the attachment;
  - present and checksum comment matches → **skip** (unchanged);
  - present and checksum differs → **update in place** (new version of the same
    attachment id).

  Because the filename is stable and edits update in place, **no orphans are created
  by editing an image.** Orphans only arise from removed/renamed references, which we
  do not clean up (same as mark; out of scope).
- **Alt text:** carried into `ac:alt` (omitted when empty).
- **Path resolution:** relative to the markdown file's directory (like `.md` link /
  parent resolution). Absolute local paths are checked for existence too.
- **Both commands** get image support via the shared pipeline.

## Implementation

### `libmarkdown.py`
- New `SUPPORTED_IMAGE_EXTS = {".png", ".jpg", ".jpeg", ".gif", ".svg", ".webp", ".bmp"}`.
- New `replace_images(html, base_dir)` step returning `(html, attachments)`:
  - Match `<img ...>` tags (marko output of `![alt](src)`), reading `src` and `alt`.
  - Remote (`http(s)://`, `//`) → `ri:url` (with `ac:alt`); no attachment.
  - Local: resolve `src` relative to `base_dir`.
    - not a whitelisted ext → `IMAGE BROKEN: <src> (unsupported type)`.
    - doesn't exist → `IMAGE BROKEN: <src> (not found)`.
    - exists → derive the stable attachment filename (`src` with `/`→`_`, leading
      `./` stripped), emit
      `<ac:image ac:alt="alt"><ri:attachment ri:filename="NAME"/></ac:image>`, and
      record an attachment `{abs_path, filename}`. Dedupe by filename.
  - `IMAGE BROKEN` text is HTML-escaped.
- `md_to_confluence(...)` now returns `(html, attachments)`. Add the image step to
  the pipeline (independent of the link/code steps — it only touches `<img>`).
- `attachments` items also stashed from `<pre>` protection? No — images aren't in
  code blocks; run `replace_images` before `collapse_paragraph_newlines` (order
  doesn't matter for correctness, but keep it near the other tag rewrites).

### `libclient.py`
- `ATTACHMENT_CHECKSUM_PREFIX = "mzcld:checksum: "`.
- `list_attachments(page_id)` → the page's attachments with `id`, `filename`, and
  the checksum comment. Use **v1** (`GET /rest/api/content/{id}/child/attachment?
  expand=metadata.comment`) since v2 doesn't surface the comment cleanly and upload
  is v1 anyway.
- `create_attachment(page_id, filename, comment, file_path, content_type)` → v1
  `PUT /rest/api/content/{id}/child/attachment`, multipart (`file`, `comment`,
  `minorEdit=true`), header `X-Atlassian-Token: nocheck`.
- `update_attachment(page_id, attachment_id, filename, comment, file_path,
  content_type)` → v1 `POST /rest/api/content/{id}/child/attachment/{attId}/data`,
  same multipart/header.
- `sync_attachments(page_id, attachments)` — orchestrates create/update/skip:
  reads existing via `list_attachments`, computes each local file's SHA-256, matches
  by filename, and creates / updates-in-place / skips per the checksum. Content type
  via `mimetypes.guess_type`.

### `update.py`
- `md_to_confluence` now returns `(html, attachments)`. After conversion:
  `client.sync_attachments(page_id, attachments)` then `update_page(...)`.

### `create.py`
- `_create_one`: convert → `create_page(...)` → `sync_attachments(new_id, attachments)`.
  (Body already references hashed filenames; they resolve once uploaded.)

## Tests

The image *rewrite* is pure/offline, so add real unit tests for `replace_images`:
- existing supported file → `ri:attachment` with the expected path-based name
  (`assets/x.png` → `assets_x.png`) + one collected attachment;
- missing → `IMAGE BROKEN: ... (not found)`;
- unsupported ext on an existing file → `IMAGE BROKEN: ... (unsupported type)`;
- remote URL → `ri:url`, no attachment;
- same basename in two directories → two distinct filenames (path-based).

(The checksum-based create/update/skip in `sync_attachments` is network-side and
verified manually, like the other API paths.)

`just check` must pass. Live upload (v1 multipart) is verified manually against
Confluence, like the other network paths.

## Image properties via JSON-in-title (implemented)

Support `alt`, `title`, `width`, `height`, `align` on images while keeping the
markdown readable in a plain viewer. `alt` stays native (clean for screen
readers/broken-image text); the extra properties ride in the markdown **title**
slot as a JSON object (only shown as a hover tooltip in normal viewers).

- Markdown: `![Login screen](./x.png '{"title":"Login","width":"100","align":"center"}')`.
  Single-quoted titles avoid escaping the inner double quotes. The simple
  `![alt](src "plain title")` case still works and maps to `ac:alt` + `ac:title`.
- In `replace_images`, also read the `title` attribute from the `<img>` tag.
  **marko HTML-escapes it** (e.g. `"` → `&quot;`), so `html.unescape` the value
  before `json.loads`.
- Title handling:
  - parses to a JSON object → map keys `title`/`width`/`height`/`align` to
    `ac:title`/`ac:width`/`ac:height`/`ac:align` (alt stays native; JSON cannot
    override it). Unknown keys ignored.
  - not JSON / not an object / malformed → use the whole string as a literal
    `ac:title` (never error).
- Validation: `align` ∈ {left, center, right}; `width`/`height` numeric px.
  Invalid → warn to stderr and skip that attribute (don't fail the run).
- Applies to both `ri:attachment` and `ri:url` images.

## Notes / edge cases

- Attachments are per-page: the same image referenced from two pages uploads to each
  page separately (same hashed name on each). No cross-page dedup — inherent to
  Confluence.
- `md_to_confluence`'s return-signature change touches both existing call sites
  (`update`, `create`).
- No image-size syntax (standard markdown has none); `ac:height`/`ac:width` omitted.
