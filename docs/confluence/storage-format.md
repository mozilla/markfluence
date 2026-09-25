# Storage format

Atlassian does not document this. Everything here was established by pushing
markup at a real instance and reading back what came out.

Read [README.md](README.md) first if you are about to run an experiment — in
particular, **`body.storage` proves only what was stored, never what renders.**
Confluence stores plenty it ignores. ADF (`body-format=atlas_doc_format`) is the
model Cloud renders from.

## Storage does not round-trip byte-for-byte

The passthrough for raw `ac:`/`ri:` markup relies on Confluence returning what
it was given. It very nearly does, with two rewrites.

**Verified 2026-08-07**, writing storage via the REST API and diffing what came
back:

```diff
- <col style="width: 200.0px;"/>
+ <col style="width: 200.0px;" />

- <ac:structured-macro ac:name="info">
+ <ac:structured-macro ac:name="info" ac:schema-version="1" ac:macro-id="ab4c0c54-…">
```

1. Self-closing tags gain a space before `/>`.
2. Macros gain server-generated `ac:schema-version` and `ac:macro-id`.

The macro-id injection is why `storage_to_md.go` strips `ac:macro-id` and
`ac:local-id` on the way back — without it, reading a page and republishing it
would churn ids forever.

So the guarantee is **semantic**, not byte-for-byte, which is also the
converter's stated design target.

### A page from the editor does not survive an export and republish byte-for-byte

**Verified 2026-09-05**, exporting a live page the editor had written and
publishing the Markdown back. The stored storage changed in two ways, neither of
which changes what renders:

- The editor writes a list item as `<li><p>text</p></li>`; the converter writes
  `<li>text</li>`.
- The editor's TOC macro carries `ac:local-id`, `ac:macro-id` and `data-layout`
  attributes; the converter's canonical form has none of them.

This is why L5 (`roundtrip-from-confluence`, [../design-principles.md](../design-principles.md))
promises the page keeps its *meaning* rather than its bytes. The Markdown side
is stricter: after one cycle it stops changing, which
`TestRoundTripMarkdownIsAFixedPoint` checks over every `storage2md` case.

### Confluence strips HTML comments on write

**Verified 2026-09-13.** A comment does not survive the write at all — it is not
stored and not rendered, and an *inline* one leaves its surrounding whitespace
behind:

```diff
- <p>before</p><!-- generic block comment --><p>mid <!-- inline comment --> text</p><p>after</p>
+ <p>before</p><p>mid  text</p><p>after</p>
```

Note the doubled space in `mid  text`: the removal is not even whitespace-clean,
so a comment cannot be treated as a no-op even positionally.

Two consequences, both about comparing a body markfluence sent against the body
Confluence stored:

- **`client.updateLanded` can never match for a page whose Markdown contains an
  HTML comment.** It recovers a lost response by re-reading the page and
  accepting the write only when version, title *and* `body.storage` all equal
  what was sent — and the stored body will always differ by the stripped
  comment. So for such a page a write that actually landed is reported as a
  failure. Narrow today, because nothing markfluence *generates* is a comment
  (`<!-- bg:COLOR -->` is consumed by the AST transformer and
  `<!-- confluence-toc -->` is substituted), but an author-written comment is
  legal Markdown and passes straight through `html.WithUnsafe()`.
- **A content-based idempotence check has to normalize comments away** or it
  reports a difference on every run for the same file — the exact opposite of
  what it is for. See #149, which proposes exactly that comparison.

## Table layout

Every table markfluence publishes carries `data-layout="align-start"`, which
auto-sizes the table to its content and left-aligns it — what a Markdown table
should look like.

**Verified 2026-08-07.** Each value written to storage and read back as ADF:

| `data-layout` sent | stored | ADF `layout` |
|---|---|---|
| `align-start` | kept | `align-start` |
| `align-end` | kept | `align-end` |
| `center` | kept | `center` |
| `wide` | kept | `wide` |
| `full-width` | kept | `full-width` |
| `default` (verified 2026-09-25) | kept | `default` |
| `bogus-value` | **kept verbatim** | **`None`** — silently dropped |
| *(absent)* | absent | absent |

Two things worth noting. `align-end` is accepted and is missing from the list in
`internal/convert/tables.go`. And an invalid value is stored happily but
discarded by the renderer — a clean demonstration that storage validates nothing.

### A `<colgroup>` induces a layout, chosen by total width

A table with a `<colgroup>` and no `data-layout` does not stay layout-less.
Confluence assigns one, picking the **narrowest layout the table fits in**.

**Verified 2026-08-07.** Single-column tables, colgroup width swept, read as ADF:

| total colgroup width | induced ADF `layout` |
|---|---|
| ≤ 680px | `default` |
| 685–960px | `wide` |
| ≥ 980px | `full-width` |
| *(no colgroup at all)* | none — no layout attribute |

