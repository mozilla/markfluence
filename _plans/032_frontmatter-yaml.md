# Plan: frontmatter is real YAML

Replace the hand-rolled frontmatter parser with `goccy/go-yaml`, so the
frontmatter markfluence writes is valid YAML. Closes #130. Adds **C2**
(`frontmatter-is-valid-yaml`) to `docs/guarantees.md`.

Field-order normalization (`Normalize`, `fix --json`'s `reordered`, and
`create --persist` normalizing) was designed here and then **split out** into
`_plans/033`: it is a separate feature that happens to need a surgical
`UpdateField`, it is the only `--json` contract change, and keeping it here made
a ~400-line change into a ~600-line one. The decisions below that concern it are
kept for the record and marked, since 033 builds on them.

## Current state of the codebase

`internal/frontmatter` is a 303-line hand-rolled parser for a flat `key: value`
block. It is self-consistent and wrong.

**The bug.** A page title containing `: ` is written unquoted:

```
---
title: Deploy Runbook: Part 2
page_id: 123
---
```

`Extract` splits each line at the *first* `:` only, so that reads back as
`Deploy Runbook: Part 2` for us. Every real YAML parser rejects it —
`mapping value is not allowed in this context`. VSCode's YAML extension is what
surfaced it.

**Why it happens.** `renderValue` (`frontmatter.go:135`) decides whether to
quote by asking *"would our own `ParseValue` round-trip this?"*, not *"is this
valid YAML?"*. A colon round-trips through our parser, so it is never quoted.
The current output is pinned by a test — `frontmatter_test.go:80`,
`{"colon value bare", "title", "a: b", "", "title: a: b"}` — so this is a
deliberate decision being reversed, not an oversight.

**The colon is one member of a class.** The same predicate emits all of these
bare (measured, not guessed):

| written | a real YAML parser sees |
|---|---|
| `title: a: b` | parse error |
| `title: true` / `no` | boolean |
| `title: 123` / `1.5` | number |
| `title: null` / `~` | null |
| `title: [draft] Foo` / `{a}` | flow collection |
| `title: @home` `*star` `&anchor` `%pct` `- dash` `\|pipe` `>gt` `!bang` | reserved or misparsed indicators |

`parent` has the same exposure, since it can hold a relative `.md` path.

**What already exists and constrains the change:**

- `MarkdownFile.Frontmatter` is a `map[string]string`, read directly by
  `pagewidth.Declared`, `fix.locatePage`, `fix.plannedChanges`,
  `create.go:570`, `update.go:323`. Keeping that shape keeps the blast radius
  inside the package.
- `coordinate()` maps a literal `"null"` to `""`, which is how `parent: null`
  and `page_id: null` mean "unset". It does **not** map `~` or `Null`.
- `Title()` deliberately does *not* collapse `"null"` — "a literal `null` is a
  legal title", pinned by `frontmatter_test.go:171`.
- `UpdateField(content, key, value, comment)` is the single write path:
  `create.go:451-455`, `fix.go:144`, and `pagedoc.RenderFrontmatter`
  (`pagedoc.go:284-298`), which chains five calls starting from `""`.
- The `comment` parameter is **write-only**. `create` emits
  `parent: 1234  # original.md` as a human breadcrumb; nothing reads it back
  (`frontmatter_test.go:107`).
- `fieldOrder` = `title, space, parent, page_id`, then the rest alphabetically.
  `UpdateField` rewrites the whole block in that order on every write.
- `ErrUnterminatedFrontmatter` is a lexical check on the `---` delimiters
  (`frontmatter.go:261-264`), before any value parsing.
- `Extract` and `ParseValue` are exported but called only by this package's own
  tests.
- `check.go:120` routes any `ParseFile` error to `r.fail(err, CodeValidation)`.
- `linkindex.Build` skips a file whose frontmatter fails to parse.
- `create.go:554` **already errors** on an empty title. `update.go:174-175`
  deliberately falls back to the live page title instead.
- `internal/convert`'s `lineOffset` already exists because goldmark parses the
  body rather than the file, so reported positions need correcting. Frontmatter
  positions have the same problem and should be reported the same way.

## What was verified (2026-09-06)

Probed `github.com/goccy/go-yaml v1.19.2` in a throwaway module. Everything
below is measured output.

