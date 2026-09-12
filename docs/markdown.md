# Markdown in a markfluence page

What the converter supports in a page **body**, construct by construct. The
frontmatter block above the body is described in the
[README](../README.md#frontmatter); this file is the body reference.

The design target is *semantic* equivalence to valid Confluence storage, not
byte-for-byte equality, so a construct listed here round-trips in meaning rather
than in markup. What Confluence itself does with the results — and the traps
behind several of these — is in [docs/confluence/](confluence/).

## Supported constructs

**Fenced code blocks** are rendered as Confluence code macros and support the
syntax highlighting, but only the languages Confluence supports.
[GFM fenced code](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-and-highlighting-code-blocks)

**Tables** use GFM syntax and are rendered as Confluence tables.
[GFM tables](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/organizing-information-with-tables)

**Table cell background colors** can be specified using an HTML comment at the
start of the cell. They will be invisible in Markdown preview, but will have
the specified background color in Confluence.

```markdown
| Service | Status                     |
| ------- | -------------------------- |
| auth    | <!-- bg:light-green --> ok |
| billing | <!-- bg:light-red --> down |
```

The color is a swatch name from the Confluence editor's cell background palette,
or a literal `#rrggbb` hex for anything else. The 21 swatches, one row here per
column of the editor's picker:

| Light | Medium | Bold |
| --- | --- | --- |
| `white` `#ffffff` | `light-grey` `light-gray` `#f4f5f7` | `grey` `gray` `#b3bac5` |
| `light-blue` `#deebff` | `blue` `#b3d4ff` | `bold-blue` `#4c9aff` |
| `light-teal` `#e6fcff` | `teal` `#b3f5ff` | `bold-teal` `#79e2f2` |
| `light-green` `#e3fcef` | `green` `#abf5d1` | `bold-green` `#57d9a3` |
| `light-yellow` `#fffae6` | `yellow` `#fff0b3` | `bold-yellow` `#ffc400` |
| `light-red` `#ffebe6` | `red` `#ffbdad` | `bold-red` `#ff8f73` |
| `light-purple` `#eae6ff` | `purple` `#c0b6f2` | `bold-purple` `#998dd9` |

Details:

- Confluence colors **cells**, not rows or columns; a colored column is
  implemented with a marker per cell in the column and a colored row is
  implemented with a marker per cell in the row.
- The marker works in header cells too.
- A cell holding nothing but a marker is an empty colored cell.
- The color marker has to be the first thing in the cell. Anywhere else it's
  ignored with a warning, since a stray comment would otherwise do nothing
  visible.
- An unknown color name is dropped with a warning and the cell publishes
  uncolored.

**Multi-line table cells** use a literal `<br>` to break a cell onto more than
one line. A real newline can't be used instead, since a GFM table row has to
stay on one physical line.

```markdown
| Field | Notes                      |
| ----- | -------------------------- |
| Key   | Type: string<br>JQL: "Key" |
```

Confluence's own editor represents a multi-line cell as separate paragraphs
rather than `<br>`; `read`/`export` converts that back to the `<br>` form
shown above, which is what publishes back to the same paragraphs.

**Lists in table cells** use HTML list tags — `<ul>`, `<ol>`, and `<li>` —
directly in the cell, the same way `<br>` is used for a plain line break.
Markdown's own list syntax needs each item on its own line, which a table row
can't do, so it isn't an option here.

```markdown
| Field  | Values                                |
| ------ | ------------------------------------- |
| Status | <ul><li>open</li><li>closed</li></ul> |
```

`read`/`export` recovers the same tags rather than converting them to
anything else.

**GitHub alerts** — `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`,
`[!CAUTION]` — become Confluence panels in the colour GitHub draws them in:

| alert | colour | published as |
|---|---|---|
| `NOTE` | blue | `info` macro |
| `TIP` | green | `tip` macro |
| `IMPORTANT` | purple | ADF panel (no macro exists for purple) |
| `WARNING` | orange | `note` macro |
| `CAUTION` | red | `warning` macro |

The mapping is one-to-one, so `read`/`export` recover the original alert.
[GFM alerts](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts)

Example:

```markdown
> [!NOTE]
> This is a note.
```

**Images** — `![alt](./path.png)` uploads a local file as an attachment (or
references a remote URL); a missing/unsupported image becomes
`line N: IMAGE BROKEN: …` text (`N` is the line it's on in the file).

Image paths resolve relative to the Markdown file, the same way they do when you
view the file on GitHub, so a page in a subdirectory can share an asset
directory above it:

```
docs/                      ← needs a markfluence.yaml here for this to work
  assets/logo.png
  guide/page.md            → ![logo](../assets/logo.png)
```

That layout needs a [documentation root](#the-documentation-root) declared at
`docs/` — without one, each page's root defaults to its own directory, and
`guide/page.md` reaching above itself for `assets/` is out of bounds.

> [!NOTE]
> An image path is a URL, not a filename, so a space or other special character
> has to be percent-encoded — `![shot](assets/my%20image.png)` for a file named
> `my image.png`. This is the same rule GitHub and your editor's preview follow,
> and it is what they produce when they write a link for you.
> 
> The angle-bracket form `![shot](<assets/my image.png>)` is an equivalent
> spelling of the same image. A bare space (`![shot](assets/my image.png)`) is
> not a valid path, so it is not an image at all and stays on the page as
> literal text — again matching what GitHub and your preview show.
> 
> `markfluence read` and `markfluence export` write the encoded form, so a page
> round-trips back to Markdown that still renders.

Every image is bounded by the [documentation root](#the-documentation-root):
one resolving outside it (`../../secrets/x.png`) is reported as
`line N: IMAGE BROKEN: … (outside the documentation root)` rather than
uploaded, and a symlink is refused even when it resolves inside the root.

Confluence attachment names cannot contain `/`, so an image is attached under
its **base name**: `assets/logo.png` is attached as `logo.png`. The path —
relative to the root, not to the page — is recorded in the attachment's
comment, which is what `markfluence read` and `markfluence export` use to put
the file back where it came from. The same file referenced as
`../assets/logo.png` from a page one directory down is the same attachment,
since both resolve to the same root-relative path.

Because the name is only the base name, two images in one file whose names
agree — `arch/diagram.png` and `deploy/diagram.png` — cannot both be published:
an attachment name is unique per page, so one would overwrite the other. That is
refused, naming both paths, and `markfluence check` reports it without
publishing. Rename one of the files.

Extra properties ride in the title as JSON:

```markdown
![alt](x.png '{"title":"…","width":"100","align":"center"}')
```

* `align` is left/center/right;
* `width`/`height` are pixels

A plain title (`![alt](x.png "tooltip")`) becomes the image tooltip.

Examples:

```
![alt text](./path.png)

![alt text](https://example.com/image.png)

![alt text](./path.png "title")

![alt text](./path.png '{"title":"sometitle","width":100}')
```

**Links to sibling `.md` files** are rewritten to the target page's Confluence
URL; **heading anchors** are rewritten to Confluence's anchor scheme.

As with image paths, a link destination is a URL: a sibling whose filename has a
space is written `[see](my%20doc.md)` (or `[see](<my doc.md>)`), and a bare
`[see](my doc.md)` is not a link at all. The same applies to the fragment, so a
non-ASCII heading anchor may arrive as `#caf%C3%A9-section`. Both are decoded
before markfluence matches them against files and headings on disk, so either
spelling resolves.

Whether an unresolved link is reported — and how badly — depends on why:

* A target that **doesn't exist at all**, or **resolves outside the
  documentation root**, is Broken: the whole link element is replaced with
  literal `line N: LINK BROKEN: … (not found)` or
  `line N: LINK BROKEN: … (outside the documentation root)` text, the same
  way a broken image already is.
* A target that **exists but has no `page_id` yet** — the normal state of
  every page in a tree that hasn't been published — is a Warning
  (`link not resolved: …`); the href still renders exactly as written. A
  **same-page anchor** (`#heading`) is internally treated as a link to the
  *current* file, so it hits this exact warning too when the current file
  itself has no `page_id` yet — which reads as though the file names itself
  as missing; it doesn't, that's just this file before its first publish.
* A `#fragment` that **matches no heading** on an otherwise-resolvable target
  is also a Warning (`anchor not found: …`); the link still works, it just
  lands at the top of the page instead of the named heading.

A mention, an attachment link, or an external URL was never meant to resolve
here and stays silent either way.

**Comment directives:**
- `<!-- confluence-toc -->` — replaced with Confluence table-of-contents macro.
- `<!-- markfluence-version -->` — replaced with the build stamp,
  `markfluence VERSION (SHA, DATE)` (the same string `markfluence --version`
  prints).

**Raw Confluence storage format.** You can paste Confluence
[storage format](https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html)
markup (`<ac:…>` / `<ri:…>` elements — any macro, layout, etc.) straight from a
page's **⋯ → View storage format** into your markdown, and it's emitted verbatim.
Two conventions:

- **Leave a blank line** between an `ac:`/`ri:` tag and any markdown you want
  converted (e.g. a macro or layout-cell body). With a blank line the content is
  parsed as markdown; tight against the tags it passes through literally.
- **Put the opening tag on its own line** (or self-close it) so it isn't wrapped in
  a paragraph.

For example, a two-column layout with markdown in each cell:

```
<ac:layout>
<ac:layout-section ac:type="two_equal">
<ac:layout-cell>

Left column with **markdown**.

</ac:layout-cell>
<ac:layout-cell>

Right column.

</ac:layout-cell>
</ac:layout-section>
</ac:layout>
```

Storage markup shown inside a fenced code block stays literal (it isn't activated).
