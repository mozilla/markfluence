# Plan: the export subcommand

Add `markfluence export`, writing a Confluence page and the attachments it uses
to a directory that previews locally and re-publishes. Closes #37.

## #37 is largely obsolete, and its crux is already solved

The issue was written before 018 (attachment name encoding) and #9 (attachment
subcommands). Its central problem statement:

> `read`'s markdown emits the **flattened** attachment filename (`assets/x.png` →
> `assets_x.png`) as the image `src`. For an export to be self-contained, the
> downloaded files and the markdown `src`s must line up — e.g. download each
> attachment into `<attachments-dir>/` and rewrite image `src`s to
> `<attachments-dir>/<filename>`.

That is no longer true, and the proposed fix would now be actively harmful. 018
made the name↔path mapping bijective and recorded the source path on the
attachment, so `read` emits the *original* `assets/x.png`; #9's
`attachment-download` restores attachments to exactly those paths. The two
already line up, from the opposite direction the issue imagined. Verified before
writing this plan:

```console
$ markfluence read 2848423944 > /tmp/exp/page.md
$ markfluence attachment-download 2848423944 --dest /tmp/exp
$ grep -o '!\[[^]]*\]([^ )]*' /tmp/exp/page.md
![Local test image](assets/markfluence-test.png
$ ls /tmp/exp/assets/
markfluence-test.png
```

So three of #37's flags and two of its open questions are answered or void:

| #37 | Status |
|---|---|
| "New capability: downloading attachments" | Done in #9 (`DownloadAttachment`). |
| `--attachments-dir` + rewriting `src`s | **Rejected** — see below. |
| "Attachment name collisions … how to disambiguate" | Void; 018's encoding is injective. |
| The image-link reconciliation "crux" | Already holds. |
| `--pattern` | Dropped; see below. |
| `--recursive` | Deferred; needs new API surface. |

What export actually adds is therefore *not* reconciliation. It is: filtering to
the attachments a page references, deriving a filename, and doing it in one
invocation that reports what it wrote.

## Out of scope (deliberately)

- **`--recursive` / descendants.** Not a flag but new API surface: no client
  method for children or descendants exists. It also drags in questions v1
  shouldn't answer — directory layout for a tree, rewriting `parent` frontmatter
  to `.md` paths so `create` can rebuild the hierarchy, and partial-failure
  semantics across many pages. Filed as a follow-up.
- **`--format storage`.** Dropped entirely; see below.
- **A sidecar metadata file.** Storage exports would need one to carry
  title/space/parent, but sidecar frontmatter is #38's question and this must not
  pre-decide it. Moot anyway once `--format` is gone.
- **Deleting or pruning `--dest`.** markfluence never deletes. An export into a
  directory holding a stale file leaves it there.

## Decisions locked

### No `--attachments-dir`; attachments land at their recorded paths

Rewriting `src`s to `attachments/<name>` would undo 018. The export would say
`attachments/x.png` where the source repo said `assets/x.png`, so re-publishing
the exported file would compute a *different* attachment name
(`attachments%2Fx.png`), orphan the original, and stop matching the layout the
page was published from. The whole point of the last two changes was that the
path survives the round trip.

```
out/
  markfluence-test-page.md      ![](assets/markfluence-test.png)
  assets/markfluence-test.png   ← the path the source repo used
```

### Markdown only — `--format` is dropped

Storage exists to inspect how Confluence stores things; nothing consumes an
exported storage tree, and it is not a round-trip format. `read --format storage
> page.xml` already covers inspection and is barely longer than the export form
would be.

An earlier draft had layout follow format (markdown → restored paths, storage →
flat, matching `ri:filename`). That coupling was self-consistency for its own
sake: it existed so an exported storage tree's references would resolve, for a
consumer that does not exist. Dropping `--format` deletes the entire branch —
one rule, always `.md`, always restored paths.

### Referenced-only by default, `--all-attachments` opts out

