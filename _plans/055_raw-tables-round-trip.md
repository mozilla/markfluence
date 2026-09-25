# 055: raw storage tables round-trip

Answers #55. A table that Markdown cannot express can be written as raw
storage, and publishing already passes it through intact. But `read` and
`export` turn every table into a GFM pipe table and drop what does not fit,
so read → edit → update silently strips a hand-built table. This plan makes
`read` write such a table as raw storage, and documents the escape hatch.

## What it looks like

Each example is a table on a page and what `read` writes for it after this
plan. Storage is abbreviated to the parts that decide.

**1. A table the editor saved, nothing configured: a pipe table.** The
editor's `data-table-width`, `ac:local-id` and default display mode are
ignored (D2).

```
<table data-layout="align-start" data-table-width="1110" ac:local-id="…">
<tbody>
<tr><th ac:local-id="…"><p>Service</p></th><th><p>Owner</p></th></tr>
<tr><td><p>auth</p></td><td><p>SRE</p></td></tr>
</tbody></table>
```

```markdown
| Service | Owner |
| --- | --- |
| auth | SRE |
```

**2. Colours and a column alignment: still a pipe table**, as today.

```
<tr><th><p>Service</p></th><th><p style="text-align: right;">Errors</p></th></tr>
<tr><td data-highlight-colour="#e3fcef"><p>auth</p></td>
    <td><p style="text-align: right;">3</p></td></tr>
```

```markdown
| Service | Errors |
| --- | ---: |
| <!-- bg:light-green --> auth | 3 |
```

**3. A layout and column widths: raw.** The table, row and cell tags are
storage; each cell's body is Markdown between blank lines (D4). A colour on a
raw cell stays the `data-highlight-colour` attribute.

```
<table data-layout="center" data-table-width="900">
<colgroup>
<col style="width: 300.0px;" />
<col style="width: 600.0px;" />
</colgroup>
<tbody>
<tr>
<th>

Service

</th>
<th>

Owner

</th>
</tr>
<tr>
<td data-highlight-colour="#e3fcef">

auth

</td>
<td>

[SRE runbooks](runbooks.md)

</td>
</tr>
</tbody>
</table>
```

**4. Merged cells: raw.** `rowspan` and `colspan` stay on the cell.

```
<table>
<tbody>
<tr>
<th colspan="2">

Q3 results

</th>
</tr>
<tr>
<td rowspan="2">

**auth**

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

**5. No header row, or a header column: raw.** GFM needs a header row across
the top; today `read` promotes the first row, and the next publish turns its
cells into headers. Written the same way as 3 and 4, with `<td>` or a leading
`<th>` in each row as the page has them.

**6. Block content in a cell: raw, and the block stays Markdown.**

````
<tr>
<td>

Restart with:

```bash
systemctl restart auth
```

</td>
</tr>
````

**7. An aligned paragraph inside a raw cell** is written as a raw `<p>` on its
own line, since a Markdown paragraph has no alignment (D4). Its text is then
storage, not Markdown. *Amended: when every paragraph in the cell shares the
alignment, it moves to the cell instead (see the end of this plan).*

```
<td>

<p style="text-align: center;">centred</p>

