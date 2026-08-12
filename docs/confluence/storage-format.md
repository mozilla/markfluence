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

## Callout macros

GitHub alert types map many-to-one onto Confluence's macros, so the mapping is
lossy in one direction: `CAUTION` folds into `warning` and cannot be recovered,
`note` came from `IMPORTANT`, and `info` came from `NOTE`. `calloutMacroInverse`
in `storage_to_md.go` is the canonical inverse. **Transcribed.**