An export should be the page and what it needs. This is not hypothetical: a
plain download of the test page yields `assets_markfluence-test.png` (an
underscore-era orphan from 018's migration) and `probe/notes.txt` (unrelated),
neither of which the page references. Orphans are on real pages right now, so
the default matters.

**Referenced means any `ri:filename` value in the raw storage body**, scanned
before conversion. Not just `ac:image` descendants: the converter only
special-cases images, so a narrower scan would drop an attachment link's target
or a reference inside a macro that passes through as raw storage — precisely the
content markfluence deliberately does not interpret. Matching the literal
attribute is exact, because it is the same string Confluence resolves.

This differs from `attachment-download`, which takes everything. That is correct:
`attachment-download` is about the attachment set, `export` is about the page.

### A reference with no attachment is a warning, not a failure

A page can carry `ri:filename="gone.png"` with nothing attached — already broken
in Confluence. Failing the export would blame the exporter for the page's
problem and block exporting a page you are trying to fix. It is reported per
reference and carried in `warnings`, matching how the converter already treats
broken local images as data rather than an error.

### `--file` defaults to a slug of the title, falling back to the page id

The export is meant to become repo content, so it should be named the way a
human would name it: `markfluence test page` → `markfluence-test-page.md`. The
slug is filename-specific — it strips path separators, caps length, and yields
the page id when the title slugs to nothing (punctuation-only or non-Latin
titles), which the existing `githubSlug`/`confluenceSlug` do not do. Those are
heading-anchor sluggers; coupling filenames to anchor rules would mean a change
to anchor generation silently renames exported files.

### Existing files are skipped unless `--force`

One rule for the page file and the attachments alike, matching
`attachment-download`. Re-running an export never clobbers edits made after the
last one, and a skip is reported rather than silent. Rejected: always rewriting
the page file while skipping attachments — two overwrite rules in one command is
hard to remember, and the markdown is the file most likely to have been edited.

### Shared code, because two commands must agree

- **`internal/pagedoc`** — a fetched page as a document: the frontmatter block,
  the attachment-sources lookup, and the storage→markdown call. `read` and
  `export` must emit byte-identical markdown, and duplicating ~45 lines
  guarantees drift. It needs a client (page width, attachment list), so it
  cannot live in `internal/convert`, which is deliberately client-free — that is
  why `StorageToMarkdown` takes a sources map at all.
- **`internal/attachfile`** — resolving where an attachment goes and writing it
  there: `Resolve` (including the traversal clamp) and `Write`.
  `attachment-download` is refactored onto it. The clamp is security-relevant
  and is the single worst thing in this tree to keep two copies of; the existing
  clamp tests move with it, so the refactor is verified by tests written before
  the second caller existed.

### JSON: one result for the page

The target is the page, as with `info`/`read`, and `update`/`create` already
nest an `attachments` array on a per-page result. Entries carry `status` and
`dest_path`, so one object describes every file written.

```json
{
  "ok": true, "page_id": "123", "title": "…", "space": "ENG", "parent": "456",
  "dest_path": "out/markfluence-test-page.md",
  "attachments": [
    {"status": "downloaded", "filename": "assets%2Fx.png", "dest_path": "out/assets/x.png"},
    {"status": "skipped_unreferenced", "filename": "old.png", "dest_path": null}
  ],
  "warnings": ["gone.png is referenced but not attached"],
  "error": null, "code": null
}
```

Attachment statuses: `downloaded` / `skipped` (exists) / `skipped_unreferenced` /
`failed`. Summary is `basicSummary` (`total` is always 1 — the page).

## Command

```
markfluence export PAGE [--dest DIR] [--file NAME] [--all-attachments]
                        [--skip-attachments] [--force] [--dry-run]
```

`--dest` defaults to `.` and is created if missing. Exits 1 if the page fails or
any attachment fails.

## Testing

- `internal/attachfile`: the clamp tests migrated from `cmd/attachmentdownload`
  (escapes, absolute paths, the root-prefix sibling, legitimate `..`), plus
  `Write`'s skip/force/dry-run behavior.
- `internal/pagedoc`: frontmatter assembly and the sources lookup, migrated from
  `cmd/read`; a page with no `ri:attachment` skips the attachment listing; a
  failed listing degrades to nil rather than failing the render.
- `cmd/export`: the referenced-set scan (image, `ac:link`, macro-internal, and a
  reference with no attachment), filename slugging including the id fallback,
  and `json_test.go` schema conformance.
- An end-to-end check against a live page: export, then confirm every non-remote
  `src` in the emitted markdown resolves on disk.

## Docs

- `README.md`: an `export` section, and a note that it is the one-command form of
  `read` + `attachment-download`.
- `CLAUDE.md`: `export` in the layout list, `internal/pagedoc` and
  `internal/attachfile`, and why `--attachments-dir` is deliberately absent.

## Commits

1. `refactor(attachfile): extract attachment file placement`
2. `refactor(pagedoc): extract page-to-markdown rendering`
3. `feat(cmd): add export` (+ schema)
4. `docs: document the export subcommand`