</td>
```

**8. A column whose cells disagree on alignment: raw**, where today the most
common alignment wins and the other cells are republished with it.

## What Confluence does with table markup

**Verified 2026-09-25** by writing one table per hypothesis to a scratch page
in the personal space, reading back storage and ADF (what the editor and
renderer use, docs/confluence/storage-format.md), then trashing the page.
Rows marked *known* repeat the 2026-08-07 findings already in that document.

| written | stored | takes effect (ADF) |
|---|---|---|
| `<th>` in the first row | kept | header row |
| `<th>` first in every row | kept | header column |
| two header rows in `<thead>` | kept | two header rows |
| `<tfoot>` | kept | an ordinary last row; no footer |
| `<caption>` | **tag dropped, its text left loose** | the caption text becomes a paragraph above the table |
| `colspan`, `rowspan` on `<th>`/`<td>` | kept | yes |
| `data-highlight-colour` on a cell (*known*) | kept | cell background |
| `style="background-color: …"` on a cell | kept, as `rgb()` | **no** |
| `class="highlight-blue"` on a cell | kept | no |
| `text-align` on a cell's `<p>` or the cell (*known*) | kept | paragraph alignment |
| `valign="bottom"` on a cell | kept | **yes**, cell `valign` |
| `style="vertical-align: top;"` on a cell | kept | no |
| `scope="col"` on a `<th>` | kept | no |
| `data-colwidth` on a cell | **dropped** | no |
| `<colgroup>` of px widths (*known*) | kept | column widths; with no `data-layout`, a layout picked by total width |
| `data-layout` `default`, `align-start`, `center`, `wide`, `full-width`, `align-end` (*known*, plus `default`) | kept | the layout |
| `data-table-width` (*known*) | kept | table width |
| `data-table-display-mode="fixed"` | kept | `displayMode: fixed` |
| `data-number-column="true"` | **dropped** | no |
| `class="numberingColumn"` on each row's first cell | kept | **numbered column**, with those cells removed from ADF |
| `class`, `border`, `width`, `style="width: …"` on `<table>` | kept | no (a `style` width induced `layout: default`) |
| a table inside a cell | kept | **not a table**: an uneditable `nested-table` migration extension ("A table in a table cell can't be created or edited in the new editor") |

So the vocabulary that works is: header rows and columns, `colspan`/`rowspan`,
`data-highlight-colour`, paragraph or cell `text-align`, `valign`, a px (or,
with `data-table-width`, percentage) `<colgroup>`, `data-layout`,
`data-table-width`, `data-table-display-mode`, and a numbered column spelled
as `numberingColumn` cells. Everything else is stored and ignored, dropped,
or -- `<caption>` and nested tables -- actively harmful.

## What was checked

**Verified 2026-09-25**, offline with `check --show-html` and
`StorageToMarkdown`, and read-only against the live instance.

- **Publishing works today.** A raw `<table>` carrying `data-layout="center"`,
  `data-table-width`, a `<colgroup>`, `rowspan`, `data-highlight-colour`, a
  `<p style="text-align: right;">`, and a blank line inside comes out intact.
  Markdown between blank lines inside a `<td>` is converted; Markdown tight
  against the tags stays literal. markfluence stamps no
  `data-layout="align-start"` on a raw table.
- **So does the cell form this plan writes** (D4): table, row and cell tags on
  their own lines, each cell's body as Markdown between blank lines. A cell
  holding bold text, a link, an image and a list published as the storage
  the Markdown describes.
- **`read` destroys the same table.** It comes back as a pipe table with the
  layout, width, colgroup and `rowspan` gone, and a ragged last row
  (`| x |` in a two-column table).
- **Already done by #48 and #54:** `data-highlight-colour` reads back as a
  `<!-- bg:NAME -->` marker, and paragraph or cell `text-align` as the
  delimiter row.
- **The editor stamps attributes on every table it saves.** Page 2913502220
  (the issue's example, three tables made in the editor) carries
  `data-table-width` (1110 on two tables, 778 on the third) on every table,
  `ac:local-id` on every table, row and cell, and
  `data-table-display-mode="default"` on one. No `<colgroup>`.

**Not verified:** whether an editor save adds `data-table-width` to a table
markfluence published. It does not matter under D2, which ignores the
attribute either way.

## Decisions

**D1. A table is written as a pipe table only when Markdown expresses all of
it.** Otherwise it is written as a raw table (D4). A table that is *nearly*
expressible -- one `rowspan` -- is raw too: degrading one cell is still loss,
and the raw form is exact.

**D2. What a pipe table may carry.** These are ignored when deciding, because
the editor writes them on tables nobody configured, and treating them as
intent would make every table the editor has saved come back as HTML:

- `ac:local-id` anywhere (already dropped on the way back);
- `data-table-width`, whatever its value;
- `data-table-display-mode="default"`;
- `data-layout="align-start"`, or no `data-layout` at all;
- `rowspan="1"` and `colspan="1"`.

What a pipe table expresses, beyond the table itself:

- `data-highlight-colour` on a cell (a `bg:` marker);
- a column alignment (the delimiter row), from a cell's `style` or its
  paragraphs' `text-align`, as today.

Anything else on `<table>`, `<tr>`, `<th>`, `<td>` or a cell's `<p>` makes the
table raw. The list is an allowlist, so an attribute nobody has seen yet is
kept rather than dropped; it gets honed as real pages turn up noise.

The cost of ignoring `data-table-width`: a table someone resized by hand in the
editor loses the resize on the next publish of its read-back file. The
alternative sends every table the editor has touched back as HTML.

**D3. What makes a table raw.**

- any other `data-layout` (`center`, `wide`, `full-width`, `align-end`...);
- a `<colgroup>`;
- `rowspan` or `colspan` greater than 1;
- **no header row**, or a header anywhere else: the first row must be all
  `<th>` and no other row may hold one. GFM has no table without a header
  row, and today `read` promotes the first row, so republishing turns its
  `<td>`s into `<th>`s. A header *column* is the same case;
- a row with a different number of cells than the header (today it becomes a
  ragged row that GFM pads or truncates);
- **block content in a cell** other than paragraphs and `<ul>`/`<ol>`: a
  heading, a code block or any other block macro, a nested table, a
  blockquote, `<pre>`, `<hr>`, a layout. An image or a status macro inside a
  paragraph is inline and stays expressible;
- **cells in one column that do not agree on alignment**, counting no
  alignment, `left` and `start` as one value (Confluence has no explicit
  left, docs/confluence/storage-format.md). Today the most common alignment
  wins and the rest are dropped, which republishes an aligned column over
  cells that were not.

The last two are consequences of D1 rather than separate choices; they are
listed because they change today's output (see Tests).

**D4. The raw form keeps each cell's body as Markdown.** Table, section, row
and cell tags are raw storage, one per line; each `<th>`/`<td>`'s body is
converted to Markdown and set off by blank lines -- the convention `read`
already uses for `ac:layout-cell` and a macro's rich-text body, and the one
publishing already accepts:

```
<table data-layout="center">
<tbody>
<tr>
<td rowspan="2">