680 and 960 are Confluence's own content-width bands, so this is not an
arbitrary threshold: the table gets the first layout wide enough to hold it.
Column count is irrelevant — only the sum matters. Three columns of 200/910/200
and one column of 1110 both land on `full-width`.

This is worth stating carefully because two earlier write-ups each generalized
from one band and got it wrong. `internal/convert/tables.go` says a colgroup
defaults the layout to `full-width` — true only above ~980px. An earlier draft
of this document said `default` — true only below ~680px, which is where the
one-column 200px test case it was based on happened to fall.

The operational advice is unchanged and is what actually matters: **if column
widths are ever emitted, keep the explicit `data-layout`**, or the table
silently acquires a layout determined by arithmetic nobody wrote down.

## Column widths

`data-table-width` on the `<table>` becomes ADF `width`. Column widths in the
`<colgroup>` become per-cell `colwidth`.

**Verified 2026-08-07**, from a probe sweeping table attributes one hypothesis
per table:

| written | ADF |
|---|---|
| `data-table-width="1110"` | `width: 1110.0` |
| px colgroup, with or without `data-table-width` | `colwidth` preserved as given |
| **percentage** colgroup **with** `data-table-width="1110"` | resolved to px against that width — 25%/75% became `[277.5]`/`[832.5]` |
| **percentage** colgroup **without** `data-table-width` | **`colwidth` dropped entirely** |

So a percentage colgroup is only meaningful alongside `data-table-width`;
without it the widths are silently discarded. px is unconditional.

A table with no attributes at all comes back as ADF `__autoSize: true` rather
than any layout — that is the "auto-sizes but unanchored" state
`data-layout="align-start"` exists to replace.

## Cell alignment rides on a paragraph, not the cell

The obvious form is the one that does nothing. GFM alignment becomes
`align="left|center|right"` on `<th>`/`<td>`, and Confluence stores that and
ignores it.

**Verified 2026-08-07.** Four cells, one page, read back as ADF:

| written | ADF paragraph mark |
|---|---|
| `<td align="center">` | **none** — discarded |
| `<td style="text-align: center;">` | `alignment` / `center` |
| `<td><p style="text-align: center;">` | `alignment` / `center` |
| `<td><p style="text-align: right;">` | `alignment` / **`end`** |

So right maps to `end`. The cell-level `style` form works too, but markfluence
writes the paragraph-level one, since that is what Confluence's own editor emits
and the cell-level form may simply be normalized into it.

**Verified 2026-08-12** on a markfluence-published table whose delimiter row was
`| --- | :--- | :---: | ---: |`, read back as ADF: the center column carries
`alignment` / `center` and the right column `alignment` / `end`, on the `<th>`
row as well as the `<td>` rows. The plain and left columns carry no mark.

### There is no explicit left

**Verified 2026-08-12** on the same page, from hand-written cells alongside the
published table:

| written | stored | ADF mark |
|---|---|---|
| `text-align: left` | kept verbatim | **none** |
| `text-align: start` | **dropped** — comes back as `style=""` | none |
| `text-align: justify` | kept verbatim | none |

Left is Confluence's default and cannot be stated; `start` does not even survive
the sanitizer. So `:---` and `---` publish identically, and a `:---` column reads
back as `---`. Only center and right make the round trip.

> Checking any of this with `body-format=view` will tell you both `align` and
> `text-align` survive, because the legacy renderer echoes them. That is wrong.
> Use ADF.

## Table markup

What else a table may carry, one hypothesis per table.

**Verified 2026-09-25** on a scratch page in the personal space, storage read
back and then ADF, the page trashed afterwards:

| written | stored | takes effect (ADF) |
|---|---|---|
| `<th>` in the first row | kept | header row |
| `<th>` first in every row | kept | header column |
| two header rows in `<thead>` | kept | two header rows |
| `<tfoot>` | kept | an ordinary last row; no footer |
| `<caption>` | **tag dropped, its text left loose** | the caption text becomes a paragraph above the table |
| `colspan`, `rowspan` on `<th>`/`<td>` | kept | yes |
| `style="background-color: …"` on a cell | kept, as `rgb()` | **no** |
| `class="highlight-blue"` on a cell | kept | no |
| `valign="bottom"` on a cell | kept | **yes**, cell `valign` |
| `style="vertical-align: top;"` on a cell | kept | no |
| `scope="col"` on a `<th>` | kept | no |
| `data-colwidth` on a cell | **dropped** | no |
| `data-table-display-mode="fixed"` | kept | `displayMode: fixed` |
| `data-number-column="true"` | **dropped** | no |
| `class="numberingColumn"` on each row's first cell | kept | **numbered column**, those cells removed from ADF |
| `class`, `border`, `width`, `style="width: …"` on `<table>` | kept | no (the `style` width induced `layout: default`) |
| a table inside a cell | kept | **not a table**: a `nested-table` migration extension, "A table in a table cell can't be created or edited in the new editor" |