1. **Quoting is correct for the whole hazard set.** `title: "a: b"`, `"true"`,
   `"123"`, `"null"`, `"~"`, `"[draft] Foo"`, `"@home"`, `"*star"`, `"- dash"`,
   `"%pct"`, `"Detect # Verify"` — and `Plain Title` / `Bob's Runbook` stay
   bare. Quotes only when YAML requires it; prefers **double**.
2. **No line wrapping.** A 309-character plain scalar emits as one line.
3. **Comments round-trip.** `parser.ParseBytes(src, parser.ParseComments)` then
   `MappingNode.String()` reproduces a full-line comment and a trailing
   `# foo.md`. One space before the `#`, where we currently write two.
4. **Reads must go through the token, not `String()`.**
   `v.Value.GetToken().Value` gives `"4"` for `parent: 4  # foo.md`;
   `v.Value.String()` gives `"4 # foo.md"`.
5. **Node type, not token text, is what identifies a null.** This corrects an
   earlier draft of this plan, which claimed token values reproduce today's map
   exactly. They do for `page_id: 123` → `"123"` and `space: ~abc` → `"~abc"`,
   but not for nulls:

   ```
   title:        -> NullNode tok="null"    (today: "")
   title: null   -> NullNode tok="null"
   title: ~      -> NullNode tok="~"       (coordinate() never mapped this)
   title: Null   -> NullNode tok="Null"    (nor this)
   ```

   So a blank `title:` would read as the string `"null"` and `create` would
   publish a page named `null`. The `~`/`Null` gap exists in the current code
   too: `parent: ~` reads today as if it were a page id.
6. **`GetToken().Value` returns the indicator for four node kinds**, so
   `scalarValue` must be a whitelist, not a Sequence/Mapping blacklist:

   ```
   title: &a foo      -> AnchorNode  tok="&"
   parent: *a         -> AliasNode   tok="*"
   page_id: !!str 123 -> TagNode     tok="!!str"
   title: |\n  lit    -> LiteralNode tok="|"
   ```
7. **Stricter than us in four places we want.** A space before the colon
   (`key :v`) fails with `non-map value is specified`; duplicate keys error
   (`mapping key "title" already defined at [1:1]`) where we silently take the
   last; tab indentation errors; a nested value parses as a
   `SequenceNode`/`MappingNode` where we silently produced `""`.
8. **An empty block is not an error, but has no mapping node.**

   ```
   ""                 -> docs=1 body=nil
   "\n"               -> docs=1 body=nil
   "# only a comment" -> docs=1 body=*ast.CommentGroupNode
   ```

   Today `---\n\n---` parses to an empty map, so `parseBlock` must treat both
   as an empty mapping rather than failing.
9. **Editing the AST works, with two traps.** A hand-built
   `MappingValueNode` whose `token.Position.Column` is 0 **panics** in
   `String()` (`ast.go:1438`, `strings: negative Repeat count`); `Column >= 1`
   is required. And a comment set on the `MappingValueNode` renders as a
   full-line head comment; it must go on the *value* node to render as
   `parent: "42" # foo.md`.
10. **Reordering carries comments *and* blank lines with their key**, producing
    stray blanks in meaningless positions. Blank lines are not nodes — they
    live in the preceding value token's `Origin` — so removing them is a
    textual filter over the emitted block, not an AST operation.
11. **goccy's default emission is not round-trip-safe for four shapes.** Probed
    ~80 strings; these are the ones that fail, and the failure modes differ:

    ```
    "a\tb"  -> title: a<TAB>b  -> reads back "ab"        (silent loss)
    "? q"    -> title: ? q      -> goccy REFUSES its own output
    ".inf"   -> title: .inf     -> InfinityNode, not a string
    ".nan"   -> title: .nan     -> NanNode, not a string
    ```

    The last two are why the verify step compares the re-read **node kind** and
    not just its text: `scalarValue` flattens every scalar to its token, so
    comparing text says `.inf` round-trips. It does, *for us* -- and the file
    still says "float" to every other reader.

    `? q` is the #130 class recurring: a file we write and then cannot read.
    Everything else in the set quotes correctly, including `0x1f`, `12:30`,
    `y`, `On`, `2026-09-06`, `  lead`, and `""`.
