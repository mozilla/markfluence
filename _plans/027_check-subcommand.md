# Plan: the `check` command

Add an offline validator for markdown files. Closes #42.

## Current state of the codebase

#42 asks for a `check` subcommand that runs the converter over one or more
markdown FILEs and reports problems without requiring network access or
credentials. That is not the thin wrapper the issue first assumed.

Current state:

- `internal/convert`'s whole diagnostic surface today is three `Broken`
  messages (unsupported image extension, image outside root, image
  not-found/not-regular), three `Warnings` (two image-property, one table),
  and exactly one link diagnostic — `links.go:94`,
  `"link not resolved: %s"` — fired whenever a sibling `.md` link doesn't
  resolve in the index. Nothing else in `links.go` touches
  `r.broken`/`r.warnings`.
- That single link warning doesn't distinguish *why* a link failed: a target
  that's missing entirely, one that resolves outside the documentation root,
  and one that exists but has no `page_id` yet all produce the identical
  message. A `#fragment` that matches no heading produces no diagnostic at
  all — both anchor branches in `rewriteHref` silently no-op on a miss.
- `MdToConfluence`'s signature is `(md *frontmatter.MarkdownFile, root
  *project.Root, index *linkindex.Index, baseURL, spaceKey, version string)`.
  `root` comes from `internal/project` (walks up from a file looking for
  `markfluence.yaml`; falling back to the starting directory when none is
  found is not an error). `index` comes from `internal/linkindex.Build`,
  which walks the whole tree under `root` once, collecting every `.md`
  file's `page_id`/title (when present) and heading anchors.
- Both `root` and `index` are filesystem-only — `internal/linkindex` never
  imports `internal/client` — so nothing in the conversion path requires
  network access or credentials.
- `update`/`create` already build this `root`/`index` pair once per batch via
  `internal/project.Cache`/`internal/linkindex.Cache`, resolved per file and
  shared across every file under the same root; `check` needs the identical
  plumbing just to call `MdToConfluence` at all.
- `idx.anchors[path]` is populated (even to an empty map) for every walked
  `.md` file regardless of whether it has a `page_id` — enough to answer
  "does this path exist under root" almost for free.
- `resolveDocKey`/`rootRelativeKey` (`convert.go`) never reject a
  `../`-prefixed result: an escaping link currently lands in "not found"
  only because the index can't contain anything outside root by
  construction, not because anything explicitly checks for it.
- `docs/guarantees.md`'s R1 (`report-unresolved-references`) already reads
  **Partial**, not **Aspirational** — the existing link warning is why.
- `root.go`'s `PersistentPreRunE` doesn't construct an
  `internal/client.ConfluenceClient`; every existing command's
  `client.Resolve` call happens inside its own `run()`, so nothing upstream
  forces a client into existence.

Per interview: the diagnostic gaps above are close enough to #42's own goal
that this plan folds fixing them in now rather than shipping `check` narrowly
and revisiting the diagnostic surface a second time later. It's still
sequenced as separate, atomic commits within one PR/branch — converter
changes first, then the command that surfaces them.

## Decisions locked

### Command: `check FILE...`, required, exactly like `fix`

`cobra.MinimumNArgs(1)` + `ValidArgsFunction: completion.MarkdownFiles`. No
bare `markfluence check` project-wide scan — no other command does that, the
issue's own wording is "one or more markdown FILEs", and the stated use case
(a pre-commit/CI hook) passes its own file list (e.g. `git diff
--name-only`) rather than wanting markfluence to walk anything.

### No `client.Resolve`, no HTTP, no file writes, ever

`check`'s `run()` never imports `internal/client`. `root.go`'s
`PersistentPreRunE` doesn't construct a client either, so nothing upstream
forces one into existence — `check` is simply the first command whose `run()`
doesn't call `client.Resolve`. It is read-only on disk too: no frontmatter
write-back, unlike `fix`/`create`.

### `project.Cache` / `linkindex.Cache`, exactly like `create`/`update`

```go
rootOverride, _ := cmd.Flags().GetString("root")
roots := project.NewCache(rootOverride)
defer roots.Close()
indexes := linkindex.NewCache()
```

Per file: `root, err := roots.Resolve(filepath.Dir(filename))`, then `index,
err := indexes.Get(root)`. The envelope's top-level `roots` field is
populated from `roots.Roots()`, matching `update`.

### `baseURL`/`spaceKey` hardcoded, no flags

`https://wiki.example.net` / `ENG` — the regression suite's own defaults —
passed straight to `MdToConfluence`. Both are used only to build the *text*
of a rewritten href; nothing in `Broken`/`Warnings` reads either, since
resolution runs off `index`. Hardcoding makes `check` byte-identical across
machines, which is what a CI gate wants, and drops two flags with no effect
on what's reported.