**Owner** is [here](https://x)

</td>
<td>

x

</td>
</tr>
</tbody>
</table>
```

Longer than verbatim storage, but the text stays editable, and a page link,
an image or a mention keeps its Markdown form instead of becoming an
`<ac:link>` to edit by hand. An empty cell is `<td></td>`. A `<colgroup>` and
its `<col />`s are serialized raw on their own lines. A nested table inside a
cell goes through the same decision.

Attributes are written as the page has them, `ac:local-id` dropped as
everywhere else. A raw table keeps its `data-table-width`: D2 ignores it only
for the decision.

A paragraph inside a raw cell that carries an attribute Markdown cannot hold
-- in practice `text-align` -- is serialized raw on its own line (example 7).
Its text is storage rather than Markdown, which costs editability in that one
paragraph; rendering it as a plain paragraph would drop the alignment, which
is the loss this plan exists to stop. Scoped to raw table cells: the same gap
exists for every aligned paragraph `read` meets (top level, layout cells,
macro bodies all read back unaligned), and closing it everywhere is a separate
change (Not in scope).

**D5. The Markdown side is a fixed point.** read → publish → read gives the
same Markdown after one cycle, as for layouts. Publishing a raw table writes
it back with the same attributes, so the second read makes the same decision.

**D6. No warning when a table falls back to raw.** Deferred until someone asks.

## Implementation

**`internal/convert/storage_to_md.go`:**

- `renderTable` asks a new `tableExpressible(n)` first and hands the table to
  `renderRawBlock` when it answers no.
- `tableExpressible` checks D2 and D3 in one walk: the allowlisted attributes,
  the header shape, the row widths, each cell's children, and each column's
  alignments.
- `isContentContainer` gains `th` and `td`, so `renderRawBlock` writes a
  cell's body as Markdown. They only occur inside a table, and a table only
  reaches `renderRawBlock` through this path.
- `columnSeparators` loses its vote: once D3 guarantees a column agrees, it
  reads the column's one alignment. The majority logic and its comment go.
- `renderTable`'s doc comment ("Alignment is not preserved") is wrong since
  #48 and is rewritten.

Nothing changes on the publish side.

## Documentation

- **`docs/markdown-file.md`**, under Tables: a "Column alignment" subsection
  (missing since #48: `:---:` and `---:`, no explicit left), and a "Tables
  Markdown cannot express" subsection: write the table as raw storage, with
  the D4 cell convention and an example. Two gotchas from
  docs/confluence/storage-format.md go with it: a raw table gets no
  `data-layout` from markfluence, and a `<colgroup>` with no `data-layout`
  makes Confluence pick one by total width, so a table with column widths
  must name its layout; percentage widths need `data-table-width`, px widths
  do not. It also says what `read` does: which tables come back as pipe
  tables, and that the rest come back raw. The "Raw Confluence storage
  format" section links to it. It lists the table markup that works (the
  vocabulary under "What Confluence does with table markup"), with
  examples 3, 4 and 6 above, and warns against the three harmful shapes:
  `<caption>` (its text escapes above the table), a nested table (the editor
  cannot edit it), and `style` colours or `class` names (stored and ignored;
  use `data-highlight-colour`).
- **`docs/confluence/storage-format.md`**: the probe table above as a new
  "Table markup" section with its date, `default` added to the layout list,
  the editor's table attributes (page 2913502220), and a line on D2's reading
  of them.
- **CLAUDE.md**: the converter bullet's `storage_to_md.go` table sentences
  gain the fallback rule and the ignored attributes.

## Tests

- **`storage2md` cases**, one per trigger, each read back as a raw table: a
  non-default `data-layout`, a `<colgroup>`, `rowspan`, `colspan`, no header
  row, a header column, a short row, a heading in a cell, a code macro in a
  cell, a nested table, a column whose alignments disagree, a numbered
  column (`numberingColumn` cells), `valign`, `data-table-display-mode="fixed"`,
  and an aligned paragraph inside a raw cell (example 7). Plus one
  editor-shaped table (D2's attributes, a colour, an agreeing alignment) that
  stays a pipe table. Each example under "What it looks like" is one of
  these cases, so the plan's examples are the goldens.
- **`TestRoundTripPassthrough`** gains `raw-table`: read → publish → read is
  stable, and the storage keeps the attributes. The existing
  `TestRoundTripMarkdownIsAFixedPoint` covers every new case for D5.
- **Forward:** `testdata/regression/raw-storage/main.md` gains a raw table
  with blank lines inside, Markdown in a cell, and one cell tight against its
  tags, so the publish path's behaviour is pinned.
- **Changed output:** `storage2md/table-alignment` has two columns whose
  cells disagree ("Cell form": right, none; "ADF names": start, end), written
  to test the vote. It is split: the recovery forms (cell style, paragraph
  style, `end`) in columns that agree, which stay a pipe table, and a
  disagreeing column in its own case, which goes raw.
  `TestRoundTripTableAlignment` is checked against the same split.

## Not in scope

- **A warning on fallback** (D6).
- **An empty-header convention.** A table with no header row could be read
  as a pipe table with an empty header row, if publishing learned to treat
  one as "no header". It changes what existing files mean (an empty header
  row publishes empty `<th>`s today) and GitHub's preview shows a blank
  header. Raw is exact; this waits for evidence that headerless tables are
  common enough to matter.
- **A `check` lint** for a raw `<colgroup>` with no `data-layout`, a
  `<caption>`, or a nested table. The docs warn about all three.
- **Aligned paragraphs outside raw table cells** (D4). `read` drops the
  alignment of a paragraph at the top level, in a layout cell or in a macro
  body today, and always has.
- **Honouring a hand resize** (`data-table-width` on a pipe table). It would
  need a way to write a width in Markdown.

## Amended during implementation

- **D2's premise was measured wrong, then corrected.** Page 2913502220 was
  not typical, and a markfluence table saved in the browser editor (page
  3109814418) came back with a bare `local-id` on every paragraph and a
  `<colgroup>` of measured pixel widths summing to `data-table-width`, so it
  read back raw. Both are now ignored: `local-id` everywhere, the
  `<colgroup>` only on an `align-start` table. A resize writes the same shape
  (only its sum stops matching `data-table-width`, seen once), so a resized
  column is lost on the next publish -- chosen over trusting the sum.
  `data-layout="default"` and `full-width`, common on tables markfluence did
  not write (a 261-table sample), stay reasons to go raw: they are visible
  layouts.
- **From the code review, each reproduced first:** loose inline content in a
  raw cell is one paragraph, not a block per element; an empty paragraph in
  a raw cell stays `<p />`; loose text in a table wrapper is kept; a
  `text-align` of an unknown value keeps a table raw (`justify` has no effect
  and is accepted); a paragraph's own alignment beats its cell's; an empty
  cell does not vote on its column's alignment; the tags allowed loose in a
  pipe cell are only those `read` can render. Inline tags `read` cannot
  render inside a paragraph (`<time>`, `<u>`...) are dropped everywhere, not
  only in tables, and are left alone. Old-editor markup (`class="wrapped"`,
  empty `<col />`s) still keeps a table raw: in the sample, ignoring it would
  change 3 of 109 old tables.
- **From the second review, each reproduced first:** a cell's shared
  paragraph alignment moves onto the cell as `style="text-align: …"`, so its
  paragraphs stay Markdown -- an aligned paragraph kept as storage kept its
  image as `<ac:image>`, which is never uploaded when an exported tree is
  published to new pages; a paragraph stays storage only when a cell's
  paragraphs disagree. A nested table in a raw cell stays raw. A paragraph
  that renders to nothing (`<br />`, `<time>`) stays storage. Two text-align
  declarations keep a table raw. A list in an aligned column keeps a table
  raw. The bare `local-id` is dropped everywhere. An element holding loose
  text is written whole. Cell text that looks like a Markdown block (`1. `,
  `# `) becomes structure on publish, in every paragraph `read` writes, not
  only in tables: a separate issue. A table with no `data-layout` stays
  ignorable, as D2 decided.