12. **Forcing `token.DoubleQuoteType` round-trips everything.** 15/15 —
    `? q`, `.inf`, `.nan`, `-.inf`, tab, newline, `a: b`, `true`, embedded
    `"` and `\`, `é`, the empty string, and leading whitespace all come back
    byte-identical. This is what makes verify-and-retry a complete strategy
    rather than a partial one.
13. **Error text is multi-line by default and off by one.**

    ```
    [1:8] mapping value is not allowed in this context
    >  1 | title: a: b
                  ^
       2 | page_id: 9
    ```

    `yaml.FormatError(err, false, false)` gives the one-liner. The line is
    relative to the block, so file line = block line + 1.
14. **Every `.md` in this repo already passes.** 15 files with frontmatter, 0
    fail a real YAML parse. Nothing in testdata depends on our leniency.

## Decisions

**Swap the parser, do not widen the predicate.** Patching `renderValue` fixes
only files we write, and the class is bigger than the colon. Hand-maintaining a
YAML plain-scalar rule set in a package with no YAML parser to check itself
against is how the version that forgets `%` ships.

**`goccy/go-yaml`, not `gopkg.in/yaml.v3`.** Both clear every hurdle; goccy is
maintained and yaml.v3 is archived at v3.0.1 (2022).

**A file that no longer parses is a hard error.** No lenient fallback to the
first-colon split — that means shipping both parsers forever and keeps
producing the invalid files this change exists to eliminate. No repair path in
`fix` either: `fix` reconciles *from the live page* and needs `page_id` to do
it, which it cannot read out of a file it cannot parse. markfluence is
unreleased, so the only affected files are already on a developer's disk.

**A non-scalar value is a hard parse error**, naming the key. Not really a new
failure: `title:\n  - a` yields `""` today, and `create` then rejects it as "no
title" — an error naming a symptom instead of the cause. A hand-authored `|`
block is a `LiteralNode` and gets the same error: the documented contract is
flat `key: value` with no multi-line values, and our writer never produces one.

**Nulls are unset, uniformly, and identified by node type.** `NullNode` reads
as `""` for every field, whatever its spelling. This drops the "a literal
`null` is a legal title" rule, and loses nothing: goccy emits the *string*
`null` as `"null"` (quoted), so a page genuinely titled `null` still round-trips
through `pagedoc.RenderFrontmatter`. It also fixes the existing `parent: ~` /
`parent: Null` gap.

**An empty block is an empty mapping, not an error.** A `nil` body and a
`CommentGroupNode` body both become an empty mapping. Nothing about
`---\n\n---` is invalid YAML.

**Typing on the write path is ours; quoting is goccy's.** `yaml.ValueToNode("123")`
emits `"123"` and `ValueToNode("null")` emits `"null"` — both quoted, both
wrong. So `UpdateField` types by key: for `page_id` and `parent`, digits become
an `IntegerNode` and `"null"` becomes a `NullNode`; everything else is a string.
This *is* a hand-rolled predicate in a plan that argues against them, and the
distinction being drawn is that goccy still owns quoting — the thing #130 is
about — while we own typing for two fields whose value domains we already parse
elsewhere (`pageref.IsDigits`, `coordinate`). C2's justification is worded
accordingly.

**Replace the value node; never mutate its token.** Mutating a plain token's
`Value` re-emits it unquoted, which reintroduces #130 through the back door.
Node replacement also drops a stale line comment, which is what `fix` wants
when it overwrites `parent: 4  # foo.md` with a live id.

**A present-but-null coordinate equals a live null.** Once `NullNode` reads as
`""`, `fix.plannedChanges` (`fix.go:208-214`) takes its `!present || blank`
branch for `parent: null`, plans `parent: (none) -> null`, writes it, and reads
`""` again -- a correct top-level page reported `changed` forever. The first
branch narrows to `!present` so a present-but-blank value falls through to the
`norm` comparison, where `norm("null") == norm("") == ""`. The `"(none)"`
display is preserved by an explicit blank check on the old value.
`TestPlannedChangesParentNullNormalizes` (`fix_test.go:158`) exists to guard
exactly this and would have kept passing: it feeds `{"parent": "null"}`, a map
the new parser can never produce. It is rewritten to feed `""`.

**A present-but-empty `title` is an error in `update`.** `create` already
errors on any empty title, absent or present (`create.go:554`), and needs no
change. `update` currently falls back to the live page title for *any* empty
title; that stays for an **absent** `title` key — a positive statement that the
file does not manage the title, and the shape `fix.go:217` already reasons
about — and becomes an error for a **present-but-empty** one (`title:`,
`title: null`, `title: ""`), which is a typo that should not silently publish
under the live title. Needs a `MarkdownFile` accessor that distinguishes the
two; the map already carries it, and the `MarkdownFile` doc comment
(`frontmatter.go:231-233`) says it is exported for exactly this.