### Link severity: four buckets, and a broken link now rewrites the output

Per interview, a broken link is treated the same way `images.go` already
treats a broken image: `Broken` doesn't just add a diagnostic string, it
**replaces the published element** with literal text (`LINK BROKEN: ...`),
not just the plain href passthrough that happens today. This is a real
behavior change to what `update`/`create` publish, not merely a `check`-only
diagnostic; it also affects any already-published page containing a link to
a genuinely missing/escaping target.

| case | severity | message (line-number prefix per below, omitted here for brevity) | output change |
|---|---|---|---|
| target `.md` not found anywhere under root | **Broken** | `LINK BROKEN: %s (not found)` | `<a>` + text replaced with literal message |
| target `.md` resolves outside the documentation root | **Broken** | `LINK BROKEN: %s (outside the documentation root)` | same |
| target `.md` exists, has no `page_id` yet | Warning (unchanged) | `link not resolved: %s` | none — href renders as-is, same as today |
| target has a `page_id`, but `#fragment` matches no heading | Warning (new) | e.g. `anchor not found: %s` | none |

A fragment-miss warning only fires when the target file itself exists — if
the whole target is missing/escaping, that's reported once as `LINK BROKEN`,
not doubled up with a redundant anchor warning. Applies to both anchor
branches in `rewriteHref` (same-page `#frag` and cross-file `path.md#frag`).

