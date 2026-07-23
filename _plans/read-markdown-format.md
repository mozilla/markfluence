# Plan: `read --format markdown` (storage→markdown converter)

Implements issue #23. Adds a storage-format → markdown converter and makes
`markdown` the default output of `read` (was `storage`, from #22). The converter
is the inverse of `MdToConfluence`.

## Decisions locked (from the interview)

### Correctness target — hybrid

- **Contract:** a faithful, golden-tested inversion of what `MdToConfluence`
  emits.
- **Tolerance:** the parser is total — it never errors on editor-authored storage
  (macros/layouts markfluence never emits); unknown constructs degrade gracefully
  (see "unknown macros").

Not a source-reproducing round-trip in general — several forward transforms are
lossy (below).

### Source representation — storage (XHTML)

Convert from `body-format=storage` (already fetched by `GetPageBodyOrNil`), **not**
ADF. Rationale: storage is exactly what `MdToConfluence` emits, so the converter is
a true inverse, the existing regression corpus doubles as converter-input fixtures,
and the `ac:`/`ri:` shield gives lossless passthrough. (atlassian-cli uses ADF, but
it has no forward converter to invert; our situation is the opposite.)

### Parser — stdlib `encoding/xml`

`encoding/xml` token stream with `decoder.Entity = xml.HTMLEntity` (built-in HTML
named-entity table) and `decoder.Strict = false`. Storage is well-formed XHTML by
Confluence contract; CDATA (code bodies) and entities (`&nbsp;` etc.) are handled
natively. **No new dependency.** (`golang.org/x/net/html` was rejected: an HTML5
parser treats `<![CDATA[…]]>` as a bogus comment ending at the first `>`, which
truncates code containing `>`.)

### Default format — markdown

`read` defaults to `markdown`; `--format storage` gives the raw body. This changes
#22's shipped default (documented in the README).

### Output includes frontmatter

The markdown output is prefixed with YAML frontmatter: `title`, `page_id`, `space`,
`page_width`.

- `title`/`page_id` from the page fetch; `space` via `client.SpaceKeyFromWebUI`.
- `page_width` via an extra `pagewidth.Read` call. **Tolerated:** on error the
  field is omitted (the read still succeeds), matching `info`'s tolerance.
- Frontmatter is assembled in `cmd/read` using `internal/frontmatter` quoting.

### Unknown macros — raw passthrough

Any `ac:structured-macro` markfluence doesn't map (i.e. not code/toc/callout) —
whether bodied (panel, expand, …) or a leaf (status, …) — is emitted as its raw
storage XML. Lossless and round-trips back through `MdToConfluence`'s existing
`ac:`/`ri:` shield.

Column layouts (`ac:layout`/`-section`/`-cell`) are emitted as their raw storage
tags wrapping each cell's content converted to markdown (set off by blank lines),
mirroring how such layouts are authored and republished — so they round-trip too,
while cell content stays readable. (A generic `div` still flattens to its
content.)

Superseded an earlier "recurse the rich-text-body of bodied macros" design: that
dropped macro parameters that carry content (e.g. an `expand` macro's title), so
passthrough is used uniformly instead.

## Construct mapping (inverse of `MdToConfluence`)

- **Blocks:** `h1`–`h6` → `#`…; `p`; `ul`/`ol`/`li` (nested); `blockquote` → `>`;
  `table`/`thead`/`tbody`/`tr`/`th`/`td` → GFM table; `<br/>` → hard break.
- **Inline:** `strong` → `**`, `em` → `*`, `code` → `` ` ``, `del` → `~~`,
  `<a href>` → `[text](href)`.
- **Code macro** (`ac:structured-macro ac:name="code"`) → fenced code block using
  the `language` parameter; CDATA body verbatim.
- **Callout macros** → `> [!TYPE]`. Canonical inverse (forward map is many-to-one):
  `info→NOTE`, `tip→TIP`, `note→IMPORTANT`, `warning→WARNING`. CAUTION is
  unrecoverable (folds into WARNING) — accepted loss.
- **`ac:image`** → `![alt](src …)`. `src` = `ri:attachment` filename or `ri:url`
  value. `ac:title`/`width`/`height`/`align` reconstructed as a plain title
  (`"title"`) or the JSON-title form (inverting `parseImageTitle`). Flattened
  attachment names and lost original paths are accepted losses.
- **toc macro** → the `<!-- confluence-toc -->` token (round-trip symmetry).

### Accepted losses

- The `<!-- markfluence-version -->` substitution (bare version string, no marker).
- Doc-links return as absolute Confluence URLs (can't reconstruct `./sibling.md`
  without the local sibling set).
- CAUTION callouts; original image paths; attachment bytes (not downloaded).

## `cmd/read` wiring

- `--format` accepts `storage` and `markdown`; **default `markdown`**.
- markdown path: fetch storage → `convert.StorageToMarkdown` → prepend frontmatter
  → stdout.
- `storage` path, `page <id> not found`, and the bodyless-content error are
  unchanged.

## Package layout

- `internal/convert/storage_to_md.go` (+ helpers) — exported
  `StorageToMarkdown(storage string) (string, error)`, reusing the callout map and
  slug helpers in the package. Add an inverse callout map.

## Testing

- **New golden suite** `internal/convert/testdata/storage2md/<case>/`:
  `input.storage` → generated `output.md`, with a `-update` flag mirroring the
  existing regression harness. Cases cover every mapped construct plus
  editor-authored macros (panel/status/expand/layout) to exercise the tolerant
  path.
- **Forward-corpus pass:** feed each existing `regression/*/test.output` `html`
  through `StorageToMarkdown` (exercises real emitted storage).
- **Round-trip sanity:** a few `md → MdToConfluence → StorageToMarkdown → md`
  checks.
- `cmd/read`: frontmatter-assembly and `--format` validation tests.
- `make test && make lint && make vet` before done.

## Docs

- README `read` section: markdown is now the default; document `--format
  storage|markdown` and the frontmatter output; note best-effort/lossy conversion.