Colour, alignment, layout and widths are in their own sections here. So the
markup that works is header rows and columns, `colspan`/`rowspan`,
`data-highlight-colour`, `text-align`, `valign`, a `<colgroup>`,
`data-layout`, `data-table-width`, `data-table-display-mode`, and a numbered
column spelled as `numberingColumn` cells. Everything else is stored and
ignored, or dropped; `<caption>` and a nested table do harm.

### What the editor writes on a table

**Verified 2026-09-25** on page 2913502220, three tables made in the editor:
every table carries `data-table-width` (1110 on two, 778 on the third) and
`ac:local-id`, every row and cell carries `ac:local-id`, and one table carries
`data-table-display-mode="default"`. None has a `<colgroup>`.

**Verified 2026-09-25** on page 3109814418, a table `create` published and
then saved in the browser editor. An edit elsewhere on the page, leaving the
table alone, added `ac:local-id` to the table, rows and cells, a bare
`local-id` to every paragraph, `data-table-width="229"`, and a `<colgroup>` of
pixel widths (72, 97, 60) that sum to it -- widths the editor measured, on the
unchanged `data-layout="align-start"`. Colours and alignments were untouched.
Dragging one column border then changed the `<col>` widths (72, 154, 48) and
left `data-table-width` at 229. A save through the API instead (the page's ADF
`PUT` back) added none of this.

So those attributes say nothing about what an author chose. `read` ignores
them when deciding whether a table can be a GFM table (#55,
`internal/convert/storage_to_md_table.go`), along with a span of 1 -- and a
`<colgroup>` of pixel widths only on an `align-start` table, which is what
keeps a markfluence table a GFM table after an editor save. Two are costly:
`data-table-width` and that `<colgroup>` are also what a hand resize records,
so a resized table that reads back as GFM loses the resize on its next
publish. The only sign of a resize is a `<col>` sum that no longer matches
`data-table-width`, seen once and not relied on.

**Tables markfluence did not write are mostly different.** Of 152 tables on
50 pages edited since June 2026 (a CQL sample, 2026-09-25), 83 carry
`data-layout="default"`, 27 `full-width`, and 35 a `<colgroup>`; 27 read back
as GFM. Of 109 tables on 50 pages last edited before 2019, 6 do, and the rest
mostly for structure GFM cannot hold -- no header row (48), merged cells (33),
block content in a cell. `read` keeps a layout other than `align-start` as a
reason to write a table raw: it is visible, and republishing a GFM table would
replace it with `align-start`.

## Cell background colors

`data-highlight-colour` on a `<td>`/`<th>` sets a cell background. It reaches ADF
as a cell `background` attribute, and it accepts **any** hex, not just palette
colors.

**Verified 2026-08-07.** `#ff00ff` and `#c0ffee` both persist and reach ADF,
alongside the named swatches resolving to their hexes (`#ffffff`, `#f4f5f7`,
`#b3bac5`, …). Backgrounds work on `<th>` as well as `<td>`.

The 21 named swatches in `internal/convert/tables.go` are markfluence's
vocabulary, not the server's: they are what the Confluence editor's cell
background picker offers, so a color set from Markdown is indistinguishable from
one set by hand and shows as the selected swatch. Read off an editor-authored page on
2026-08-04; the picker is seven hue columns by three shades, with the grey
column running white / light grey / grey. **Transcribed.**

## Callout macros and ADF panels

A callout has **two** storage spellings, and the vocabularies they use collide.
Read this section before touching `callouts.go` or `calloutMacroInverse`.

### The four macros are four of five ADF panel types

**Verified 2026-09-01.** Each macro published as storage, read back as
`atlas_doc_format`:

| macro published | ADF `panelType` | colour |
|---|---|---|
| `info` | `info` | blue |
| `tip` | `success` | green |
| `note` | `warning` | yellow |
| `warning` | `error` | red |

### Trap: `note` and `warning` mean different colours in each vocabulary

The macro names and the ADF panel types overlap on three strings and agree on
exactly one:

| string | as `ac:name` on a macro | as an ADF `panelType` |
|---|---|---|
| `info` | blue | blue — *the only agreement* |
| `note` | **yellow** | **purple** |
| `warning` | **red** | **yellow** |
| `tip` | green | not a panel type |
| `success` | not a macro | green |
| `error` | not a macro | red |

So "the Note panel" is ambiguous on its own. The `note` *macro* is yellow. The
purple thing the editor calls a Note is `panelType: note`, which has no macro
at all. A map keyed by panel type must never be keyed by macro name or the
other way round.