**Renderer restructuring** (`links.go`/the `ast.Link` node renderer): today
`renderLink` writes `<a href="...">` on `entering` and `</a>` on `!entering`
as two independent writes, with child text nodes rendered by goldmark's
walker in between. Replacing the whole element means `entering` must detect
"broken", write the literal message, and return `ast.WalkSkipChildren` — but
goldmark still invokes the renderer a second time on `!entering` regardless
of that skip (`WalkSkipChildren` only suppresses descending into children,
not the node's own second visit). Needs a small per-node flag on
`storageRenderer` (same pattern as `r.seen` for image dedup) to suppress the
stray `</a>` on the matching leave call.

**New plumbing needed:**

1. **`Index.FileExists(path) bool`.** No `Build` changes needed —
   `idx.anchors[path]` is already populated (even to an empty map) for every
   walked `.md` file regardless of `page_id`, so existence is
   `_, ok := idx.anchors[path]`.
2. **An explicit escape check on the query side.** `resolveDocKey` →
   `rootRelativeKey` (`convert.go:135`) returns whatever `filepath.Rel`
   produces with `ok=true` unconditionally; it never rejects a `../`-prefixed
   result the way `images.go`'s `rootRelative` does for image paths. Today an
   escaping link happens to land in "not found" only because the index can
   never contain anything outside root by construction (`DocKeyFor`'s own doc
   comment, and `linkindex`'s package doc, both already explain why *that*
   side needs no clamp) — but the query side still needs a lexical check
   (`r == ".."` or `strings.HasPrefix(r, "../")`) to tell "outside root" apart
   from "not found" for the two distinct Broken messages.
3. **A fragment-miss warning.** Both anchor branches in `rewriteHref`
   currently no-op silently on a miss; add it there, gated on `FileExists`.

Because this is a converter change, `update`/`create` inherit these
diagnostics (and the output change) for free via the shared
`ConfluencePage.Broken`/`Warnings` fields — no command-specific plumbing
needed there beyond confirming the existing pass-through still works.

### Every Broken/Warning message gains a source line number

`"LINK BROKEN: ../outside.md (outside the documentation root)"` doesn't say
*where* in the document that link is — a real gap for anyone acting on
`check`'s output, not just a nicety. Originally scoped out of this plan as
"bigger than this issue" on the assumption that it needed AST rewiring; it
doesn't, for the common case:

- The `NodeRenderer` interface already passes the raw source bytes to every
  renderer function (`renderImage` already uses this for alt text via
  `nodeText`; `tableCellBGTransformer.warn` (`tables.go:154`) already has
  both `cell ast.Node` and `source []byte` in scope; only `renderLink`
  currently discards it — the parameter is literally named `_`).
- Neither `ast.Link` nor `ast.Image` carries its own position (goldmark's
  parser never calls `SetLines` on either), but their child `*ast.Text`
  nodes do, via `Segment.Start` — an already-established pattern (`nodeText`
  in `images.go:188` walks exactly these segments today). A byte offset
  converts to a 1-indexed line by counting newlines in `source[:offset]`.
- A new shared helper, `nodeLine(n ast.Node, source []byte) (int, bool)`
  (next to `nodeText` in `images.go`), walks to the first descendant
  `*ast.Text` and returns its line; `ok=false` when a node has no text
  descendant at all (e.g. an empty link), in which case the message stays
  unprefixed rather than showing a wrong line.

**Format**: prefix the existing message text, `"line %d: "` — e.g. `"line
12: LINK BROKEN: typo-target.md (not found)"` — rather than changing
`Broken`/`Warnings` to structured entries. This is deliberately the light
version: a fully structured `{line, column, message}` entry (what an LSP
integration would eventually want for a `Range`) stays out of scope, per
below.

**Call sites, and the one real threading cost**: `images.go`'s four
`Broken` and two `Warnings` sites, and `tables.go`'s `warn`, already have a
node and `source` in scope — one-line additions each. `links.go` needs real
threading: `renderLink` must stop discarding `source`, and `rewriteHref`/
`rewriteDocLink` (currently `href string` only) need the line/`ok` pair
threaded through to their warning-append sites (the not-resolved case and
both anchor-miss branches). Contained to `links.go`'s existing private
functions — no AST changes, no changes outside `internal/convert`.

**Ripple**: this changes the text of every *existing* `Broken`/`Warning`
message, not just the new link ones — every regression fixture with an
image warning/broken case needs its golden `test.output` regenerated
(`make regen-regressions`), and the README's example messages need
updating to match.

Frontmatter-sourced diagnostics (`page_width`, `page_id`, the unterminated
block) do **not** get a line number here — they're not goldmark-AST-based
at all, and unlike a link buried in a long body, a bad frontmatter key is
already trivially locatable (a handful of lines at the top of the file).

### Frontmatter validation: three checks, one of them shared across every command

- **`page_width`**: reuse `pagewidth.Declared(frontmatter)` verbatim.
- **`page_id`**: reuse `pageref.IsDigits` — flag only a *present* non-numeric
  value, never require one.
- **Unterminated frontmatter block**: new, and per interview wired into
  `internal/frontmatter.Parse` itself rather than kept `check`-only, since
  `update`/`create`/`fix` all hit this exact silent misread today (a file
  starting with `---\n` that never closes it isn't an error to `Extract` — it
  falls back to "no frontmatter, whole file is body", `frontmatter.go:30-50`,
  which for `create` surfaces as a confusing downstream "missing title" error
  instead of "your frontmatter is malformed"). `Parse`'s signature changes to
  `(*MarkdownFile, error)`; `ParseFile` propagates it. Six call sites need
  updating: `cmd/update`, `cmd/fix`, `cmd/create` (×2 — the file itself and
  the parent `.md` lookup), `internal/pageref`, `internal/linkindex.Build`.
  `Build` walks every sibling in the tree, not just the file under test — on
  this new error it skips that file's entry exactly the way it already skips
  an unreadable one today (silently, via `return nil` from the walk
  callback), so one malformed file elsewhere never blocks checking or
  converting an unrelated one.
- **Explicitly not checked**: "required fields for the intended operation"
  (`page_id`/`space`/`parent` presence) — dropped per the issue's own
  comment, because `check` cannot know intent and a false positive here is
  worse than a miss.

### `--json`: `checkResult` and `checkSummary`

Modeled on `fixResult`/`fixSummary` (`cmd/fix/json.go`), plus a `broken`
field `fix` has no analog for (fix never fails on content, only on I/O/API
errors):

```
checkResult: {
  ok: bool
  status: "clean" | "warnings" | "broken" | "failed"
  file: string
  broken: []string    // always present, [] not null
  warnings: []string   // always present, [] not null
  debug: {              // null unless --show-html, or on a failed file
    html: string          // the converted storage HTML, unindented/compact
    attachments: [{filename: string, path: string, source: string}]  // ConfluencePage.Attachments verbatim
  } | null
  error: string | null
  code: Code | null
}
```

- `clean`: no broken, no warnings. `ok: true`.
- `warnings`: warnings only. `ok: true` — warnings don't fail.
- `broken`: `broken` non-empty (frontmatter or converter). `ok: false`,
  contributes to the batch's non-zero exit.
- `failed`: the file never got a clean answer at all (unreadable,
  unterminated frontmatter block, bad `page_width`, non-numeric `page_id`).
  `ok: false`, `code: VALIDATION`.

`checkSummary` mirrors `fixSummary`'s shape: `{ total, succeeded, failed,
clean, warnings }`.

Schema wiring (`cmd`'s `TestCommandEnumMatchesRegisteredCommands` is
bidirectional): add `"check"` to `schema/json-output/v1.json`'s `command`
enum, an `if/then` branch pinning `checkResult`/`checkSummary`, and the two
`$defs`. `check` is **not** added to `noJSONEnvelope` — it emits a real
envelope, unlike `schema`.

### `--show-html`

There is no existing command that prints the converted storage-format HTML
for inspection: `ConfluencePage.HTML` is only ever consumed internally by
`update`/`create` to publish it, never written to stdout or a file. `check`
is a natural place for a debugging escape hatch, since it already runs
`MdToConfluence` and holds the result — and `ConfluencePage.Attachments`
(the local-image → upload-name mapping) is just as relevant to debugging a
conversion as the HTML body is, so `--show-html` surfaces both, not just the
body.

`--show-html` prints, for a file that reached the converter (not `failed`),
in addition to its diagnostic lines (not instead of them — the point is
seeing "what's wrong" and "what would actually publish" together):

- the storage HTML, **indented by nesting depth** in human mode. The
  renderer already emits one tag per line at every structural boundary
  (confirmed against `testdata/regression/table-cell-colors/test.output` —
  `<table...>\n<thead>\n<tr>\n<th>...`, one element per line already); it
  just never indents by depth. A per-line indent (count open/closed tags at
  each line's start, prefix accordingly) gets full readability without a
  whitespace-normalizing reformatter, which would risk altering meaningful
  inline text mixed into a block.
- the attachment list (filename → source path), when non-empty.

```
$ markfluence check --show-html docs/table-example.md

