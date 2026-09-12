# A markfluence markdown file

Each Markdown file is one Confluence page: an optional YAML **frontmatter** block
followed by the Markdown **body**.

```
---
title: My Page Title
space: ENG
parent: null
page_id: 1234567890
page_width: max
---

# Body starts here
...
```

## Frontmatter

Frontmatter is a **YAML** block delimited by `---` lines, restricted to flat
`key: value` pairs. A value is a single-line scalar, or a list of them — written
either inline (`labels: [a, b]`) or as `- ` lines. The fields markfluence reads
as single values (`title`, `space`, `parent`, `page_id`, `page_width`) are an
error when written as a list, rather than being read as unset. No nesting, and no multi-line
values. That restriction is enforced: a nested value, a `|` block, a duplicate
key, a tab indent, or a list item split over two lines is an error naming the
key, not something read as blank. Full-line `#` comments and trailing inline
` # ...` comments are preserved when markfluence rewrites a block, and a list
keeps whichever of the two spellings you wrote it in.

Because it is real YAML, a value that YAML would read as something other than a
plain string has to be quoted — a colon-space (`title: "Deploy Runbook: Part 2"`),
a leading `#`, `[`, `{`, `@`, `*`, `&`, `%`, `!`, `|`, `>`, `-`, or `?`, leading
or trailing whitespace, and the words YAML types for you: `true`, `false`, `yes`,
`no`, `null`, `~`, and anything that looks like a number. **markfluence quotes
automatically whenever it writes a value**, so this only matters for frontmatter
you hand-write.

`null` in any spelling (`null`, `Null`, `~`, or an empty value) means *unset*.
A page genuinely titled `null` is written `title: "null"`.

| Field | Value domain | Notes |
| --- | --- | --- |
| `space` | a space key (e.g. `ENG`, or a personal space like `~1234abcd`) | Target space for `create` (or pass `--space`); written back by `create`. Always a key, never a numeric space id. |
| `parent` | `null`, a numeric page **or folder** id, or a relative `.md` path | `null` = top-level page; an id = an existing parent, which may be a page or a Cloud folder (the value is just an id either way — nothing records which kind it is); a `.md` path = a parent authored in the same run (`create` resolves it in dependency order, then rewrites the value to `<page_id>  # <original.md>`). Used by `create` (or `--parent`). |
| `page_id` | a numeric page id, or `null` | The target page. `update` looks it up by `title` and writes it back when missing; `create` writes it after creating the page. `null`/absent means "no page yet." |
| `title` | text (**required**) | The Confluence page title. |
| `labels` | a list of label names, e.g. `[ci/cd, howto]` | The page's labels. **Present means asserted exactly** — a label on the page that the file does not list is removed — and `labels: []` removes them all. **Absent means untouched**, so a page labeled by hand is safe from a run that never mentioned labels. Only `global:` labels are managed; a `my:`/`team:` label is shown by `info` and never written or removed — and if an unmanaged label shares a name with a surplus managed one, the removal is skipped with a warning, because Confluence's removal takes a name with no prefix and would delete the personal label instead. Names are lowercased (with a warning) since Confluence does that anyway; anything else invalid is an error before any write. `fix` writes back the live page's labels, which is how you adopt a page labeled in the UI. |
| `page_width` | `narrow`, `wide`, or `max` | The published page width (the UI's "Adjust width" options; `narrow`/`wide`/`max` map to the `default`/`full-width`/`max` appearance properties). Absent or blank defaults to `max`. `create`/`update` assert it on every publish (so a width set in the Confluence UI is overwritten unless the frontmatter matches); `fix` writes back the live page's width. |

To create a page, you only need to specify the `title` in the frontmatter.

## Body

The rest of this file is the body reference, construct by construct: what each
markdown construct becomes in Confluence storage format.

The design target is *semantic* equivalence to valid Confluence storage, not
byte-for-byte equality, so a construct listed here round-trips in meaning rather
than in markup. What Confluence itself does with the results — and the traps
behind several of these — is in [docs/confluence/](confluence/).

### Fenced code blocks

[GFM fenced code blocks](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-and-highlighting-code-blocks)
are rendered as Confluence code macros, with syntax highlighting for the
languages Confluence supports.

### Tables

[GFM tables](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/organizing-information-with-tables)
are rendered as Confluence tables.

#### Cell background colors

Cell background colors can be specified using an HTML comment at the
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

#### Multi-line cells

Multi-line table cells use a literal `<br>` to break a cell onto more than
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

#### Lists in cells

Lists in table cells use HTML list tags — `<ul>`, `<ol>`, and `<li>` —
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

### GitHub alerts

GitHub alerts — `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`,
`[!CAUTION]` — become Confluence panels in the colour GitHub draws them in:

| alert | colour | published as |
|---|---|---|
| `NOTE` | blue | `info` macro |
| `TIP` | green | `tip` macro |
| `IMPORTANT` | purple | ADF panel (no macro exists for purple) |
| `WARNING` | orange | `note` macro |
| `CAUTION` | red | `warning` macro |

The mapping is one-to-one, so `read`/`export` recover the original
[GFM alert](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts).

Example:

```markdown
> [!NOTE]
> This is a note.
```

### Images

`![alt](./path.png)` uploads a local file as an attachment (or
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

That layout needs a [documentation root](../README.md#the-documentation-root) declared at
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

Every image is bounded by the [documentation root](../README.md#the-documentation-root):
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

### Links to other pages

Links to sibling `.md` files are rewritten to the target page's Confluence
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

An attachment link or an external URL was never meant to resolve here and stays
silent either way. A mention is a link too, but a special one — see below.

### Mentions

A Confluence mention round-trips as an ordinary markdown link to the person's
profile, with an `@` on the link text:

```markdown
Ping [@Ada Lovelace](https://home.atlassian.com/people/712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3) about the deploy.
```

`read` and `export` write that; `create` and `update` publish it back as a real
mention. Three things worth knowing:

- **The `@` is what makes it a mention.** A link to the same URL whose text does
  not start with `@` publishes as a plain link, so you can still link to
  somebody's profile without pinging them.
- **The account id is the only durable part.** The display name is regenerated
  on every `read`/`export`, so it goes stale harmlessly when somebody changes
  their name, and the host is regenerated too — nothing site-specific survives
  into the markdown.
- **An id that names nobody is a warning, not an error.** Confluence accepts any
  account id and renders it as `@Unlicensed user` rather than failing, so
  markfluence looks the id up and says so; nothing else will.

**A colleague who has left keeps their name.** A deactivated account resolves
normally, and Confluence appends the suffix itself, so a round-tripped page
reads `[@Mark Reid (Deactivated)](…)` and records who has gone rather than
losing them.

An id that genuinely does not resolve — a typo, a hand-edited URL — renders as
`[@Unlicensed user](…)`, matching what the page itself will show. The id stays
in the URL, so it is still the easiest thing to correct.

If markfluence cannot *ask* whether an account exists (no network, a rejected
token), the mention is left exactly as it was rather than being given a
placeholder — otherwise one bad moment mid-export would write `Unlicensed user`
over every real name in a tree.

### Comment directives

- `<!-- confluence-toc -->` — replaced with Confluence table-of-contents macro.
- `<!-- markfluence-version -->` — replaced with the build stamp,
  `markfluence VERSION (SHA, DATE)` (the same string `markfluence --version`
  prints).

### Raw Confluence storage format

You can paste Confluence
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
