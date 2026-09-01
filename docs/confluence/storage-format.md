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

## Table layout

Every table markfluence publishes carries `data-layout="align-start"`, which
auto-sizes the table to its content and left-aligns it — what a markdown table
should look like.

**Verified 2026-08-07.** Each value written to storage and read back as ADF:

| `data-layout` sent | stored | ADF `layout` |
|---|---|---|
| `align-start` | kept | `align-start` |
| `align-end` | kept | `align-end` |
| `center` | kept | `center` |
| `wide` | kept | `wide` |
| `full-width` | kept | `full-width` |
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

## Cell background colors

`data-highlight-colour` on a `<td>`/`<th>` sets a cell background. It reaches ADF
as a cell `background` attribute, and it accepts **any** hex, not just palette
colors.

**Verified 2026-08-07.** `#ff00ff` and `#c0ffee` both persist and reach ADF,
alongside the named swatches resolving to their hexes (`#ffffff`, `#f4f5f7`,
`#b3bac5`, …). Backgrounds work on `<th>` as well as `<td>`.

The 21 named swatches in `internal/convert/tables.go` are markfluence's
vocabulary, not the server's: they are what the Confluence editor's cell
background picker offers, so a color set from markdown is indistinguishable from
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
