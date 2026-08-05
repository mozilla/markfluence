# Plan: attachment names that round-trip

Make the mapping between a markdown image's source path and its Confluence
attachment name **bijective**, and record the source path on the attachment, so
that publishing a page and reading it back recovers the image's original
location instead of a flattened approximation.

Confluence attachment names cannot contain `/`, so a path has to be flattened
into a name. Today that is one line (`internal/convert/images.go:147`):

```go
// attachmentFilename derives a stable, collision-free attachment name from an
// image path: a leading "./" is dropped and "/" becomes "_" so images from
// different directories don't collide.
func attachmentFilename(src string) string {
	name := strings.TrimPrefix(src, "./")
	return strings.ReplaceAll(name, "/", "_")
}
```

There is no inverse: `renderImage` (`internal/convert/storage_to_md.go:377`)
uses `ri:filename` verbatim as the markdown `src`. That produces two defects.

**The "collision-free" claim is false.** `a/b.png` and `a_b.png` both flatten to
`a_b.png`. Because the encoded name is also the dedupe key
(`internal/convert/images.go:59`), the *second* file is silently skipped: one set
of bytes is uploaded and both images on the page resolve to it. `_` is a bad
separator precisely because `_` is common in filenames.

**Directory structure is unrecoverable.** The publish → `read` → publish cycle is
a stable fixed point, but a lossy one:

```
docs/img.png  →  publish  →  docs_img.png  →  read  →  ![](docs_img.png)  →  publish  →  docs_img.png
```

The directory is gone and the read-back `src` names a file that does not exist on
disk. This is what makes #37 (`export`) incoherent — export cannot lay
attachments out so the markdown's image links resolve. #37 calls this
reconciliation its crux.

## Out of scope (deliberately)

- **Clamping `..` when writing files.** A decoded `../assets/logo.png` is
  legitimate (see the base-directory decision below), and `read` only prints
  text, so an accurate path is the right output. Refusing to write outside a
  destination directory belongs to #37, at the point export actually creates
  files — which it must do for hand-uploaded attachments regardless.
- **Warning about orphaned attachments.** Detecting one means plumbing a legacy
  name through `convert.Attachment` → `client.LocalAttachment` →
  `planAttachments` for a one-time cosmetic concern. #9's `attachment-list` will
  let users see every attachment on a page and remove orphans directly.
- **#9 (attachment subcommands).** Paused for this: a standalone
  `attachment-upload` needs settled guidance on what remote name to use.

## Decisions locked

### Percent-encode, because the encoding must escape its own escape character

`%` → `%25`, then `/` → `%2F`. Decode reverses it: `%2F` → `/` first, `%25` → `%`
last, so the `%25` output is not rescanned — which is exactly how a literal
`%2F` in a source path (encoded `%252F`) round-trips instead of collapsing into a
separator.

| source path | `_` (today) | `%2F` |
|---|---|---|
| `assets/x.png` | `assets_x.png` | `assets%2Fx.png` |
| `a/b.png` | `a_b.png` ⚠️ | `a%2Fb.png` |
| `a_b.png` | `a_b.png` ⚠️ collides with the above | `a_b.png` |
| `__a.png` | `__a.png` | `__a.png` |
| `a_/b.png` | `a__b.png` | `a_%2Fb.png` |
| literal `a%2Fb.png` | `a%2Fb.png` | `a%252Fb.png` |

Because the encoding is injective, **the collision defect is fixed by
construction**: two distinct sources can no longer produce one name, the dedupe
becomes sound, and the doc comment's claim becomes true.

**Rejected: `/` → `__`.** The obvious fix, and wrong. It is a substitution, not
an escaping scheme — it never escapes its own delimiter, so it cannot be
bijective:

| source | `__` encoding | decodes to |
|---|---|---|
| `__a.png` | `__a.png` | `/a.png` — **absolute path at filesystem root** |
| `a_/b.png` | `a___b.png` | `a/_b.png` — wrong |
| literal `a__b.png` | `a__b.png` | `a/b.png` — wrong |

The first is disqualifying: #37's export would write to `/a.png`.

**Rejected: escaped underscore** (`_` → `__`, then `/` → `_s`). Equally bijective
and certain to be accepted by any API, but produces names like
`docs_sguide_simg.png`. Kept as the fallback if percent-encoding had failed the
live probe.