[docs/table-example.md] clean
[docs/table-example.md] --- storage HTML ---
<table data-layout="align-start">
  <thead>
    <tr>
      <th>Service</th>
      <th data-highlight-colour="#f4f5f7">Status</th>
    </tr>
  </thead>
</table>
[docs/table-example.md] --- attachments ---
diagram.png -> assets/diagram.png
```

In `--json`, this is the `debug` object on `checkResult` above: `null` for a
`failed` file or when the flag wasn't passed; otherwise `{html,
attachments}`, present on every result either way per the schema's
no-`omitempty` rule. `debug.html` stays the compact, unindented string the
converter actually produced — indentation is a human-output display concern,
not a data one, and inserting it into the JSON value would mean the field no
longer matches what `update`/`create` would literally publish.

### Human output

Mirrors `fix`'s `renderHuman`: a `[file]`-prefixed line per broken item
(`ui.Error`) and warning (`ui.Warn`), and a plain "clean" line when there's
nothing to report. A `failed` file (never got a clean answer at all) prints
just its one error line, the same way `fix` short-circuits on failure. The
batch-level summary line only appears when at least one file has `ok:
false`, matching `fix`'s exact wording and its silence-on-success behavior.

```
$ markfluence check docs/intro.md docs/guide.md docs/broken-links.md docs/bad-frontmatter.md

