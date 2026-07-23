# Go port — step-by-step execution plan

Companion to `008_go-port.md` (the design). This is the **ordered, resumable** build
sequence for **phase 1** (Go port alongside Python). Phase 2 (delete Python) is the
final step.

## Invariants (true at the end of *every* step)

- `go build ./...` compiles and `go test ./...` passes (for whatever is implemented so far).
- The Python tool is **untouched** and still works — you can stop after any step and ship nothing / resume later.
- Each step has a one-command **validation** you can eyeball to confirm we're on track.

Steps are ordered to de-risk early: pure logic first, then the **converter + parity
harness** (the riskiest part) so its correctness is visible from the middle of the
project onward, then the client, then the CLI.

---

## Step 1 — Module scaffold + UI
**Do:** `go.mod` (`module github.com/mozilla/markfluence`, `go 1.25`), `main.go` shim,
`cmd/root.go` (cobra root, `--url`/`--debug`/`--no-color` persistent flags, `Version`
ldflag, `CONFLUENCE_AUTH` documented), `internal/ui/ui.go` (mirror mzcld), `Makefile`
(build/install/lint/vet/fmt/test), and a smoke test (`cmd/root_test.go`) so `go test`
has at least one test to run from day one (a genuine check that the root command is
wired — `Use`/flags present — rather than a pure no-op).
**Goal:** a buildable binary with working help/version and no subcommands yet.
**Validate:** `make build && ./bin/markfluence --help && ./bin/markfluence --version`;
`make test` runs and passes; `make vet` clean.

## Step 2 — `internal/frontmatter` + tests
**Do:** port `extract_frontmatter`, `parse_value`, `_scan_quoted`, quoting, and
`UpdateField`; add the `MarkdownFile` type (`Parse`/`ParseFile`, exported
`Filename`/`Content`/`Frontmatter`/`Body`, accessors `Title()`/`PageID()`/`Space()`/`Parent()`
with `""`-for-unset + `"null"` sentinel rules). Port `tests/test_frontmatter.py` to Go
table tests.
**Goal:** frontmatter parse/quote/round-trip/write-back + accessor normalization match Python.
**Validate:** `go test ./internal/frontmatter/`.

## Step 3 — `internal/pagewidth` (pure) + tests
**Do:** port `declared_width`, normalization, and the width↔property reverse-map (the
non-network parts); property-name constants. Port the pure cases from `tests/test_pagewidth.py`.
**Goal:** page-width validation/normalization matches Python. (Client-coupled `set`/`read`
land in Step 8 with the client.)
**Validate:** `go test ./internal/pagewidth/`.

## Step 4 — Converter skeleton + regression harness + `paritycheck` ⭐
**Do:** `internal/convert` with `ConfluencePage`, the `MdToConfluence(md, baseURL, spaceKey)`
signature, and a **minimal** renderer (goldmark GFM → storage for headings/paragraphs/
lists/emphasis/inline-code/tables, soft-break→space). Copy the regression corpus into
`internal/convert/testdata/regression/`. Add `regression_test.go` (walks cases,
normalizes, exact-matches Go goldens, `-update` flag) and generate initial Go goldens.
Build `tools/paritycheck` + `make parity` + `make regen-regressions`.
**Goal:** the validation harness is live across all 15 cases; the simple cases already
read identical/cosmetic vs Python, the rest show as structural (expected — not built yet).
**Validate:** `go test ./internal/convert/` green; **`make parity`** prints a per-case
identical / cosmetic / structural report. *This report is the on-track signal for Steps 5–7.*

## Step 5 — Converter: code blocks, callouts, TOC, raw-storage shield
**Do:** fenced code → code macro (+CDATA/`]]>` escape, language), the callout parser
extension + our renderer (NOTE→info/…), `<!-- confluence-toc -->` → toc macro, and the
`ac:`/`ri:` sentinel shield round-trip.
**Goal:** the `code-blocks`, `callouts`, `toc`, `raw-storage` cases are structural-clean.
**Validate:** `make parity` — those four cases now cosmetic-only.

## Step 6 — Converter: images
**Do:** `ast.Image` → `<ac:image>`; local supported+existing → `ri:attachment` (stable
`assets/x.png`→`assets_x.png`, collected for upload); remote → `ri:url`;
missing/unsupported → `IMAGE BROKEN:`; title-JSON attrs + invalid-value warnings.
**Goal:** `images-local/remote/broken` and `image-properties` parity-clean; `attachments`/
`broken`/`warnings` match Python **exactly**.
**Validate:** `make parity` on the image cases.

## Step 7 — Converter: links + anchors  ✅ converter done
**Do:** sibling-file scans (`build_docs_page_map`, `build_headings_anchor_map`),
`github_slug`/`confluence_slug` (RE2-safe rewrites), `#frag` and `other.md#frag`
rewriting, sibling `.md` → Confluence URL (incl. the no-space `viewpage.action` form).
**Goal:** `anchor-links`, `internal-doc-links(-null-space)`, and `kitchen-sink` parity-clean.
**Validate:** `make parity` — **all 15 cases cosmetic-only** (converter parity achieved).

## Step 8 — `internal/client` + pagewidth set/read + tests
**Do:** `ConfluenceClient` over `net/http` (basic auth from `CONFLUENCE_AUTH`); port every
method 1:1 (pages v2, users/attachments v1, content properties, `_links.next` pagination,
`set_content_property` retry-once, `sync_attachments` SHA-256 skip/update). Finish
`pagewidth` set/read against the client. `httptest`-based unit tests.
**Goal:** the client speaks the v1/v2 shapes; attachment sync + property idempotency work.
**Validate:** `go test ./internal/client/ ./internal/pagewidth/`.

## Step 9 — Commands wired into the CLI
**Do:** `cmd/update`, `cmd/create`, `cmd/fix`, `cmd/info` (+ their `internal` logic),
registered on the root; `ui` output; multi-file / non-zero-exit / mtime-skip behavior.
**Goal:** a functionally complete CLI matching the Python commands.
**Validate:** each `markfluence <cmd> --help`; then **live e2e** on a scratch Confluence
page (`--url` + `CONFLUENCE_AUTH`): `create` (page_id/space/parent written back), edit +
`update` (mtime-skip, then `--force`), `info`, `fix`, image uploads once/skips on re-run,
page width applies.

## Step 10 — Build/dist polish + phase-1 sign-off
**Do:** `.goreleaser.yaml` (CGO_ENABLED=0, GOWORK=off, darwin+linux×arm64+amd64, tar.gz),
`Formula/markfluence.rb`, `.golangci.yml`, optional pre-commit; `CHANGELOG.md`/`README.md`.
**Goal:** phase 1 complete — Go port fully functional, idiomatic, and releasable.
**Validate:** `make test && make lint && make vet` clean; `goreleaser release --snapshot --clean` builds; `make parity` still all-cosmetic.

---

## Phase 2 (separate, after sign-off) — remove Python
Delete `src/markfluence/`, `pyproject.toml`, `uv.lock`, `justfile`, `.python-version`,
the pytest `tests/` (runners + harness + generator + `tests/regression/`),
`tools/paritycheck`; strip Python entries from `.gitignore`. The Go `testdata/` suite
becomes the sole conformance suite. **Validate:** `go test ./... && make lint`; repo is pure Go.