### Purple is the only panel that is not a macro

**Verified 2026-09-01.** PUTting all five panel types as ADF — which is what
the editor does on any save — and reading the storage back gave four
`ac:structured-macro`s and one `ac:adf-extension`:

```xml
<ac:adf-extension>
  <ac:adf-node type="panel">
    <ac:adf-attribute key="panel-type">note</ac:adf-attribute>
    <ac:adf-attribute key="local-id">54e36e4937ac</ac:adf-attribute>
    <ac:adf-content>…the content…</ac:adf-content>
  </ac:adf-node>
  <ac:adf-fallback>
    <div class="panel …"><div class="panelContent" …>…the same content…</div></div>
  </ac:adf-fallback>
</ac:adf-extension>
```

`ac:adf-extension` is what Confluence falls back to when a construct has no
storage element of its own. It is the only shape a callout takes that is not a
macro, and it only ever appears for a purple panel a human inserted.

### Trap: `ac:adf-fallback` looks like content and is not

The extension carries the same content **twice**: once as the authoritative
`ac:adf-node`, once as a pre-rendered `ac:adf-fallback`. Anything walking the
tree generically renders both. This is exactly the bug that produced #125 —
every purple panel exported twice.

**`ac:adf-fallback` is a cache of a derived rendering, not a source of truth.**
**Verified 2026-09-01:**

- A bare extension with **no** fallback is accepted on a storage PUT, reads
  back as a real ADF `panel` node, and stores byte-identical.
- Confluence does **not** regenerate a stored fallback — the page above still
  had none on a later read.
- `body-format=export_view` on that fallback-less page returns the styled div
  in full: `#EAE6FF` background, `#998DD9` border, complete body.
  `body-format=view` likewise. PDF/Word export is built from `export_view`, so
  the one consumer the stored fallback plausibly served is served without it.

So markfluence neither preserves nor synthesizes a fallback. Preserving one is
worse than dropping it: it goes stale the moment the body is edited, and a page
that renders new text while a fallback consumer sees old text is a silent
divergence.

### `ac:structured-macro` is canonical, not legacy

**Verified 2026-09-01.** Publishing `<ac:adf-extension>` with `panel-type`
`info`/`success`/`warning`/`error` is also accepted, is stored **verbatim**
(not normalized on write), and produces ADF byte-identical to what the macros
produce. But serializing that ADF back to storage yields the *macro*. Since an
editor save is exactly that round trip, the four panel types with macros always
come back as macros.

Confluence's own serializer picks the macro. "Legacy" is the wrong word for it:
publish the macro, and let the extension be what it is — the spelling for a
construct with no macro.

### Trap: a macro `title` parameter does not survive an editor save

**Verified 2026-09-01.** ADF's `panel` node has no title attribute, only
`panelType`. Publishing `<ac:parameter ac:name="title">Heads up</ac:parameter>`
renders a header at first, but the ADF Confluence derives from it is:

```json
{"type":"panel","attrs":{"panelType":"info"},"content":[
  {"type":"paragraph","content":[{"text":"Heads up","marks":[{"type":"strong"}]}]},
  {"type":"paragraph","content":[{"text":"titled info body"}]}]}
```

and the storage after that save has **no `title` parameter** — the title has
become a bold first paragraph of the body.

This is why markfluence does not publish an alert's name as a title, though
`kovetskiy/mark` does: mark is publish-only and never reads a page back, so it
never meets the consequence. For a tool with an export direction the title
compounds — publish `title="Note"`, the editor demotes it to `**Note**` in the
body, export reads that as body text, the next publish sets the title *and*
keeps the bold line, and the next save makes two of them.

### The colour-faithful map

GitHub renders NOTE blue, TIP green, IMPORTANT **purple**, WARNING orange,
CAUTION red
([changelog](https://github.blog/changelog/2023-12-14-new-markdown-extension-alerts-provide-distinctive-styling-for-significant-content/)).
Because purple is reachable, markfluence can match all five:

| alert | GitHub | publishes as | ADF panel |
|---|---|---|---|
| NOTE | blue | `<ac:structured-macro ac:name="info">` | `info` |
| TIP | green | `<ac:structured-macro ac:name="tip">` | `success` |
| IMPORTANT | purple | `<ac:adf-extension>` `panel-type=note` | `note` |
| WARNING | orange | `<ac:structured-macro ac:name="note">` | `warning` |
| CAUTION | red | `<ac:structured-macro ac:name="warning">` | `error` |

The map is bijective, so **nothing is unrecoverable** — `CAUTION` used to fold
into `warning` and could not be read back. `calloutTargets` in `callouts.go`
and `calloutMacroInverse` in `storage_to_md.go` are the two halves.