**Reads refuse a multi-line scalar, and a second document.** The flat contract
has to be enforced rather than assumed. An untouched key is re-emitted from the
node the parser produced, and goccy's re-emission of a parsed node is not
identity: a continued plain scalar comes back as a `|-` block that the parser
then rejects, so a write would produce a file markfluence cannot read -- in
`create`, only after the page was made. A multi-line single-quoted scalar is
worse, re-emitting on one line and silently turning `"sq\nline"` into
`"sq line"`. Detected on the value token's origin with trailing whitespace
stripped, since a token's origin runs up to the next one; a newline markfluence
wrote is a two-character escape inside a double-quoted scalar and occupies one
physical line, so it is unaffected. A `...` line starts a second document, and
reading only the first would drop every later key in silence.

**The writer verifies its own output.** goccy's default emission is wrong for
at least four shapes (verified items 11-12), and any predicate we wrote to
catch them would be incomplete -- those three non-tab cases turned up only by
probing ~80 strings, after two review rounds. So the writer does not *predict*
which values goccy mishandles, it **checks**: emit with goccy's default,
re-parse, compare; on mismatch re-emit with `token.DoubleQuoteType`, which
round-trips all 15 probed cases. If even that fails, return a typed error
rather than write a value known to be wrong -- unreachable in probing, which is
exactly what makes the fuzz target worth having.

This is the one place the plan overrides goccy, and the justification is
different from the two style overrides it declines: those were cosmetic, this
is a correctness bug that produces either silent data loss or an unreadable
file. It also upgrades C2 from "goccy is correct" to "the writer verifies its
own output", which holds against goccy regressions rather than only today's
bugs. Cost is one extra parse per value written -- at most five per file,
each over a block of under ten lines.

**`UpdateField` becomes surgical -- at node granularity, not text.** Worth
being precise, because "minimum diff" oversells it: re-emitting normalizes
intra-line whitespace (`title:    Spaced Out` becomes `title: Spaced Out`),
drops a trailing blank line before the closing `---`, and moves an interior
blank line, since a blank lives in the *preceding* value's `Origin`. Quote
style on untouched keys is preserved. This is much less churn than today's
whole-block rewrite, but it is not zero, and a test pins what actually
survives rather than claiming everything does.

Change the touched key's value node in place; insert a *new* key **before the first existing key that sorts after it**
(well-defined even in a jumbled block); never move an existing key. Minimum
diff on a file that lives in git, and it makes the comment-travel problem
disappear on the common path.

**Editing and building are separate entry points.** `UpdateField` parses, so it
returns `(string, error)`; only `create.go:451-455` and `fix.go:144` call it and
both already have error paths. `frontmatter.Render(fields) string` builds a
block from scratch and **cannot fail** — it constructs nodes and never parses —
so `pagedoc.RenderFrontmatter` keeps its signature and nothing ripples into
`read` or `export`. It also stops re-parsing and re-emitting a five-field block
five times. CLAUDE.md's "read and export cannot drift" concern is real, so a
test pins that `Render` and repeated `UpdateField` agree.

**Field order is normalized by `fix` and `create`, not by every write.** *(Deferred to `_plans/033`.)* Stable
ordering matters; doing it as a side effect of writing one field does not.
`fix` normalizes by default with no flag. `create --persist` normalizes too —
the minimal-diff argument behind a surgical `UpdateField` is about not churning
lines the caller did not ask to touch, and `create --persist` already writes all
five fields by definition. `update` stays out of it: it never writes back.
README:339 and CLAUDE.md both currently say `fix` "writes a file only when a
field actually changed" and must be updated rather than treated as a
constraint — that sentence describes today's behavior, not a guarantee in
`docs/guarantees.md`.

**A reorder-only file is `changed`, not `consistent`.** *(Deferred to `_plans/033`.)* Otherwise you run `fix`,
see `consistent`, and still have a jumbled file. Keeping `consistent` honest
also keeps `--dry-run` a faithful preview.