**Rejected: fullwidth solidus `／`** (U+FF0F). Renders almost identically to a
slash in Confluence's UI, which is genuinely attractive, but it is a
confusable-character hazard — users copy the name and get a character that is not
a slash — plus non-ASCII filename risk on upload and on export to disk.

### Names are page-anchored; the documentation root bounds what may be published

Image *resolution* stays page-relative — `filepath.Join(baseDir, src)` where
`baseDir` is the markdown file's directory — unchanged, and the same thing GitHub
does rendering a repo's markdown.

Two separable questions follow: what bounds the "too far out" check, and what the
name is anchored to. With cwd `docs/` and page `docs/guide/foo.md`:

| src | resolves to | A: root-anchored | **B: page-anchored, root-bounded** | C: page-anchored, page-bounded |
|---|---|---|---|---|
| `image1.png` | `docs/guide/image1.png` | `guide%2Fimage1.png` | `image1.png` | `image1.png` |
| `sub/deep.png` | `docs/guide/sub/deep.png` | `guide%2Fsub%2Fdeep.png` | `sub%2Fdeep.png` | `sub%2Fdeep.png` |
| `../assets/logo.png` | `docs/assets/logo.png` | `assets%2Flogo.png` | `..%2Fassets%2Flogo.png` | ❌ BROKEN |
| `../../outside.png` | `outside.png` | ❌ BROKEN | ❌ BROKEN | ❌ BROKEN |

**B is chosen**, because it is how these files behave viewed on GitHub: the path
the author wrote is the truth, and a shared asset directory above the page works.

- The name derives from the `src`, so it **never depends on which directory
  markfluence was invoked from** — no `--root` flag is needed, and cwd only
  affects the boundary check, not naming.
- A is the only model where `..` can never appear in a name, which would let
  decode refuse both absolute and escaping paths. Its price is that the name
  depends on the invocation directory: run from `docs/guide/` instead of `docs/`
  and the same image gets a different name, silently orphaning the old
  attachment — the exact migration pain this work exists to stop repeating.
- C keeps stable names *and* the clean invariant, but bans shared asset
  directories outright, forcing assets to be duplicated under each page's
  directory.

markfluence is expected to be run from the root of the documentation tree. An
image resolving outside it is `IMAGE BROKEN: … (outside the documentation root)`,
consistent with the existing broken-image handling. The check fails open when cwd
is unknown: it is an authoring guard, not a security boundary.

### Decode refuses absolute paths only

The encode side normalizes (`path.Clean`, strip a leading `/`) so an absolute path
is never produced. On decode, an absolute result therefore proves the attachment
did not come from markfluence — refuse it and fall back to the raw attachment
name.

`..` is deliberately **not** refused: under model B we produce it legitimately.

No encoding can fix the underlying ambiguity for foreign attachments — a
hand-uploaded `%2Fetc%2Fpasswd.png` decodes to `/etc/passwd.png` under *any*
scheme, because there is no way to distinguish a name we wrote from one a human
typed. That is why the guard lives on the decode side rather than in the
encoding.

### The comment carries the source path

Decoding a name is inference; the comment is truth, and markfluence already
writes one on every attachment it uploads. New format, retiring the dead `mzcld`
name (markfluence has not been that tool for a long time, and #9's
`attachment-list` would surface the string to users):

```
legacy:  mzcld:checksum: ab12cd…
new:     markfluence: sha256=ab12cd… path=assets/x.png
```

`path=` is written last and unquoted so a source path may contain spaces.

**Both forms are parsed, and skip/update compares the parsed `sha256` rather than
the raw comment string.** Without that, changing the format would force a
needless re-upload of every attachment whose name did not change; with it, an
unchanged root-level image keeps its legacy comment and is still correctly
skipped. The path is stamped the next time the file actually changes.

This also gives #9's `attachment-list` a real signal for whether an attachment is
markfluence-managed.

### `StorageToMarkdown` takes a sources map

It is a pure function over a storage string — no client, no page id — so it
cannot look up comments itself:

```go
func StorageToMarkdown(storage string, sources map[string]string) (string, error)
// sources maps attachment name → source path; nil falls back to decoding names.
```

`read` builds the map from `ListAttachments`, but **only when the body actually
contains `ri:attachment`**, and a failed listing passes `nil` rather than failing
the read — the same tolerance `read` already applies to page width
(`cmd/read/read.go:142`). There is exactly one production caller
(`cmd/read/read.go:85`), so the signature change is cheap.