[docs/intro.md] clean
[docs/guide.md] line 8: link not resolved: sibling-draft.md
[docs/broken-links.md] line 5: LINK BROKEN: ../outside.md (outside the documentation root)
[docs/broken-links.md] line 12: LINK BROKEN: typo-target.md (not found)
[docs/broken-links.md] line 19: anchor not found: overview.md#nonexistent-heading
[docs/bad-frontmatter.md] invalid page_width "huge"; expected narrow, wide, or max

2 of 4 file(s) failed.
```

`docs/guide.md` is `warnings`-status (`ok: true`, exits 0 — an unresolved
sibling link is expected in an unpublished tree). `docs/broken-links.md` is
`broken`. `docs/bad-frontmatter.md` is `failed` — it never reached the
converter at all, which is also why its message has no `line N:` prefix:
frontmatter diagnostics aren't goldmark-AST-based and don't get one (see
above).

The matching `--json` result for `docs/broken-links.md`:

```json
{
  "ok": false,
  "status": "broken",
  "file": "docs/broken-links.md",
  "broken": [
    "line 5: LINK BROKEN: ../outside.md (outside the documentation root)",
    "line 12: LINK BROKEN: typo-target.md (not found)"
  ],
  "warnings": [
    "line 19: anchor not found: overview.md#nonexistent-heading"
  ],
  "debug": null,
  "error": null,
  "code": null
}
```

(`debug` is `null` here because `--show-html` wasn't passed in this example.)

### R1 (`docs/guarantees.md`) bumps from Partial to Holds

Per interview: R1 says "every reference markfluence could not resolve is
reported", scoped to the two reference kinds markfluence actually attempts to
resolve — doc-links (`.md` siblings) and images. A relative link to a local
non-`.md`, non-image file (e.g. a PDF) gets zero existence checking, before
or after this PR: `rewriteDocLink` only ever attempts resolution for hrefs
ending in `.md`, so that case is never a resolution attempt at all — by
design, since only images are uploaded and a relative href to anything else
would be dead regardless. That sits outside R1's claim rather than inside it
unmet, so it doesn't block Holds. State this explicitly in the guarantees.md
update rather than letting it be noticed later — this is a status bump with
its own commit message per CLAUDE.md's rule on guarantee statuses, not folded
into another commit.

## Out of scope (deliberately)

- **A bare `markfluence check` project-wide scan.**
- **Style/lint rules** (heading levels, line length, prose linting).
- **Any network-based staleness check.** That's `update`'s mtime check.
- **Structured (non-string) broken/warning entries.**
  `ConfluencePage.Broken`/`Warnings` stay `[]string`; a line number is a
  text prefix (see above), not a `{line, column, message}` field. The
  structured version is what a future LSP integration would want for a
  `Range`, but nothing here needs it.
- **Existence-checking non-`.md`, non-image relative links** (e.g. a PDF).
  Explicitly named in the R1 update as outside its claim, not silently
  dropped.

## Steps

1. `docs(plans): plan the check command` — this file.
2. `feat(frontmatter): detect an unterminated frontmatter block` — `Parse`
   gains an error return; update all six call sites; `linkindex.Build` skips
   the file on this error the same way it skips an unreadable one.
3. `feat(linkindex): track file existence independent of page_id` —
   `Index.FileExists`.
4. `feat(convert): reject an escaping link the way images.go already does` —
   the `../`-prefix check on the query side, `LINK BROKEN: %s (outside the
   documentation root)`.
5. `feat(convert): report a missing link target as Broken, not a warning` —
   `LINK BROKEN: %s (not found)`, using `FileExists` to distinguish it from
   "no `page_id` yet" (stays a warning); restructure `renderLink` to replace
   the whole element on Broken (the per-node flag for the stray `</a>`).
6. `feat(convert): warn on a fragment that matches no heading` — both
   branches in `rewriteHref`, gated on `FileExists`.
7. `feat(convert): prefix Broken/Warning messages with a source line
   number` — the `nodeLine` helper; wire it through `images.go`'s four
   `Broken`/two `Warnings` sites and `tables.go`'s `warn`; thread `source`
   through `renderLink`/`rewriteHref`/`rewriteDocLink` for the link sites
   (new and pre-existing alike); regenerate every regression golden touched
   (`make regen-regressions`) and update the README's example messages.
8. `feat(check): add the check command` — `cmd/check/{check,json}.go`,
   registration in `root.go`, `completion.MarkdownFiles`, the schema's
   `command` enum entry + `if/then` branch + `checkResult`/`checkSummary`
   defs, including the `debug` object and the `--show-html` flag (storage
   HTML plus the attachment list, with per-line indent-by-depth in human
   output).
9. `docs(guarantees): bump R1 to Holds` — with the scope note above.
10. `docs(readme): document the check command and the new link-severity
    messages`.
11. `docs: add check to the architecture notes` — CLAUDE.md gains a
    `cmd/check/` bullet; the `internal/convert` writeup gains the
    link-severity split and the line-number prefix (now affecting
    `update`/`create` too, not just `check`); `internal/frontmatter`'s
    writeup notes `Parse`'s new error return.

## Testing

**`internal/frontmatter`.** New detector: a file with `---\n` and no closing
line is flagged; a file with proper frontmatter, no frontmatter at all, or a
`---` appearing only in the body (e.g. inside a fenced code block) is not a
false positive. `Parse`'s new error propagates through `ParseFile` and all
call sites.

**`internal/linkindex`.** `FileExists` true for an indexed page, true for a
`page_id`-less file, false for a genuinely absent path; unaffected by
`TestCacheBuildsOncePerRoot`'s memoization contract. `Build` skips (rather
than fails) a sibling with an unterminated frontmatter block.

**`internal/convert`.** One regression case per severity bucket: link to a
nonexistent file (Broken/not found, output replaced), link that resolves
outside root via `../` (Broken/outside root, output replaced), link to a real
page-id-less sibling (Warning, message and output unchanged), link with a
`#fragment` matching no heading (new Warning, output unchanged). Confirm
`update`/`create` pick these up for free via the shared `ConfluencePage`
fields. `nodeLine`: correct line for a link/image on line 1, a link/image
several lines down, one inside a nested construct (a link inside a list
item inside a blockquote), and `ok=false` (no line prefix) for a node with
no text descendant (e.g. `[](target.md)`). One existing image-Broken and one
existing table-warning regression case confirm the line prefix lands on
already-existing messages too, not just the new link ones.

**`cmd/check`.** One test per `status` value end-to-end (clean,
warnings-only, broken, failed), `--json` envelope shape via
`internal/schematest`, exit code 0 for clean/warnings, non-zero for
broken/failed, `roots` populated correctly for a batch spanning one and for a
batch spanning two `markfluence.yaml` roots, `ValidArgsFunction` wired
(`TestSubcommandsCompleteArgs`), confirmation that `run()` never constructs a
`client.ConfluenceClient`, and `--show-html`: `debug` populated (`html` plus
`attachments`) for a clean/warnings/broken file, `null` on a `failed` file,
`null` when the flag is omitted; the human-output indenter on a case with
nested tags (e.g. the `table-cell-colors` regression fixture) matches
expected depth and never alters text content.