*(Deferred to `_plans/033`.)* **Reported as a dedicated `reordered: boolean` on `fixResult`,** not a
pseudo-entry in `changes[]`. `changes[].field` is an actual key name everywhere
else; a non-field there makes the slot polymorphic. The existing `"(none)"`
sentinel lives in `old`, explicitly a *display* slot (`oldDisplay`) — `field` is
an identity slot. No equivalent on `createResult`: `create` writes all five
fields, so its output is always canonical.

*(Deferred to `_plans/033`.)* **`Normalize` is a no-op when the order is already canonical.** Blank lines die
only as a consequence of an actual reorder, so `reordered` means exactly "keys
moved" and the blank-line behavior follows from a property of the file rather
than from whatever else `fix` was doing that run. A canonical file with blank
lines keeps them: `fix` normalizes ordering, it is not a formatter.

*(Deferred to `_plans/033`.)* **When it does reorder, comments travel with their key.** Better than today's
hoist-to-top — a comment about `page_id` belongs next to `page_id`. Not claimed
to be free: a header comment written above a key that is not `title` sinks with
it, which is visible in the diff and accepted.

**Take goccy's quote style.** `Bob's Guide: Part 2` is a realistic title, and
`"Bob's Guide: Part 2"` beats `'Bob''s Guide: Part 2'`, whose doubled quote
reads as a typo. Forcing single quotes means hand-setting `token.SingleQuoteType`
and an `Origin` on every write.

**Quote only when needed.** The original proposal was to always quote `title`.
Its correctness argument is gone — it existed because we could not trust
ourselves to detect the hazards, and now a serializer does.

**Parse errors are one-liners with our own position prefix.** `yaml.FormatError(err,
false, false)`, wrapped as `filename:line:col`, with the line corrected by +1
for the `---` opener. goccy's default four-line source art would land verbatim
in `check --json`'s `error` string field, and every line number in it is wrong
by one. Matches `internal/convert`'s existing `line %d: ` reporting, which has
`lineOffset` for the same reason.

**C2 verification is manual and issue-driven.** No second YAML implementation in
`go.mod`. If a real tool reports a divergence, that is an issue with the
offending frontmatter attached.

**There is still a write-path round-trip test**, and it is not testing goccy —
it guards that we still go *through* goccy. The failure it catches is someone
later adding a fast path that string-concatenates a frontmatter line, which is
how the current bug exists. Same reasoning as `internal/schematest` validating
against the embed.

## Implementation

### `go.mod`

Add `github.com/goccy/go-yaml v1.19.2`.

### `internal/frontmatter`

**Deleted:** `ParseValue`, `scanQuoted`, `stripInlineComment`, `quoteValue`,
`renderValue`, `splitFrontmatter`, `inlineCommentRE`, `Extract`. `Extract` and
`ParseValue` are exported but called only by this package's own tests;
`ParseValue` is a hand-parser concept that would be misleading to keep.

**Kept unchanged:** `frontmatterRE` and the `ErrUnterminatedFrontmatter`
pre-check — lexical, about the `---` delimiters, and goccy never sees it, so the
two error kinds cannot collide. `MarkdownFile` and `coordinate()`.

**New internals:**

- `parseBlock(fmText string) (*ast.MappingNode, error)` — `parser.ParseBytes`
  with `parser.ParseComments`. A `nil` or `CommentGroupNode` body returns an
  empty mapping; a `CommentGroupNode`'s comment is carried onto the first key
  later inserted, so a `---\n# note\n---` block does not silently lose it. Any
  other body kind (`StringNode` for `just text`, `SequenceNode` for `- a`) is a
  typed error. Errors go through `yaml.FormatError(err, false, false)` with the
  line offset applied. Known limit: goccy's duplicate-key message embeds a
  second position (`already defined at [1:1]`) which stays block-relative.
- `scalarValue(key string, n ast.Node) (string, error)` — a **whitelist**:
  `StringNode`, `IntegerNode`, `FloatNode`, `BoolNode` → `GetToken().Value`;
  `NullNode` → `""` regardless of spelling; everything else (`Sequence`,
  `Mapping`, `Anchor`, `Alias`, `Tag`, `Literal`) is a typed error naming the
  key.
- `toMap(m *ast.MappingNode) (map[string]string, error)`.
- `keyLess(a, b string) bool` — the canonical comparator from `fieldOrder`. The
  single source of ordering, shared by insertion and `Normalize`.
