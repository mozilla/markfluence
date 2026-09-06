# Plan: normalize frontmatter field order

Make `fix` and `create` leave a file's frontmatter in canonical field order,
reported as `reordered`. Split out of `_plans/032`, which established the
decisions this rests on; read its Decisions section first.

## Why this is separate

032 replaced the hand-rolled frontmatter parser with `goccy/go-yaml` (#130). One
of its decisions was that `UpdateField` becomes **surgical** -- an existing key
keeps its position and only its value node is replaced -- where the old writer
rewrote the whole block in canonical order on every single write.

That is the right default for an edit. These files live in git, and a write
should not churn lines nobody asked it to touch. But it means ordering is no
longer normalized as a side effect, and stable ordering is worth having, so the
two commands that already rewrite frontmatter wholesale do it explicitly.

Kept out of 032 for three reasons. It is a feature rather than a consequence:
nothing about #130 requires it. It carries the only `--json` contract change in
either plan, which is the part a consumer can be broken by. And bundled together
the two came to ~600 lines against ~400 for the parser swap alone, which buried
the thing that actually fixes the bug.

## Current state of the codebase

After 032:

- `frontmatter.UpdateField(content, key, value, comment) (string, error)` is
  surgical: replace in place, or insert before the first key that sorts after
  it. `keyLess` is the canonical comparator, from `fieldOrder`
  (`title, space, parent, page_id`, then the rest alphabetically).
- `frontmatter.Render(fields) string` builds a block from scratch, already in
  canonical order, and cannot fail.
- `cmd/fix`'s `processFile` plans changes, returns `statusConsistent` with **no
  write** when `plannedChanges` is empty, and otherwise applies each `change`
  through `UpdateField`.
- `fixResult` carries `changes []change` where `change` is
  `{field, oldDisplay, newValue}`; `jsonFixResult` mirrors it.
- `cmd/create`'s `writeBackFrontmatter` sets all five persisted fields through
  `UpdateField`.
- `README.md` says `fix` "writes a file only when a field actually changed".

## Decisions

These were all settled while designing 032; the reasoning is repeated here
because this is the plan that implements them.

**`fix` normalizes by default, with no flag.** Adding `--normalize` or a
separate `fmt`-style verb would keep "reconcile with the server" and "tidy the
file" conceptually apart, which is cleaner on paper. It is not worth a flag
nobody would remember to pass: a tidy-ordering feature you have to opt into
leaves the files untidy.

**A reorder-only file is `changed`, not `consistent`.** Otherwise you run `fix`,
are told there is nothing to do, and still have a jumbled file. It also keeps
`--dry-run` a faithful preview, which this codebase protects elsewhere: a
dry-run's per-file output is identical to a real run apart from the banner.

The cost, stated plainly: `fix` now touches files it previously left alone, so
the first run after this lands produces a diff across the tree. `README.md`'s
"writes a file only when a field actually changed" becomes false and is
rewritten -- it describes today's behaviour rather than promising anything, and
it is not in `docs/guarantees.md`, where the load-bearing promises live.

**Reported as a dedicated `reordered: boolean` on `fixResult`,** not a
pseudo-entry in `changes[]`. `changes[].field` is an actual frontmatter key
name everywhere else, built from real fields; a non-field there makes the slot
polymorphic, and a consumer doing `changes | map(.field)` gets a phantom key it
has to know to filter. The existing `"(none)"` sentinel lives in `old`, which is
explicitly a *display* slot (`oldDisplay` in the struct) -- `field` is an
identity slot, which is a different thing. Costs a schema edit; that is what the
schema is versioned for.

No equivalent field on `createResult`: `create --persist` writes all five
fields, so its output is always canonical and there is nothing to report.

**`Normalize` is a no-op when the order already holds.** This is what makes the
boolean mean exactly "keys moved", and it is what keeps the blank-line handling
predictable -- blank lines are dropped only as a consequence of a real reorder,
never as a side effect of some unrelated field being written. A canonical file
keeps its blank lines: this normalizes ordering, it is not a formatter.

**Blank lines are dropped textually, and comments travel with their key.** A
blank line is not a node -- it lives in the preceding value's token origin -- so
reordering carries it to a position that means nothing. Dropping them is a text
filter over the emitted block, which is safe only because 032 refuses a
multi-line scalar: nothing this package emits spans more than one line, so a
blank line in the output is always a real blank line.

Comments travelling is better than the old writer's hoist-every-comment-to-the-
top: a comment about `page_id` belongs next to `page_id`. Not claimed to be
free -- a block-header comment written above a key that is not `title` sinks
with that key. Visible in the diff, and accepted.

**`create --persist` normalizes too.** The minimal-diff argument behind a
surgical `UpdateField` does not apply there: persist rewrites all five fields by
definition, so there is no untouched line left to protect. It also means the
frontmatter markfluence *authors* is always canonical, rather than "canonical
unless it came from a jumbled file and you have not run fix yet".

## Implementation

### `internal/frontmatter`

- `Normalize(content string) (string, bool, error)` -- returns content unchanged
  with `false` when `keyLess` order already holds, and likewise for content with
  no frontmatter block. Otherwise sorts `MappingNode.Values` by `keyLess`, drops
  blank lines, and returns `true`.
- `isCanonical(*ast.MappingNode) bool` -- an adjacent-pairs check, which equals
  global sortedness because the parser rejects duplicate keys.
- `dropBlankLines(string) string` -- the text filter, with the safety argument
  above in its doc comment.

### `cmd/fix`

- `fixResult` gains `reordered bool`; `jsonFixResult` gains
  `Reordered bool \`json:"reordered"\``.
- `processFile` calls `Normalize` on `mf.Content` during planning and stores the
  boolean. The `len(r.changes) == 0` early return becomes
  `len(r.changes) == 0 && !r.reordered`.
- The write path applies each `change` through `UpdateField`, then `Normalize`
  **last**, so a key inserted above lands canonically rather than wherever the
  surgical insert put it.
- Computing `reordered` on pre-change content is stable for two reasons, not
  one: a surgical `UpdateField` never moves an existing key, *and* inserting
  before the first key that sorts after it cannot flip canonicity in either
  direction -- an existing inversion survives the insert, and a canonical
  sequence stays canonical.
- Human output gains one line, `normalized frontmatter field order`, printed
  before the per-field lines.

### `cmd/create`

`writeBackFrontmatter` runs `Normalize` after its five `UpdateField` calls, and
discards the boolean: there is nothing to report.

### `schema/json-output/v1.json`

`fixResult` gains `reordered` in `properties` and in `required`
(`additionalProperties: false` needs both).

## Tests

- **`Normalize`**: reorders a jumbled block and drops its blank lines; no-op on
  a canonical block, *including one that has a blank line* (the case that pins
  "ordering, not formatting"); no-op on content with no block; a full-line
  comment stays attached to its key across a reorder.
- **`cmd/fix`**: a file whose values all match its page but whose fields are
  jumbled is `changed` with `reordered: true`, `changes` empty, and is written
  in canonical order; `--dry-run` reports it without writing; a canonical
  consistent file still reports `consistent` and is not written.
- **`cmd/create`**: persist output is canonical from jumbled input.
- **Schema conformance**: `fixResult` with `reordered`, built through the
  command's own `jsonResult()`.

## Docs

- `README.md` -- the `fix` section: drop "writes a file only when a field
  actually changed", add order normalization and `reordered`.
- `CLAUDE.md` -- the `fix` sentence, and the `internal/frontmatter` bullet's
  entry-point list (`Normalize` becomes the third).

## Out of scope

- **`check` reporting a jumbled file.** Ordering is not a publishability defect,
  and `check` is about what would fail a publish.
- **`update` normalizing.** It never writes back to files, and that stays true.
- **Normalizing anything else about the block** -- intra-line whitespace, blank
  lines in a canonical file, comment placement. `fix` orders fields; it is not
  `gofmt` for frontmatter.
