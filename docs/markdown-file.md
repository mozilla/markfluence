# A markfluence Markdown file

Each Markdown file is one Confluence page. A file has an optional YAML
**frontmatter** block, and then the Markdown **body**.

```
---
title: My Page Title
space: ENG
parent: null
page_id: 1234567890
page_status: Ready for review
page_width: max
---

# Body starts here
...
```

## Frontmatter

Frontmatter is a **YAML** block between two `---` lines. It can hold only flat
`key: value` pairs. A value is a single-line scalar, or a list of scalars. You
can write a list inline (`labels: [a, b]`) or as `- ` lines.

markfluence reads some fields as single values: `title`, `space`, `parent`,
`page_id`, `page_width`, and `page_status`. If you write one of them as a list,
that is an error. markfluence does not read it as unset.

You cannot use nesting or multi-line values, and markfluence enforces this.
Each of these is an error that names the key: a nested value, a `|` block, a
duplicate key, a tab indent, or a list item on two lines. markfluence does not
read it as blank.

When markfluence writes a block again, it keeps full-line `#` comments and
trailing ` # ...` comments. A list keeps the spelling that you wrote it in.

The block is real YAML. Thus you must quote a value that YAML would read as
something other than a plain string:

- a colon and a space (`title: "Deploy Runbook: Part 2"`)
- a leading `#`, `[`, `{`, `@`, `*`, `&`, `%`, `!`, `|`, `>`, `-`, or `?`
- leading or trailing whitespace
- the words that YAML gives a type: `true`, `false`, `yes`, `no`, `null`, `~`,
  and anything that looks like a number

**markfluence quotes each value automatically when it writes it.** Thus this is
important only for frontmatter that you write by hand.

`null` in any spelling (`null`, `Null`, `~`, or an empty value) means *unset*.
For a page with the real title `null`, write `title: "null"`.

