# Plan: `read` subcommand

Add a `read` subcommand that fetches a single Confluence page and prints its body
to stdout. Modeled on confluence-cli's `read` command
(https://github.com/pchuri/confluence-cli#read-a-page).

`read` is **read-only**: it never writes to Confluence or to local files. Output
goes to stdout so it composes with shell redirection (`markfluence read 123 > page.xml`).

## The format reality (why the default is `storage`, not markdown)

The issue and confluence-cli both frame markdown as the default output. That is
**not achievable from the API**: the Confluence Cloud v2 API has no markdown
representation. `GET /wiki/api/v2/pages/{id}?body-format=…` accepts `storage`,
`atlas_doc_format`, `view`, `export_view`, `styled_view`, `anonymous_export_view`,
`editor`, `editor2`, `wiki`, `plain`, `dynamic` — but never `markdown`. (`wiki` is
the legacy Confluence wiki markup, a different language, not markdown.)

Every tool that emits markdown (confluence-cli included) fetches storage/HTML and
converts **client-side**. For markfluence that means writing a storage→markdown
converter that inverts the entire `internal/convert` package — code macros,
GitHub-alert callouts, images/attachments, link + anchor rewriting, tables, the TOC
macro. That is a substantial component with its own golden-file suite, not a flag.

**Decision:** ship storage-first now; defer markdown to a follow-up (see below).
The v1 default and only value is `storage` — the same format `MdToConfluence`
emits, so `read` complements it at the storage layer today, and at the markdown
layer once the converter lands.

## Scope

**Build now:**

- `cmd/read/read.go` exporting `Cmd`, registered in `cmd/root.go`.
- A page-locator that accepts a **numeric id** or a **Confluence URL** (both the
  modern `/wiki/.../pages/{id}/...` path form and the legacy `?pageId={id}` query
  form). No `.md` file support — that would not make sense for `read`.
- A `--format` flag that currently accepts only `storage` (its default). The flag
  exists now for forward-compat; any other value is rejected with a message naming
  the supported value(s).
- Client support for fetching a page body: a `Body` field on `client.Page` and a
  new method that GETs with `?body-format=storage`.
- Bodyless-content detection (folders / unsupported types) → clear error.

**Deferred (follow-up, not filed yet):**

- `--format markdown` — the storage→markdown converter. Its own project, its own
  regression suite, inverting `internal/convert`. This is the "complement to
  `MdToConfluence`" in the fullest sense.
- `--format view` / `text` — rendered HTML and plain text. v2 support for these is
  shaky (a v2 gap on `export_view` is documented), so they'd likely pull in a v1
  call; and `text` needs client-side tag-stripping. Some of that work is throwaway
  once the markdown converter exists.
- `--title` / `--space` lookup via `SearchPagesByTitle` — deferred; must handle
  zero/multiple matches. Straightforward to add later.

## Decisions locked (from the interview)

### Input — a single argument: id or URL

`read ARG` takes exactly one argument. Resolution order:

1. `ARG.isDigits()` → literal numeric page id.
2. else parse as a URL and extract the id from **either**:
   - the path form `/wiki/spaces/<SPACE>/pages/<id>/<slug>` (also
     `/wiki/spaces/<SPACE>/pages/<id>` with no slug), or
   - a `pageId` query parameter (legacy `viewpage.action?pageId=<id>` URLs).
3. else → error (`<ARG> is not a numeric page id or a Confluence page URL`).

The id-extraction is a pure function (`parsePageID(arg string) (string, error)`),
table-unit-tested independently of the network.

### Output — raw storage to stdout

The storage XHTML is printed **as returned**, unmodified (no pretty-printing / reindent),
with a single trailing newline. This matches confluence-cli and keeps the output a
faithful copy of what the server stores.

### `--format` — flag present, `storage`-only for now

- Default: `storage`.
- Validation: any value other than `storage` → error listing the supported
  value(s), e.g. `unsupported --format "html" (supported: storage)`.
- Rationale for shipping the flag with one value: forward-compat with the deferred
  markdown/view/text values and confluence-cli parity, so adding them later is
  purely additive.

### Client layer — a body-fetching path distinct from the metadata path

- Add a nested `Body` field to `client.Page`:

  ```go
  type Page struct {
      // …existing fields…
      Body Body `json:"body"`
  }
  type Body struct {
      Storage BodyRepresentation `json:"storage"`
  }
  type BodyRepresentation struct {
      Value          string `json:"value"`
      Representation string `json:"representation"`
  }
  ```

- Add a new method (e.g. `GetPageBodyOrNil(pageID string) (*Page, error)`) that
  GETs `/wiki/api/v2/pages/{id}?body-format=storage` and returns `nil, nil` on 404
  (mirroring `GetPageOrNil`). The existing `GetPage`/`GetPageOrNil` stay bodyless so
  `info` and friends don't over-fetch.

### Bodyless content — explicit error

After a successful fetch, if `page.Body.Storage.Value == ""`, exit non-zero with a
confluence-cli-style message:

```
page <id> has no readable body (it may be a folder or an unsupported content type)
```

### Errors — consistent with `info`

- Arg neither numeric nor a parseable page URL → clear error.
- Resolved id not found / no access (404) → clean `page <id> not found` (via the
  OrNil path returning nil), **not** a raw `HTTPError`.
- All failures: `ui.Error(...)` + return `ui.ErrSilent`, exit non-zero.

## Command shape

```
markfluence read ARG [--format storage]

ARG is a numeric page id or a Confluence page URL.
```

Registered in `cmd/root.go` alongside `update`/`create`/`fix`/`info`. Inherits the
`--url`/`--username`/`--debug`/`--no-color` persistent flags; resolves the client
via `client.Resolve` like the other commands.

## Testing

- `cmd/read`: table-driven unit tests for `parsePageID` (bare id; modern path URL
  with and without slug; legacy `?pageId=`; junk → error) and for `--format`
  validation.
- `internal/client`: extend the existing `httptest`-based client tests to cover
  `GetPageBodyOrNil` — a 200 with a storage body, a 200 with an empty body, and a
  404 → `nil, nil`.
- `make test && make lint && make vet` before done.

## Docs

- Add `read` to the README command list with the id/URL + `--format storage` usage
  and a note that markdown output is a planned follow-up.
