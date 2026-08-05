# Plan: attachment subcommands

Expose standalone attachment management — `attachment-list`, `attachment-upload`,
`attachment-download` — complementing the automatic sync that `update`/`create`
already perform. Closes #9.

018 paused this work on one unsettled question: what remote name a standalone
upload should use. Bijective percent-encoding plus the `path=` comment answers
it, so the naming rule below is a consequence of 018 rather than a new invention.

018 also deferred "clamping `..` when writing files" to #37, on the reasoning that
`read` only prints text. That deferral expires here: `attachment-download`
restores directory layout by default, so it is the first code that writes
attachment bytes to paths derived from server data. **The clamp lands in this
plan.**

## Out of scope (deliberately)

- **`attachment-delete`.** #9 does not ask for it and markfluence holds no delete
  scopes. Removing an orphan is still a Confluence-UI operation; `attachment-list`
  exists so users can *see* orphans, which is what 018 promised.
- **`export` (#37).** This plan builds the two pieces export needs — a download
  client method and a layout-restoring writer — but ships no multi-file, image-src
  reconciling command.
- **Filtering flags on `list`** (`--managed`, `--pattern`). `--pattern` belongs to
  #37, which specifies it; filtering a table is `jq`'s or `grep`'s job in v1.
- **`--comment` on upload.** The comment slot is markfluence bookkeeping. A
  user-supplied comment would either destroy the checksum (breaking skip) or need
  a sub-field, and nothing asks for it yet.

## Decisions locked

### Flat, noun-first command names

`attachment-list`, `attachment-upload`, `attachment-download`.

Cobra alphabetizes `--help`, so a noun-first prefix keeps the three adjacent in
the listing and makes `attachment-<TAB>` a completion group for #14. The JSON
`command` field is the command name verbatim, so it stays derivable from what the
user typed.

**Rejected: an `attachment` parent command with `list`/`upload`/`download`
children.** The issue's own phrasing, and what confluence-cli does. It would make
these the only nested commands in the CLI and force a two-word JSON
discriminator.

**Rejected: verb-first** (`list-attachments`, `upload-attachment`). Reads better
in a sentence, but scatters the three across the help listing under l/u/d.

### One page-argument resolver for the whole CLI

Two resolvers exist today and neither is a superset:

| | numeric id | page URL | `.md` with `page_id` |
|---|---|---|---|
| `info.resolvePageID` | ✅ | ❌ | ✅ |
| `read.parsePageID` | ✅ | ✅ | ❌ |

New `internal/pageref` accepts all three; `info` and `read` are retrofitted onto
it. Both changes are purely additive — each command gains the form it lacked — so
no existing invocation changes meaning. #37 and #44 inherit it.

### `list` shows what a publish will and will not touch

Columns `NAME`, `SIZE`, `VER`, `TYPE`, `SOURCE`, with human-readable sizes.
`NAME` is the **stored** name (`assets%2Fx.png`), because that is what identifies
the attachment to `attachment-download` and what appears in Confluence's own UI.
`SOURCE` is `Meta().Source`; an unmanaged attachment shows `—`.

That em-dash is the point of the command: it distinguishes attachments a publish
will overwrite from hand-uploaded ones it will leave alone, and it surfaces the
orphans 018 chose not to warn about.

**Rejected: decoding `NAME` for display.** Prettier, and makes `SOURCE`
redundant, but it lies about server state and hides the string you need in order
to download the file.

**Rejected: emitting `download_url`.** Built on `SiteURL()` it 401s under a
scoped token; built on `BaseURL()` it leaks the gateway host into reader-facing
output, which the two-bases rule exists to prevent. A URL that works or does not
depending on the reader's token type is a footgun, and `attachment-download` is
the supported way to fetch bytes.

### Upload: basename by default, `--name` takes a path

```
attachment-upload 123 docs/assets/x.png                      → x.png              (path=x.png)
attachment-upload 123 docs/assets/x.png --name assets/x.png  → assets%2Fx.png     (path=assets/x.png)
```

`--name` accepts a **path** and markfluence encodes it, so percent-escapes never
leak into the UI. It is single-file only, like `--title` on `update`/`create`.

**The recorded `path=` is always the decode of the stored name.** If the two were
allowed to drift — name `x.png`, path `docs/assets/x.png` — then a later publish
from `docs/assets/x.png` would encode to `docs%2Fassets%2Fx.png` and create a
*second* attachment, while `download` restored the first to a location the
markdown never references. One truth, enforced by construction: both derive from
the same input.

**Rejected: encoding the given path by default.** Makes a hand-upload agree with
a publish for free, but 018's `normalizeSrc` strips a leading `/`, so
`~/Downloads/report.pdf` becomes `Users%2Fyou%2FDownloads%2Freport.pdf`. Upload
takes arbitrary filesystem paths; image `src`s are page-relative by construction.
The two are not the same input space.

**Rejected: relative-when-under-cwd, basename otherwise.** Does the right thing
in both common cases with no flag, at the cost of a name that depends on where
the user is standing — the instability 018 rejected model A to avoid.

Sync semantics are reused wholesale: created/updated/skipped by checksum, so a
hand upload and a publish agree on state. `--force` uploads regardless (bumping
the version), recovering an attachment whose bytes drifted server-side while its
comment still matches. `--dry-run` is nearly free via the existing
`PlanAttachments`.

### Download: restore layout by default, from `path=` and never from the name

A **managed** attachment is written to its recorded `path=`. An **unmanaged** one
is written under its literal stored name. `--flat` writes everything under
literal stored names.

Restoring from the comment rather than by decoding the name sidesteps the
ambiguity `attachname.go` warns about: there is no way to tell a hand-uploaded
`a%2Fb.png` from one we published, so a decode-by-default would scatter a
literally-named file into `a/b.png`. The comment is truth; the name is inference.

Restore is the default because round-trip is the product thesis. A download
yielding `docs%2Fassets%2Fx.png` leaks Confluence's no-slash restriction into the
user's filesystem and breaks local preview in GitHub or VSCode until every file
is renamed by hand.

**The traversal clamp.** 018 model B legitimately produces `..%2Fassets%2Flogo.png`,
so `..` cannot be refused outright at decode. But a destination path resolving
outside `--dest` is a **hard error for that file**, not a silent clip: the
attachment comment is server-controlled data, and `path=../../../.ssh/authorized_keys`
must not write there. Absolute paths are already refused by
`attachmentSource`. Failing the single file rather than the run keeps one hostile
attachment from blocking a legitimate download.

`--dest` defaults to `.` and is created if missing. An existing file is skipped
with a warning unless `--force`. Zero names means all attachments; a named
attachment absent from the page is a per-item failure.

### `DownloadAttachment` goes through `send`

```go
func (c *ConfluenceClient) DownloadAttachment(att Attachment, w io.Writer) error
```

Reusing `send` inherits retry/backoff, per-attempt timeouts, and typed
`HTTPError` for roughly twenty lines. It buffers the whole attachment in memory —
acceptable for docs and images, revisited in #37 if large exports appear.

A new `timeoutDownload` (120s, matching upload) replaces `timeoutRead`'s 30s,
which is thin for a large file.

**Redirect handling is load-bearing and correct by default.** The endpoint 302s
to `api.media.atlassian.com` with its own short-lived `token=`. `send` sets basic
auth via `req.SetBasicAuth` on a stock `&http.Client{}`, and Go strips
`Authorization` on a cross-host redirect — so Confluence credentials are never
sent to the media host, which does not want them anyway. This wants a comment;
adding a custom `CheckRedirect` that forwarded headers would be a credential
leak.

**Rejected: streaming with its own retry loop.** Constant memory at any size, at
the cost of a second retry/backoff implementation parallel to `send` — the kind
of duplication that drifts.

### `ListAttachments` paginates by offset

The collection carries `size`/`limit`/`start` and **omits `_links.next` when
results fit one page** (018, confirmed again below). `resolveNext` is also written
for v2's `/wiki/api/v2/...` links, while v1's `next` is relative to the `/wiki`
context, so reusing it would drop `/wiki`. Loop on `start += limit` until a short
page. Today's hardcoded `limit=250` with no pagination silently truncates a page
with more attachments.

### The name codec is exported from `internal/convert`

`AttachmentFilename` and `AttachmentSource` become exported. The commands need to
encode a `--name` path and decode for `--flat`. One owner, and `cmd/read` already
imports `convert`.

**Rejected: a new `internal/attachname` package.** The codec is intimately tied
to the converter's image handling, and 018 deliberately put it there.

### JSON contract

`command` is the command name verbatim. Status verbs follow the existing
per-command pattern:

| command | verbs | summary |
|---|---|---|
| `attachment-list` | *(none — data only, like `info`/`read`)* | `basicSummary` |
| `attachment-upload` | `created` / `updated` / `skipped` / `failed` | `attachmentSummary` |
| `attachment-download` | `downloaded` / `skipped` / `failed` | `attachmentSummary` |

`attachmentSummary` is `total`/`succeeded`/`failed`/`skipped`, shared by the two
write commands. `list` emits one result per attachment — target is the
attachment, so `summary.total` is the attachment count and `.results[] |
.filename` works directly. A page-level failure (not found, fetch error) uses the
existing `singleOpFailure` shape.

Download results carry the local `dest_path` actually written. Per-item failures
are `{ok:false, error, code}` with exit 1 if any item failed; fatal/pre-flight
failures keep the stderr error object and exit 2.

**Each command commit carries its own `schema/json-output/v1.json` update**,
because `schematest`'s `additionalProperties:false` makes code and schema fail
together otherwise.

## Verified against the live API

018 recorded incidental findings for #9; they were re-probed against page
`2848423944` before writing this plan, and one assumption was wrong.

- **`extensions.fileSize` (171) and `extensions.mediaType` (`image/png`)**
  confirmed; `metadata.mediaType` duplicates the latter.
- **`version.number`** confirmed, alongside `version.when` and
  `version.by.displayName`. `version.message` duplicates the comment.
- **`_links.download` is `/rest/api/content/{pageId}/child/attachment/{attId}/download`** —
  *not* the classic `/download/attachments/…` UI path. Absolute URL is
  `baseURL + "/wiki" + link`. Because it is an API path, it works through the
  `api.atlassian.com` gateway, which the UI path would not.
- **It 302s to `api.media.atlassian.com/file/{fileId}/binary?token=…`**, a
  different host with a short-lived token — the finding that drove the redirect
  decision above. Following it yields 200 and 171 bytes of valid PNG.
- **The collection returns `size`/`limit`/`start` with no `_links.next`**,
  confirming offset pagination.
- **018's scheme is live in production**: stored title
  `assets%2Fmarkfluence-test.png`, comment
  `markfluence: sha256=e733ac00… path=assets/markfluence-test.png`.

## Testing

- `internal/client/client_test.go`: decoding the expanded `Attachment`
  (`fileSize`/`mediaType`/`version.number`/`_links.download`) from a recorded
  payload shaped like the live one; offset pagination across a short final page;
  `DownloadAttachment` writing bytes, including across a redirect to a second
  test server, asserting the `Authorization` header does **not** reach it.
- `internal/pageref/pageref_test.go`: all three argument forms, plus the
  rejections (empty, non-numeric, a directory, a `.md` with no `page_id`).
- `cmd/attachment-*/`: per-command `json_test.go` validating each new result and
  summary shape against `v1.json` via `schematest`; unit tests for the
  size formatter, the `--name` encode path, and the destination resolver.
- The destination resolver gets adversarial cases: `path=../../escape`,
  `path=/etc/passwd`, a legitimate `..%2Fassets%2Flogo.png` under model B landing
  inside `--dest`, and an unmanaged attachment falling back to its literal name.
- `make test && make lint && make vet`.

## Docs

- `README.md`: a usage section per subcommand alongside `info`/`read`, the
  naming rule for `--name`, the restore-vs-`--flat` behavior, and the `--json`
  notes for the three new `command` values.
- `CLAUDE.md`: the three commands in the layout list (and the stale "four
  subcommands" phrasing in the `cmd/root.go` bullet), `internal/pageref`, and the
  download method plus offset pagination on the `internal/client` bullet.

## Commits

1. `feat(client): expose attachment metadata and downloads` — expanded
   `Attachment`, offset pagination, `DownloadAttachment`, `timeoutDownload`.
2. `refactor(convert): export the attachment name codec`.
3. `refactor(pageref): unify page argument resolution` — new package, `info` and
   `read` retrofitted.
4. `feat(cmd): add attachment-list` (+ schema).
5. `feat(cmd): add attachment-upload` (+ schema).
6. `feat(cmd): add attachment-download` (+ schema).
7. `docs: document the attachment subcommands`.