| Field | Value domain | Notes |
| --- | --- | --- |
| `space` | a space key (e.g. `ENG`, or a personal space like `~1234abcd`) | The target space for `create`. You can also give `--space`, or set a `space:` default for the whole project (see below). If `--space` and this field disagree, `create` refuses the file. `create` writes it back. `update` refuses a page that is in a different space, and does not move it. It is always a key, and never a numeric space id. |
| `parent` | `null`, a numeric page **or folder** id, or a relative `.md` path | See [`parent`](#parent). `create` uses it, or `--parent`. `update` moves the page to agree with it. |
| `page_id` | a numeric page id, or `null` | The target page. `update` needs one, in the frontmatter or in the entry for the file in `markfluence.yaml`. Without one, `update` fails, and it does not search by title. `create` writes it back after it creates the page. `null` or absent means "no page yet". |
| `title` | text | The Confluence page title. `create` needs it, or `--title`, and writes it back. For `update`, an absent title keeps the live title of the page. An empty `title:` is an error in both. |
| `labels` | a list of label names, e.g. `[ci/cd, howto]` | The labels of the page. See [`labels`](#labels). |
| `page_width` | `narrow`, `wide`, or `max` | The published page width. See [`page_width`](#page_width). |
| `page_status` | the display name of a status **that page** can be given, e.g. `Ready for review` | The colored lozenge that Confluence shows next to the page title. See [`page_status`](#page_status). |

To create a page, you need only the `title` in the frontmatter, or `--title`.

### `parent`

- `null` means a top-level page in the Confluence space.
- An id names a parent that exists. The parent can be a page id or a Cloud folder id.
  Nothing records which kind of thing the id is for.
- A `.md` path names a parent page by its file. The file can be in the same run,
  and then `create` resolves it in dependency order. The file can also be a
  page that you published earlier, and then `create` uses its `page_id` or its
  `pages:` entry. In both cases, `create` then changes the value to
  `<page_id>  # <original.md>`.
- A `.md` path must be inside the documentation root, and it cannot be a
  symlink. Otherwise `create` refuses the file.

`update` moves a page to agree with `parent`. With `parent: null` (or `~`, or
an empty value), it moves the page to the top of its space. With no `parent:`
line, it does not move the page. The page takes its children with it. A move
does not give the page a new version. `update` refuses a parent that it
cannot find, a parent in a different space, and a parent that is the page
itself or a page under it, and it does this before it writes anything.

A new page from `create`, and a page that `update` moves, go last among the
children of the parent. markfluence never changes the order of siblings after
that. To put pages in a different order, drag them in Confluence.

### `labels`

**A present field means that markfluence asserts it exactly.** markfluence
removes a label on the page that the file does not list. `labels: []` removes
all of them.

**An absent field means that markfluence does not touch the labels.** Thus a
run that never mentioned labels cannot change a page that a person labeled by
hand.

markfluence manages only `global:` labels. `page-info` shows a `my:` or
`team:` label, but markfluence never writes or removes one. An unmanaged label
can have the same name as a managed label that markfluence must remove. Then
markfluence skips the removal and gives a warning. Confluence removes a label
by a name with no prefix, so it would delete the personal label instead.

markfluence changes names to lowercase and gives a warning, because Confluence
does that anyway. Any other invalid name is an error before any write.

To adopt a page that a person labeled in the UI, use `page-info` to see its
labels, and copy them into the file.

### `page_width`

These are the "Adjust width" options of the UI. `narrow`, `wide`, and `max`
map to the `default`, `full-width`, and `max` appearance properties.

markfluence **asserts a declared width on every publish**. Thus it overwrites
a width that a person set in the Confluence UI, unless the file agrees.

The two verbs do different things when the field is *absent*. `create` uses
`max`. `update` leaves the live width alone and makes no width request at all.
`--page-width` and a `page_width:` default for the whole project both count as
declared (see below).

`create` writes the width that it used back to the file. Thus after `create`,
the field is not absent. A later `update` asserts that width, and it overwrites
a width that a person set in the UI. To let the UI decide the width, remove the
`page_width:` line after `create`.

### `page_status`

**A present field means that markfluence asserts it. An absent field means
that markfluence does not touch it.** "Does not touch" is exact: for a file
that declares no status, no verb even reads the status of the page.

The names that you can use are not a fixed list. If a name does not agree with
the list, the file fails, and the error lists the names that would work.
`page-info PAGE` lists them too (`page_status/available`).

The match ignores case, because markfluence sends only the id of the status to
Confluence. Thus `ready for review` publishes, and `read` and `export` output
the spelling of the space.

markfluence refuses a *custom* status, also when your own Confluence picker
shows it. A custom status belongs to your account, and not to the space. Thus a
file that names one would publish for you and fail for all other persons.

A file **cannot clear** a status. `page_status:` with no value is an error, and
not an instruction. Every empty spelling of a scalar looks the same as an
unfinished edit. To clear a status, use the Confluence UI.

A status write gives the page a new version. Thus markfluence does not write a
status that already agrees.

Confluence decides which statuses a page can have for each page **and for each
account**, and not for each space. The same account had 4 statuses on a page
that it created, and 3 on a page that it did not create. The write enforces
this. Thus `update` asks the page that it publishes to.

The page of `create` does not exist until `create` makes it. Thus `create`
does a check of the name only after it makes the page. If the new page does
not accept the name, the page stays created, with no status and a warning.
This is also important if you move a tree between spaces, because `export`
writes this field.

### Defaults for the whole project

You can declare `space` and `page_width` one time for a whole project. Put them
in the `markfluence.yaml` that marks the
[documentation root](root-model.md#markfluenceyaml-the-project-file):

```yaml
space: ENG
page_width: max
```

For `page_width`, the sequence is **the flag first, then the frontmatter, then
the project file**. The answer that is nearest to the content wins.

`space` is different at the top. If `--space` and a frontmatter `space:`
disagree, `create` refuses the file, and neither wins.

`update` uses the space from the file, or else the `space:` default, as a
check. If the page is in a different space, `update` refuses the file, because
markfluence cannot know which of the two is the mistake. It never moves a page
between spaces.

For both settings, markfluence reads the project file only when the two levels
above it say nothing. Thus the project file never disagrees with either of
them.

`page_status` is **not** one of these defaults, on purpose. A width is house
style, and a whole tree can share it. A status is a claim about the maturity of
one page. A `page_status: Rough draft` default for the whole project would make
a false claim about most of the tree on every publish.

### The same block, in a different location

Every field above can be in a `pages:` entry in `markfluence.yaml` instead
of in the frontmatter of the file. With this, a Markdown file stays clean and
markfluence still publishes it:

```yaml
pages:
  docs/deploy-runbook.md:
    title: Deploy Runbook
    page_id: 12346
    labels: [runbook]
```

An entry in `pages:` in `markfluence.yaml` is the same frontmatter block but in
a different location. It has the same field names, the same value domains, and
the same canonical sequence. `create` writes an entry for you when the project
uses `pages:` and the file has no frontmatter of its own.

One value has a different spelling in the two locations: a `parent:` that names
a `.md` file. In a `pages:` entry, it is relative to the root. In frontmatter,
it is relative to the file (see
[root-model.md](root-model.md#pages--page-metadata-for-a-pristine-file)).

Both locations are legal, and markfluence says nothing when they agree. A
`pages:` entry is *not* a fourth level of precedence. Frontmatter and a `pages:`
entry are two spellings of one level. Thus when both give a value, markfluence
uses a rule for disagreements, and not a rule for precedence. For the details,
and the rules for path keys, see
[root-model.md](root-model.md#pages--page-metadata-for-a-pristine-file).

## Body

The other part of this file is the reference for the body. For each Markdown
construct, it tells you what the construct becomes in Confluence storage
format.

The design target is *semantic* equivalence to valid Confluence storage format,
and not byte-for-byte equality. Thus each construct here keeps its meaning
through a round trip, but not always its markup. What Confluence does with the
result, and the traps behind some of these constructs, is in
[docs/confluence/](confluence/).

### Fenced code blocks

markfluence renders
[GFM fenced code blocks](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/creating-and-highlighting-code-blocks)
as Confluence code macros. They have syntax highlighting for the languages that
Confluence supports.

### Tables

markfluence renders
[GFM tables](https://docs.github.com/en/get-started/writing-on-github/working-with-advanced-formatting/organizing-information-with-tables)
as Confluence tables.

#### Cell background colors

To give a cell a background color, put an HTML comment at the start of the
cell. The comment is not visible in a Markdown preview. In Confluence, the cell
has the background color.

```markdown
| Service | Status                     |
| ------- | -------------------------- |
| auth    | <!-- bg:light-green --> ok |
| billing | <!-- bg:light-red --> down |
```

The color is a swatch name from the cell background palette of the Confluence
editor. For any other color, use a literal `#rrggbb` hex value. There are 21
swatches. Each row here is one column of the picker in the editor:

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

- Confluence colors **cells**, and not rows or columns. To color a column, put
  a marker in each cell of the column. To color a row, put a marker in each
  cell of the row.
- The marker also works in header cells.
- A cell that holds only a marker is an empty colored cell.
- The color marker must be the first thing in the cell. In any other position,
  markfluence ignores it and gives a warning, because otherwise a stray comment
  would do nothing that you can see.
- markfluence drops an unknown color name and gives a warning. The cell
  publishes with no color.

#### Multi-line cells

To break a table cell onto more than one line, use a literal `<br>`. You cannot
use a real newline, because a GFM table row must stay on one physical line.

```markdown
| Field | Notes                      |
| ----- | -------------------------- |
| Key   | Type: string<br>JQL: "Key" |
```

In storage format, the editor of Confluence records a multi-line cell as
separate paragraphs, and not with `<br>`. `read` and `export` change that back
to the `<br>` form above.
That form publishes back to the same paragraphs.

#### Lists in cells

For a list in a table cell, use HTML list tags directly in the cell: `<ul>`,
`<ol>`, and `<li>`. You write them as inline HTML in the cell, as you write
`<br>` for a line break. The list syntax of Markdown needs each item on its own
line, and a table row cannot do that. Thus you cannot use it here.

```markdown
| Field  | Values                                |
| ------ | ------------------------------------- |
| Status | <ul><li>open</li><li>closed</li></ul> |
```

`read` and `export` get back the same tags. They do not change them to anything
else.

#### Column alignment

To align a column, use the delimiter row of GFM: `:---:` centers a column and
`---:` aligns it to the right.

```markdown
| Service | Errors |
| ------- | -----: |
| auth    |      3 |
```

Confluence has no explicit left alignment. Left is its default. Thus `:---`
publishes the same as `---`, and `read` gives back `---`.

#### Tables that Markdown cannot express

A GFM table cannot express some things that a Confluence table can: column
widths, a layout other than the default, merged cells, a table with no header
row, a header column, or a code block or a heading in a cell. For these, write
the table as raw storage format. markfluence publishes it with no change. See
[Raw Confluence storage format](#raw-confluence-storage-format).

Put each table, row, and cell tag on its own line. Put a blank line before and
after the content of a cell. Then markfluence converts the content of the cell
as Markdown. Without the blank lines, the content is storage format, and
markfluence does not convert it.

```
<table data-layout="center" data-table-width="900">
<colgroup>
<col style="width: 300px;" />
<col style="width: 600px;" />
</colgroup>
<tbody>
<tr>
<th colspan="2">

Q3 results

</th>
</tr>
<tr>
<td rowspan="2" data-highlight-colour="#e3fcef">

**auth** is [up](https://status.example.com)

</td>
<td>

99.9%

</td>
</tr>
<tr>
<td>

99.8%

</td>
</tr>
</tbody>
</table>
```

This is the table markup that has an effect in Confluence:

| markup | effect |
| --- | --- |
| `<th>` cells in the first row | a header row |
| a `<th>` cell first in each row | a header column |
| `colspan` and `rowspan` on a cell | merged cells |
| `data-highlight-colour="#rrggbb"` on a cell | the background color of the cell |
| `style="text-align: center;"` or `right` on a cell, or on a `<p>` in a cell | alignment |
| `valign` on a cell, such as `valign="bottom"` | vertical alignment |
| `<colgroup>` with a `<col style="width: 300px;" />` for each column | column widths |
| `data-layout` on the table: `align-start`, `align-end`, `center`, `default`, `wide`, or `full-width` | the layout of the table |
| `data-table-width` on the table, in pixels | the width of the table |
| `data-table-display-mode="fixed"` on the table | the fixed display mode of the editor |
| `class="numberingColumn"` on the first cell of each row | a numbered column |

Details:

- markfluence does not add `data-layout="align-start"` to a raw table, as it
  does to a GFM table. You control all the attributes.
- If a table has a `<colgroup>` and no `data-layout`, Confluence picks a layout
  from the total width of the columns. Wide columns make the table
  `full-width`. Thus, if you give column widths, also give a `data-layout`.
- Column widths in pixels always work. Column widths in percent work only if
  the table also has `data-table-width`.
- Do not use `<caption>`. Confluence removes the tag and puts its text in a
  paragraph above the table.
- Do not put a table in a table cell. The Confluence editor cannot edit a
  nested table.
- Colors in `style` or `class` do nothing. Use `data-highlight-colour`.

`read` and `export` give back a GFM table when GFM can express the whole
table. Otherwise, they give back a raw table in the form above, with the
content of each cell as Markdown. A paragraph with an alignment stays storage
format, because a Markdown paragraph has no alignment.

A table that markfluence published stays a GFM table after someone edits the
page in Confluence. The Confluence editor adds attributes to every table that
it saves: IDs, a `data-table-width`, and, on a table with the `align-start`
layout that markfluence uses, a `<colgroup>` with the column widths that it
measured. `read` ignores these. The editor writes the same `<colgroup>` when
someone changes a column width by hand. Thus that change is lost when you
publish the file again, and the columns fit their content again. (Column
widths that add up to more than the page give the table a horizontal scroll
bar, so this is often what you want.) A table with any other layout and a
`<colgroup>` comes back as a raw table.

### GitHub alerts

GitHub alerts become Confluence panels in the color that GitHub gives them. The
alerts are `> [!NOTE]`, `[!TIP]`, `[!IMPORTANT]`, `[!WARNING]`, and
`[!CAUTION]`:

| alert | colour | published as |
|---|---|---|
| `NOTE` | blue | `info` macro |
| `TIP` | green | `tip` macro |
| `IMPORTANT` | purple | ADF panel (no macro exists for purple) |
| `WARNING` | orange | `note` macro |
| `CAUTION` | red | `warning` macro |

Each alert maps to one panel, and each panel maps to one alert. Thus `read` and
`export` get back the original
[GFM alert](https://docs.github.com/en/get-started/writing-on-github/getting-started-with-writing-and-formatting-on-github/basic-writing-and-formatting-syntax#alerts).

Example:

```markdown
> [!NOTE]
> This is a note.
```

### Images

`![alt](./path.png)` uploads a local file as an attachment. It can also
reference a remote URL. A missing image, or an image of a type that is not
supported, becomes the text `line N: IMAGE BROKEN: …`. `N` is the line of the
image in the file.

An image path is relative to the Markdown file, as it is when you look at the
file on GitHub. Thus a page in a subdirectory can share an asset directory
above it:

```
docs/                      ← needs a markfluence.yaml here for this to work
  assets/logo.png
  guide/page.md            → ![logo](../assets/logo.png)
```

That layout needs a declared
[documentation root](../README.md#the-documentation-root) at `docs/`. Without
one, the root of each page is its own directory. Then `guide/page.md` cannot
go above itself to `assets/`, because that is out of bounds.

> [!NOTE]
> An image path is a URL, and not a filename. Thus you must percent-encode a
> space or another special character. For a file named `my image.png`, write
> `![shot](assets/my%20image.png)`. GitHub and the preview of your editor use
> the same rule, and they write this form when they make a link for you.
>
> The angle-bracket form `![shot](<assets/my image.png>)` is a different
> spelling of the same image. A bare space (`![shot](assets/my image.png)`) is
> not a valid path. Thus it is not an image at all, and it stays on the page as
> literal text. GitHub and your preview show the same result.
>
> `markfluence read` and `markfluence export` write the encoded form. Thus a
> page round-trips back to Markdown that still renders.

The [documentation root](../README.md#the-documentation-root) bounds every
image. An image that resolves outside it (`../../secrets/x.png`) is not
uploaded. markfluence reports it as
`line N: IMAGE BROKEN: … (outside the documentation root)`. markfluence also
refuses a symlink, also when it resolves inside the root.

A Confluence attachment name cannot contain `/`. Thus markfluence attaches an
image with its **base name**: `assets/logo.png` becomes `logo.png`. The
attachment comment records the path, relative to the root and not to the page.
`markfluence read` and `markfluence export` use that path to put the file back
where it came from. A page one directory down can reference the same file as
`../assets/logo.png`. That is the same attachment, because both paths resolve
to the same path relative to the root.

The name is only the base name. Thus markfluence cannot publish two images in
one file that have the same name, such as `arch/diagram.png` and
`deploy/diagram.png`. An attachment name is unique on a page, so one image
would overwrite the other. markfluence refuses the file and names both paths.
`markfluence check` reports it with no publish. Rename one of the files.

You can put more properties in the title as JSON:

```markdown
![alt](x.png '{"title":"…","width":"100","align":"center"}')
```

* `align` is `left`, `center`, or `right`.
* `width` and `height` are in pixels.

A plain title (`![alt](x.png "tooltip")`) becomes the tooltip of the image.

Examples:

```
![alt text](./path.png)

![alt text](https://example.com/image.png)

![alt text](./path.png "title")

![alt text](./path.png '{"title":"sometitle","width":100}')
```

### Links to other pages

markfluence changes a link to a `.md` file to the Confluence URL of the target
page. The path is relative to the file that has the link, as on GitHub. The
target can be any `.md` file under the documentation root, such as
`../other/page.md`. It changes **heading anchors** to the anchor scheme of
Confluence.

A link destination is a URL, as an image path is. For a sibling whose filename
has a space, write `[see](my%20doc.md)` or `[see](<my doc.md>)`. A bare
`[see](my doc.md)` is not a link at all. The same rule applies to the
fragment, so a non-ASCII heading anchor can be `#caf%C3%A9-section`.
markfluence decodes both before it compares them with files and headings on
disk. Thus either spelling resolves.

For a link to a `.md` file, whether markfluence reports an unresolved link, and
how serious it is, depends on the reason:

* A target that **does not exist at all**, or that **resolves outside the
  documentation root**, is Broken. markfluence replaces the whole link element
  with the literal text `line N: LINK BROKEN: … (not found)` or
  `line N: LINK BROKEN: … (outside the documentation root)`. It does the same
  for a broken image.
* A target that **exists but has no `page_id` yet** gives a Warning
  (`link not resolved: …`). This is the usual state of each page in a tree that
  nobody published yet. The href still renders exactly as you wrote it.

  markfluence treats a **same-page anchor** (`#heading`) as a link to the
  *current* file. Thus it gives this warning too when the current file has no
  `page_id` yet. The warning looks as if the file names itself as missing. It
  does not: that is only this file before its first publish.
* A `#fragment` that **matches no heading** on a target that resolves is also a
  Warning (`anchor not found: …`). The link still works, but it goes to the top
  of the page, and not to the named heading.

An attachment link or an external URL does not resolve here, and markfluence
says nothing about it in both cases. A mention is also a link, but a special
link. See below.

### User mentions

A Confluence user mention round-trips as a usual Markdown link to the profile of
the person, with an `@` at the start of the link text:

```markdown
Ping [@Ada Lovelace](https://home.atlassian.com/people/712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3) about the deploy.
```

`read` and `export` write that form. `create` and `update` publish it back as a
real mention. Know these 3 things:

- **The `@` is what makes it a mention.** A link to the same URL with text that
  does not start with `@` publishes as a plain link. Thus you can link to the
  profile of a person without triggering a user mention.
- **The account id is the only critical part.** On publish, markfluence
  generates the user mention using the account id. On `read` and `export`,
  markfluence will lookup the user display name with the accoun id and
  re-generate the user mention link. Thus when a person changes their name,
  markfluence will create the correct user mention on publish and on the next
  `read` or `export`, generate an updated user mention link.
- **An id that doesn't tie to a real, live account gives a warning, and not an error.**
  Confluence accepts any account id and shows it as `@Unlicensed user`, and it
  does not fail. Thus markfluence looks up the id and tells you. Nothing else
  will.

**A colleague who left keeps their name.** A deactivated account resolves
normally, and Confluence adds the suffix itself. Thus a page after a round trip
shows `[@Mark Reid (Deactivated)](…)`. It records who left, and it does not
lose them.

An id that really does not resolve, such as a typo or a URL that somebody
edited by hand, shows as `[@Unlicensed user](…)`. That is what the page itself
shows. The id stays in the URL, so it is still the easiest thing to correct.

Sometimes markfluence cannot *ask* whether an account exists, for example with
no network or a refused token. Then it leaves the mention exactly as it was,
and does not put a placeholder in it. Otherwise one bad moment during an
export would write `Unlicensed user` over every real name in a tree.

### Comment directives

- `<!-- confluence-toc -->`: markfluence replaces it with the Confluence
  table-of-contents macro.
- `<!-- bg:COLOR -->` at the start of a table cell: markfluence removes it and
  gives the cell a background color. See
  [Cell background colors](#cell-background-colors).

There are no other directives. **Any other HTML comment that you write is
lost** because Confluence removes every comment on write, so the comment never
gets to the stored page (measured; see
[storage-format.md](confluence/storage-format.md#confluence-strips-html-comments-on-write)).

Don't use HTML comments to leave notes on published pages that you will `read`
or `export` in the future.

### Raw Confluence storage format

You can paste Confluence
[storage format](https://confluence.atlassian.com/doc/confluence-storage-format-790796544.html)
markup into your Markdown. This is `<ac:…>` and `<ri:…>` elements, such as any
macro or layout. Copy it from **⋯ → View storage format** on a page.
markfluence writes it with no change. There are two conventions:

- **Put a blank line** between an `ac:` or `ri:` tag and the Markdown that you
  want markfluence to convert. An example is the body of a macro or a layout
  cell. With a blank line, markfluence parses the content as Markdown. Without a
  blank line, markfluence does not convert the content, and passes it through
  as it is.
- **Put the opening tag on its own line**, or close it in the same tag. Then
  markfluence does not put it in a paragraph.

This example is a two-column layout with Markdown in each cell:

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

Storage markup in a fenced code block stays literal. markfluence does not
activate it.

A table uses the same conventions. See
[Tables that Markdown cannot express](#tables-that-markdown-cannot-express).