- `valueNode(key, value string) ast.Node` — the typing rule. For `page_id` and
  `parent`: `pageref.IsDigits` → integer, `"null"` → null. Otherwise string.
  Positions built with `Column: 1`.

**Changed:**

- `Parse` — lexical `---` check, then `parseBlock`, then `toMap`. Wraps the
  goccy error with the filename and the corrected line.
- `UpdateField(content, key, value, comment string) (string, error)` —
  surgical. Replace the value node if the key exists, else insert before the
  first key that sorts after it. Comment goes on the **value** node. Content
  with no frontmatter block gets one created, as today (`frontmatter.go:164-166`);
  `create --persist` on a flag-only file depends on it. Emit via
  `MappingNode.String()`, then verify-and-retry the value.

**New exported:**

- `Render(fields []Field) string` — build a block from scratch. Cannot fail.
- `Normalize(content string) (string, bool, error)` — if `keyLess` order already
  holds, return unchanged with `false`; likewise for content with no block.
  Otherwise reorder, drop blank lines textually, return `true`.
- A `MarkdownFile` accessor distinguishing an absent `title` from a
  present-but-empty one.

### `cmd/fix`

Only the call-site change `UpdateField`'s new error return forces, plus the
`plannedChanges` fix above. Normalization is `_plans/033`.

### `cmd/create`

`writeBackFrontmatter` collects the five `UpdateField` calls
(`create.go:451-455`) so one error path covers them; a failure routes through
`failKeepingPage`, since the page already exists by then. `create.go:554`'s
empty-title error is unchanged. Normalization is `_plans/033`.

### `cmd/update`

`resolveTitlePageID` gains a third return distinguishing an absent `title` key
from a present-but-empty one -- it cannot express that with two strings. The
error fires in `processFile` **before** `GetPageOrNil` (`update.go:167`),
matching the `IsDigits` pre-flight at `:155`: a local validation failure should
not cost a request. `--title` still wins, as every other override does, so
`--title X` against a present-but-empty frontmatter title succeeds. The
live-title fallback at `:174-175` stays for an absent key.

### `internal/pagedoc`

`RenderFrontmatter` builds a `[]Field` and calls `frontmatter.Render` once
instead of chaining five `UpdateField` calls. Signature unchanged.

### `schema/json-output/v1.json`

`checkResult`'s description at `:436` enumerates the `failed` causes --
"unterminated frontmatter, bad page_width, non-numeric page_id" -- and is now
incomplete. `fixResult` gaining `reordered` is `_plans/033`.

### Not changed

`cmd/check` — `check.go:120` already routes any `ParseFile` error to
`CodeValidation`/`status: failed`. `check` does not flag jumbled field order:
ordering is not a publishability defect. `internal/linkindex` already skips a
file whose frontmatter fails to parse.

## Tests

- **Write-path round trip** (the C2 guard): table over the printable hazard set
  — `UpdateField` writes it, `parser.ParseBytes` re-reads from scratch, assert
  identical. No fuzz target, no control characters.
- **`Render` and `UpdateField` agree** on the same field set — the anti-drift
  pin for the two entry points.
- **Typing**: `page_id`/`parent` emit bare `123` and `null`; a *title* of `123`
  or `null` emits quoted.
- **Null spellings**: `title:`, `title: null`, `title: ~`, `title: Null` all
  read as `""`; `parent: ~` no longer reads as an id.
- **Reads reproduce today's map** for `page_id: 123` → `"123"` and
  `space: ~abc` → `"~abc"`.
- **New hard errors**: nested value, `|` block, anchor, alias, tag, duplicate
  key, tab indentation — each naming the key. An unterminated block still
  reports `ErrUnterminatedFrontmatter`, not a goccy error. Error text is a
  single line with a file-accurate position.
- **Empty block** (`---\n\n---` and a comment-only block) parses to an empty map.
- **Surgical `UpdateField`**: existing key keeps position; new key inserts
  before the first key sorting after it; blank lines and comments elsewhere
  survive *modulo* the emitter's own normalization (intra-line whitespace,
  trailing blank line, interior blank position) -- pinned explicitly; the
  `parent` comment renders inline and the value round-trips without it; a
  hand-built node does not panic; a comment-only block's note survives the
  first insert.
