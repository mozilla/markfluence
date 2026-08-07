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

**Verified 2026-08-07** on page `2952004326`, writing storage via the REST API
and diffing what came back:

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

### A `<colgroup>` induces a layout

**Verified 2026-08-07.** A table with a `<colgroup>` and *no* `data-layout`
comes back as ADF `layout: default` with `colwidth: [200.0]` on the cell, where
the same table without a colgroup has no layout at all.

The code comment in `tables.go` says this defaults the layout to `full-width`.
The value is actually `default`. The operational advice is unchanged and still
correct: **if column widths are ever emitted, the explicit `data-layout` must
stay**, or the table silently picks up a layout nobody asked for.

## Cell alignment: only one form works

GFM alignment becomes `align="left|center|right"` on `<th>`/`<td>`. Confluence
stores that and ignores it.

**Verified 2026-08-07.** Four cells, one page, read back as ADF:

| written | ADF paragraph mark |
|---|---|
| `<td align="center">` | **none** — discarded |
| `<td style="text-align: center;">` | `alignment` / `center` |
| `<td><p style="text-align: center;">` | `alignment` / `center` |
| `<td><p style="text-align: right;">` | `alignment` / **`end`** |

So right maps to `end`, and there is no explicit left — Confluence's default.

Note the cell-level `style` form also works, which issue #48 does not mention;
it only documents the paragraph-level form. Prefer paragraph-level anyway, since
that is what Confluence's own editor emits, and the cell-level form may simply be
normalized into it.

This is issue **#48**, still open: markfluence emits the `align` attribute, which
is the one form that does nothing.

> Checking this with `body-format=view` will tell you both `align` and
> `text-align` survive, because the legacy renderer echoes them. That is wrong.
> Use ADF.

## Cell background colors

`data-highlight-colour` on a `<td>`/`<th>` sets a cell background, and it accepts
**any** hex — `#ff00ff` persists fine (**verified 2026-08-07**), so it is not
limited to a palette.

The 21 named swatches in `internal/convert/tables.go` are markfluence's
vocabulary, not the server's: they are what the Confluence editor's cell
background picker offers, so a color set from markdown is indistinguishable from
one set by hand and shows as the selected swatch. Read off an editor-authored
page (`2913502220`) on 2026-08-04; the picker is seven hue columns by three
shades, with the grey column running white / light grey / grey. **Transcribed.**

## Callout macros

GitHub alert types map many-to-one onto Confluence's macros, so the mapping is
lossy in one direction: `CAUTION` folds into `warning` and cannot be recovered,
`note` came from `IMPORTANT`, and `info` came from `NOTE`. `calloutMacroInverse`
in `storage_to_md.go` is the canonical inverse. **Transcribed.**