Threading the map requires converting the render chain (`blockStrings`,
`renderBlock`, `renderList`, `renderListItem`, `renderTable`, `cellTexts`,
`renderMacro`, `renderCallout`, `renderInlineChildren`, `renderInline`,
`renderLink`, `renderImage`, `renderRawBlock`) into methods on a small
`mdRenderer`, mirroring the forward direction's `storageRenderer`.

**Rejected: rewriting `ri:filename` in the parsed tree** after `parseStorage`,
which would need no signature changes at all. It would corrupt attachment
references inside unknown macros, which pass through as raw storage and are
re-serialized verbatim — writing a decoded path back into published content.

## Migration: orphaned attachments

markfluence never deletes. For every already-published page with a subdirectory
image, the next `update` uploads under the new name, rewrites the body to
reference it, and leaves the old `assets_x.png` attached but unreferenced.
Nothing breaks and no data is lost, but the cruft is permanent and accumulates
with every page published under the old scheme before this lands — which is an
argument for doing it sooner rather than later.

## Verified against the live API

Atlassian documents none of the attachment-name rules, so percent-encoding was
**probed before any code was written**; the fallback was escaped-underscore.

- **`%` is stored verbatim.** Uploaded as `probe%2Fsub%2Fx.png`; both the create
  response and a later `GET .../child/attachment` return the title unchanged —
  not rejected, not stripped, not normalized back to a slash.
- **`ri:filename` matching is literal.** Rendered non-destructively via
  `POST /wiki/rest/api/contentbody/convert/view?contentIdContext=<page>`: the
  percent-encoded name resolves to a real `confluence-embedded-image`,
  structurally identical to an underscore-named control, while a nonexistent
  attachment renders the `unknown-attachment` placeholder — so the test
  discriminates. The rendered `src` is **double-encoded** (`%252F`), i.e.
  Confluence treats `%2F` as literal filename characters and escapes them
  correctly.
- **The comment format survives** verbatim under `metadata.comment`.
- **Download works**: `{base}/wiki` + `_links.download` → 200, bytes identical.

End-to-end after implementation, against a real page: `assets/markfluence-test.png`
published as `assets%2Fmarkfluence-test.png`, the page renders 3 embedded images
with 0 placeholders, `read` returned `![…](assets/markfluence-test.png)` with the
directory recovered, and a re-run skips.

Incidental findings, recorded for #9: `expand=extensions` yields `fileSize` and
`mediaType`; `version.number` is available; the collection carries
`size`/`limit`/`start` and **omits `_links.next` when results fit one page**, so
`start`/`limit` offset pagination is the reliable approach; `_links.download` has
the form `/rest/api/content/{pageId}/child/attachment/{attId}/download`.

## Testing

- `internal/convert/attachname_test.go`: round-trip over every edge case above —
  the three `__` failures, the escape character itself, spaces, `../` — plus an
  injectivity test (the property the dedupe depends on), normalization of
  equivalent spellings, and refusal of names decoding to absolute paths.
- Regression cases: `images-shared-parent` (a page in a subdirectory using
  `../assets`, proving the supported layout publishes as `..%2F…`) and a third
  entry in `images-broken` for an image above the root.
- `internal/convert/storage_to_md_test.go`: a recorded source overrides a decoded
  name; an absolute recorded source is refused.
- `internal/client/client_test.go`: comment construction and parsing of both
  forms, that an upload stamps `path=`, and skip-on-match for the new format. The
  **existing** skip/update tests are repointed at `legacyChecksumPrefix`, so they
  become the legacy-tolerance coverage.
- Goldens: `make regen-regressions`, plus the `storage2md/images` fixtures, whose
  `input.storage` moves to the new scheme so `output.md` demonstrates the decode.

## Docs

- `README.md`: images section — resolution is page-relative like GitHub, run from
  the documentation root, the encoding itself with worked examples, and a note
  that republishing leaves the old underscore-named attachment behind.
- `CLAUDE.md`: `attachname.go` in the converter breakdown, the root-bounding rule
  in `images.go`, and the comment format plus tolerant-parsing rationale on
  `SyncAttachments`.

## Commits

1. `feat(convert): percent-encode image paths into attachment names` — encoding,
   normalization, root bounding, `Attachment.Source`, tests.
2. `feat(convert): recover an image's original path when reading a page` — decode,
   `sources` param, `mdRenderer`, `read` plumbing, goldens.
3. `feat(client): record an attachment's source path in its comment` — comment
   format, tolerant parsing, compare on parsed sha.
4. `docs: document attachment name encoding and the documentation root`.