- **`cmd/fix`**: a canonical top-level page with `parent: null` is
  `consistent` and converges -- the regression 1a would have caused, with
  `TestPlannedChangesParentNullNormalizes` rewritten to feed `""` rather than
  `"null"`, which the new parser can never produce; a reorder-only file is
  `changed` with `reordered: true` and is written; `--dry-run` reports without writing; canonical consistent file still
  `consistent`.
- **`cmd/update`**: present-but-empty title errors before any request; absent
  title still falls back to the live page title; `--title X` wins over a
  present-but-empty frontmatter title.
- **`cmd/check`**: present-but-empty title is `broken`; absent title is not.

**Existing tests that change** (not an exhaustive diff, but the ones known now):
`frontmatter_test.go:80-83` (quote style, `4  #` → `4 #`), `:107`, `:117`
(surgical no longer reorders), `:129` (surgical keeps blanks), `:171` (literal
`null` title); `pagedoc_test.go:13,22,30,39`; `fix_test.go:279` (jumbled
fixture flips to `changed`), `:328`.

## Docs

- `docs/guarantees.md` — add **C2** `frontmatter-is-valid-yaml`, status
  **Holds**, enforced by the writer verifying its own output rather than by
  trusting the serializer -- goccy owns quoting, we own typing for `page_id`
  and `parent`, and the verify-and-retry loop is what makes the guarantee hold
  regardless of either. The entry names the one gap honestly: the check is that
  *goccy* can re-read what goccy wrote, not that another implementation can. Add a verification-table row:
  review judgement. One sentence relating C2 to **L7** — C2 is the YAML half,
  split out because it is checked against a different external spec.
- `README.md` — the frontmatter section (~1057-1093): the block is YAML, values
  are quoted when YAML requires it, flat-key is now enforced rather than
  assumed, and what an author can no longer write bare. The `fix` section
  (:334-340): drop "writes a file only when a field actually changed", add
  order normalization.
- `CLAUDE.md` — the `internal/frontmatter` bullet; the `fix` sentence; the
  `check` bullet, whose "deliberately narrow" list is now incomplete (duplicate
  keys, tabs, nested values, reserved indicators all fail too) and whose
  create-vs-update justification no longer covers `title`.

## Commits

1. `build: add goccy/go-yaml`
2. `refactor(frontmatter): read and write the block with goccy` -- the reader
   and writer swap in **one** commit. Split, commit 2 leaves the old
   `renderValue` writing `title: a: b` bare while the new reader rejects it, so
   `TestWriteThenReadRoundTrips` (`frontmatter_test.go:95`) fails mid-series and
   the per-commit `make check` rule breaks.
3. `feat(frontmatter): surgical UpdateField, Render, and Normalize`
4. `fix(fix): a present-but-null coordinate matches a live null`
5. `fix(update): error on a present-but-empty title`
6. `feat(check): report a present-but-empty title as broken`
7. `test(frontmatter): pin that every write verifies its own output`
8. `docs: C2 (frontmatter-is-valid-yaml), README, CLAUDE.md`

## Consequences found during implementation

- **`page_width: null` was an error and is now "unset".** It used to reach
  `pagewidth.Declared` as the string `"null"`, which is not in the width
  vocabulary. Every null spelling now reads as `""`, so it means "not set" and
  the default applies -- and `check` no longer reports it. Follows from the
  uniform null rule and is the better behaviour, but it was a consequence rather
  than a decision.
- **A `create --persist` write can now fail on frontmatter.** `UpdateField`
  returns an error, and it arrives after `CreatePage`. Routed through
  `failKeepingPage`, the same path the existing `os.WriteFile` failure uses, so
  the page id survives in the result rather than becoming an orphan.
- **The commit split in the plan was not achievable.** `UpdateField` gaining an
  error return forces every call site into the same commit as the package, so
  the reader/writer swap, the `plannedChanges` convergence fix, and the `update`
  title check land together -- the tests do not pass otherwise.

## Out of scope

- **A tab in a value under goccy's own default emission.** Fixed incidentally by
  the double-quote fallback, but the *cross-parser* gap remains: `1e3` is a
  string to goccy and a float to yaml.v3, and no self-check can see that.
- **#100** (`markfluence.yaml` project-wide settings) now has a YAML parser
  available, but this change does not touch that file.
- **Field-order normalization**, split out to `_plans/033`.
- **A repair path for an unparseable file.** Circular, as argued above.
- **#38** (sidecar frontmatter) and **#21** (`layout:` directive) both get
  easier after this; neither is in here.
