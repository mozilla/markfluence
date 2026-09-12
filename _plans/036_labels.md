# Plan: publish page labels from frontmatter

Add a `labels` frontmatter field that `update`/`create` assert against the live
page, `fix`/`read`/`export` recover from it, `info` displays, and `check`
validates offline. Implements #138.

Labels are how Confluence content is actually organized and searched — the SRE
space carries 94 distinct labels across 1123 pages — and today markfluence
cannot see them at all: a published page must be labeled by hand in the UI
afterward, and `read`/`export` silently drop the labels a page already carries.
The README's own `search --cql 'label = "runbook"'` example only pays off for
pages someone labeled by hand.

## Two layers, and the lower one is the real work

The feature splits cleanly, and the lower half is both larger and more
dangerous than the label logic on top of it.

**`internal/frontmatter` does not support sequences at all.** `toMap` calls
`scalarValue` on every mapping value, and `scalarValue` is a whitelist of
scalar node kinds — so a `*ast.SequenceNode` returns `frontmatter "labels" must
be a single scalar value, found Sequence`. That error comes out of `Parse`,
which means the moment anyone writes `labels: [a, b]` **every** command that
touches the file fails, not just the label-aware ones: `update` on that file
reports a frontmatter error instead of publishing. So sequence support has to
land first and has to be general — a parser that special-cases the key `labels`
would leave `reviewers: [ana, bo]` breaking the same way, and #21 and #100 both
want the same door opened.

**The label vocabulary on top is ordinary**, modeled on `internal/pagewidth`:
validate, normalize, diff against the live set, apply.

## What Confluence does — verified 2026-09-08

Probed against `mozilla-hub.atlassian.net` with a scratch page in a personal
space: label names POSTed one at a time via v1, read back through both v1 and
v2, both delete forms exercised, page purged. The full table goes into
`docs/confluence/labels.md`; the four findings that shape the design:

1. **A space or comma is a separator, not a character.** `"Runbook Two"` posts
   as two labels, `runbook` + `two`. `"a,b"` posts as `a` + `b`. HTTP 200, no
   warning. Under assert-exactly this is a permanent non-convergence:
   `labels: [Runbook Two]` publishes as two labels, reads back as neither, and
   is re-added on every `update` forever with no way to remove it. Two rows in
   the SRE space look like this having already happened to a human —
   `continuous` + `delivery` and `url` + `shortener`, each once, on pages where
   someone plainly typed a two-word label. **This is what the validation exists
   to prevent, and it is why an invalid label is a hard failure rather than a
   repair.**
2. **The reject set is the server's own**: a 400 `label.contains.invalid.chars`
   names `space ! # & ( ) * , . : ; < > ? @ [ ] ^`. A colon is in it, so there
   is no silent prefix-splitting and no frontmatter syntax for a prefix.
3. **Removal must use `?name=`, not `DELETE /label/{name}`.** The path form
   works until the name holds a `/`: `a%2Fb` → 400 (Tomcat HTML, no JSON),
   while `?name=a/b` → 204. Not hypothetical: `ci/cd` is a real label in the
   SRE space.
4. **Writes are v1; v2 is read-only for labels** (`POST`/`DELETE` on
   `/wiki/api/v2/pages/{id}/labels` → 405). Exactly the attachment split.

Plus: names are lowercased server-side (Unicode-aware), the cap is **255
UTF-16 code units** (255 × `é` → 200, 256 × `é` → 400; 128 emoji → 400), POST
is additive and idempotent with no bulk-set route, a DELETE of an absent name is
404, and neither GET returns a useful order.

A survey of the SRE space found **every label is `global:`**, which is the
evidence for managing that prefix and nothing else.

## What goccy does with sequences — verified 2026-09-11

Probed with the pinned `goccy/go-yaml`, the same way #130's scalar traps were
found. Both spellings are accepted on read (see Decisions), so both were
measured.

**Both forms read correctly, and a block sequence survives the write path.**
Parsed, reordered the way `Normalize` does, run through `dropBlankLines`, and
re-parsed: a block list comes back as a block list with the same elements, a
`# comment` inside it survives, and a blank line between two items is dropped
without changing the value (a blank line between items means nothing in YAML,
unlike inside a `|-` block, where it is content). So there was never a
correctness reason to refuse block form.

**Flow style is a field, not something to infer from the token.** A
`*ast.SequenceNode` carries `IsFlowStyle`. The sequence node's own
`GetToken().Origin` is just `" ["` — it does **not** cover the sequence's text
— so a `spansLines` check on the outer node proves nothing either way.

**The line check has to move to the element interior.** `spansLines` trims only
the right side, which is correct for a scalar but wrong for an element, where a
*leading* newline means "this element began on a new line" — structure, not
content. Measured element origins:

| source | element origin | `spansLines` | interior check |
|---|---|---|---|
| `[a, b]` | `" b"` | false | false |
| `[a,`⏎`  b]` (wrapped flow) | `"\n  b"` | **true** | false |
| `- a` (block) | `" a\n  "` | false | false |
| `- a plain scalar`⏎`    continued` | `" a plain scalar\n    continued\n  "` | true | **true** |
| `- 'sq`⏎`    folded'` | `" 'sq\n    folded'"` | true | **true** |

So the rule is **a sequence may span lines; every element must be a
single-line scalar**, enforced as the node-kind whitelist plus a line check on
the origin trimmed at *both* ends. Reusing `spansLines` unchanged would refuse
a wrapped flow sequence for no reason.

**Elements report indicator characters exactly as scalars do.** `&anch a`
reports node type `Anchor` with token value `&`, `*anch` reports `Alias`/`*`,
`!!str a` reports `Tag`/`!!str`, and a `- |-` element reports `Literal`/`|-`.
Note the last one: its origin does **not** span lines, so the whitelist is the
only thing that catches it — exactly as for scalars. A blacklist would silently
read `|-`.

**Write-side quoting must be verified in the form being emitted, and the two
forms have different traps.** The existing `readsBackAs` checks a scalar in
*mapping* context. Measured, emitting with goccy's chosen style and re-reading:

| value | mapping | flow `[…]` | block `- …` |
|---|---|---|---|
| `x,y` | reads back | **splits into two elements** | reads back |
| `has]bracket` | reads back | **ends the sequence; unparseable** | reads back |
| `? q` | fails (#130) | fails | **parses as a `Mapping`** (`- ? q` is YAML's explicit-key syntax) |
| `.inf` | fails | fails | **reads back as `Infinity`, not a string** |
| tab | fails (dropped) | fails | fails |
| `#hash`, `{braces}`, `true`, `""`, `ci/cd` | reads back | reads back | reads back |

Neither form's check substitutes for the other, and mapping context passes two
values that flow context corrupts. With a per-element fallback to a
double-quoted scalar, every probed value round-trips in the form it was
checked in.

**Indentation is controllable, so emitting block form is not a problem.**
`ast.Sequence(tok, false)` with default token positions renders `labels:`⏎`- a`
— valid YAML, but not the `  - a` convention. Setting the sequence token's
`Column` fixes that precisely: `3` yields `  - a`, `5` yields `    - a`. So a
hand-built block sequence can be emitted in the conventional shape, which is
what makes preserving the author's *form* cheap.

Reindenting an author's list is explicitly **not** a concern — correct YAML and
the right form are the requirements, matching indentation is not one — so
markfluence does not try to reproduce a 4-space list at 4 spaces. It carries
over `IsFlowStyle` and emits block items at column 3. (Replacing only the
parsed node's elements, borrowing each original item's position, also works and
preserves the indent exactly; it is more machinery for a property nobody needs.)

**`yaml.ValueToNode([]string{...})` returns a block sequence**
(`IsFlowStyle == false`), so it cannot be used to build the value. The node is
assembled by hand: `ast.Sequence(tok, true)` with per-element nodes appended.
An empty sequence renders `[]` in both styles.

**Re-emission of a parsed sequence is faithful**, including each element's
original quoting style (`'x,y'` and `"has]bracket"` survive as written), which
is what lets an untouched `labels:` key — in either form — pass through a `fix`
write unchanged. One mutation: a `~` element re-emits as `null`. Harmless —
both read as the empty string, and for `labels` both are invalid anyway.

Worth noting how the quoting work interacts with the label rules: every
character that breaks flow context (`,` `]` `[` `?` `#` space `.`) is *also* in
Confluence's reject set, so for `labels` specifically the trap is unreachable —
a value needing the fallback is refused before anything is written. The quoting
work is load-bearing for the **general** field, which is why the generality
test below is not optional.

## Decisions

**Sequences get their own map, not a re-typed `Frontmatter`.**
`MarkdownFile.Frontmatter` stays `map[string]string` and a sequence-valued key
is **omitted** from it, landing in a parallel `Lists map[string][]string`.
Changing the scalar map's type would touch every caller for no gain, and
leaving a sequence key in it with some flattened spelling is worse than absent:
`MarkdownFile.field()` would hand `update` a title of `[a, b]`. The parser
learns a *kind*, not a name.

**Both sequence forms are accepted on read, and an author's form is
preserved on write.** The block form is valid YAML that goccy parses correctly
and that survives `Normalize` intact, so refusing it would mean rejecting a
file markfluence understood perfectly — and a long label list genuinely reads
better as a block list, which is the case that matters, since a set large
enough to want block form is exactly the set whose flow spelling is an
unreadable 200-column line.

What is preserved is the **form**, not the formatting: a rewrite reads the
existing value's `IsFlowStyle` and emits the same style, with block items at a
fixed column 3. Reindenting a list an author wrote with some other indent is
accepted — valid YAML in the right form is the bar, byte-preservation is not.
A `fix` that changes a block list's contents writes a block list back. The
alternative — always emitting flow — was in an earlier draft on the false
premise that a hand-built block node could not be indented at all; it can.

The real cost is that markfluence can now emit **both** forms, so the
write-side self-check has to run in whichever form is about to be written. The
trap sets differ (see above: `? q` becomes a `Mapping` in block form, `.inf` an
`Infinity`, while `x,y` and `has]bracket` are flow-only hazards), so one
context's check does not stand in for the other's. That is a real addition to
the surface keeping C2 true, and it is the price of not mangling the field this
feature exists to write.

**A brand-new `labels:` key is written flow**, since `read`/`export`/`create
--persist` have no author choice to honour and a generated list is usually
short. A page carrying enough labels to want block form gets one long line the
first time, and keeps whatever the author changes it to thereafter.

**A scalar `labels:` is refused rather than read as a one-element list.**
`labels: runbook` is legal YAML and the friendly reading is obvious, but the
field is destructive — declaring it *removes* every label not listed — and
`labels:` with a null value would then mean "remove every label" on a file
where the author most likely typed a key and stopped. Both forms error with a
message naming both sequence spellings, and `labels: []` stays the one way to
say "remove them all". Rejected alternative: accept a scalar and rewrite it as
a list on the next `fix`; it saves the author one pair of brackets and costs
the ability to tell an unfinished edit from an instruction to strip the page.

**Validation refuses, never repairs — except for case.** Lowercasing is the one
normalization, and it comes with a warning naming the label, because Confluence
lowercases server-side and a file that disagrees would never converge. Anything
else invalid is a hard failure before any write. A leading or trailing space is
refused like an inner one (`TrimSpace` is used only to give an all-whitespace
value the clearer "empty" message), since a space is a separator server-side and
trimming would be a silent repair of the exact class of input that produced
`continuous` + `delivery`.

**The reject set is mirrored, not an allowlist.** An allowlist would refuse
`ci/cd`, `dataops_reports` and `héllo-wörld`, all of which publish fine. The
error message quotes the set, because `.` being invalid means a version-shaped
label (`v1.2`) is impossible and no author will guess that.

**`internal/labels` owns the set arithmetic**, so no command reimplements
"what to add, what to remove". Callers filter to `prefix == "global"`; the
client does not, because `info` needs the unfiltered list.

**Absent means untouched, declared means asserted.** This mirrors `update`'s
existing asymmetry for `page_width`, and it is what keeps a hand-labeled page
from being silently stripped by a run that never mentioned labels. Pinned by a
test asserting that an absent `labels:` key produces **no label request at
all** — not merely no write. Without that, "untouched" is an implementation
detail rather than a property.

**Labels are applied after the body and after the width**, non-fatally: a
failure there is a warning on an `ok: true` result, exactly as
`pagewidth.Apply` failures already are. The page is published by then; failing
the result would say the publish did not happen. Ordering after width keeps the
human output and the `--json` field order matching `info`'s layout.

**`create` validates labels in preflight** alongside #127's converter check, so
a bad label cannot leave a created page behind, and applies them in the publish
phase. The preflight failure carries `CodeValidation` (a bad label is a property
of the file, not of the conversion, so not `CodeConvert`).

**`fix` rewrites `labels:` only when the set differs.** Compared as sets, so a
hand-written list keeps the author's order and any duplicate; when markfluence
generates the list itself (`read`, `export`, `fix` making a change) it emits
sorted and deduped. Neither GET returns a useful order, so sorting locally is
not a preference — unsorted output is unstable across runs.

**A new law, `L9`, for the assert-exactly rule**, with an honest **Partial**:
`update` leaves an undeclared field alone, but `create` asserts a *default*
`page_width` for a file that declares none, so "omitted means untouched" holds
for labels and not yet for width.

## Implementation

### `internal/frontmatter` (the general half)

- `sequenceValue(key string, n *ast.SequenceNode) ([]string, error)` — accepts
  either style, reading each element through `elementValue` with an indexed key
  (`labels[1]`) so a message points at the offending item.
- `elementValue(key string, n ast.Node) (string, error)` — `scalarValue`'s
  sibling, sharing the node-kind whitelist (so `Anchor`/`Alias`/`Tag`/`Literal`
  are refused rather than read as their indicator characters) but checking the
  line rule on the origin trimmed at **both** ends, since a leading newline in
  an element origin is structure. Factor the whitelist into one helper both call
  rather than copying the type switch — a second copy is how one of them
  silently stops refusing anchors.
- `toMap` becomes `toMaps(m) (map[string]string, map[string][]string, error)`,
  branching on `*ast.SequenceNode` before falling through to `scalarValue`. A
  sequence key is absent from the scalar map.
- `MarkdownFile` gains `Lists map[string][]string`, always non-nil, with the
  same "exported so callers can distinguish absent from empty" rationale the
  `Frontmatter` doc comment already gives.
- `Field` gains `List []string`; **non-nil means the field is a sequence** and
  `Value` is ignored, so `[]string{}` renders `labels: []` and a nil list stays
  a scalar. `Render` and `mappingValue` branch on it.
- `sequenceNodeFor(values []string, flow bool) ast.Node` —
  `ast.Sequence(tok, flow)` with each element from `elementNodeFor`, block
  items emitted at column 3. `elementNodeFor` is `valueNodeFor`'s sibling:
  goccy's chosen style when `readsBackInSeqAs(n, want, flow)` accepts it, else
  `doubleQuoted`.
- `readsBackInSeqAs(n ast.Node, want string, flow bool) bool` — wraps the node
  in a one-element sequence **of the style about to be emitted**, inside a
  one-key mapping, re-parses, and requires a `*ast.StringNode` element whose
  text matches. Both halves are load-bearing: the node-kind check for the same
  reason it is for scalars (`.inf` reads back as `.inf` either way while saying
  "float" to every other tool), and the style parameter because the two
  contexts have different traps — mapping context passes `x,y` and
  `has]bracket` which flow corrupts, and block context turns `? q` into a
  `Mapping`. A single fixed context would be a check that passes while the
  write is wrong.
- `UpdateListField(content, key string, values []string) (string, error)` —
  `UpdateField`'s list form, sharing `setField` so the surgical key-node
  preservation is not duplicated. `setField` gains one lookup: if the key
  already holds a sequence, its `IsFlowStyle` is carried into the replacement,
  so a block list stays a block list. An untouched key is never re-emitted at
  all, so a list nothing changes keeps its exact bytes.
- The "flat mapping" error text in `parseBlock` and the package doc comment both
  say sequences are allowed in either style, with single-line elements.
- `Normalize`/`dropBlankLines` need no code change, but the safety comment on
  `dropBlankLines` does: its justification is currently "nothing this package
  emits spans more than one line", which a passed-through block list makes
  false. The accurate statement is that no value markfluence can emit carries a
  *meaningful* blank line — a `|-` block would, and is refused on read. Pinned
  by a test that runs a block list through `Normalize`.

### `internal/labels` (new)

```go
const RejectChars = ` !#&()*,.:;<>?@[]^`  // the server's own set

// Set is what a file asserts: the normalized names, whether the key was
// present at all, and any warning normalization raised.
type Set struct {
	Names    []string // lowercased, sorted, deduped
	Declared bool
	Warnings []string
}

func Declared(lists map[string][]string, fm map[string]string) (Set, error)
func Validate(name string) error
func Normalize(name string) (norm string, changed bool)
func Diff(declared, live []string) (add, remove, unchanged []string)
func Global(live []client.Label) []string
func Apply(c *client.ConfluenceClient, pageID string, s Set) ([]Action, error)
func Read(c *client.ConfluenceClient, pageID string) ([]client.Label, error)
```

`Declared` takes both maps so it can refuse the scalar form with a useful
message rather than reporting "not declared" for `labels: runbook`. A `Set`
rather than four return values because `Declared` is the one call every command
makes, and `(names, declared, warnings, err)` at five call sites is four
opportunities to drop the warnings. Length is
`len(utf16.Encode([]rune(s))) <= 255` — not bytes, not runes.

`Action` is `{Name, Action string}` with `added`/`removed`/`unchanged`, which is
what the `--json` field reports; `Apply` returns the **full** declared set's
worth of actions, not just the changes.

### `internal/client`

- `Label` struct (`id`, `name`, `prefix`).
- `ListLabels(pageID)` — v2 `GET /wiki/api/v2/pages/{id}/labels` via `listV2`,
  unfiltered.
- `AddLabels(pageID, names []string)` — v1 `POST
  /wiki/rest/api/content/{id}/child/label`, batched (the route takes an array).
- `RemoveLabel(pageID, name)` — v1 `DELETE
  /wiki/rest/api/content/{id}/child/label?name=…`, with a comment recording why
  the path form is wrong and citing `ci/cd`. A 404 is "already gone", not a
  failure — via `notFound`, so a rejected credential is not mistaken for it.

### Commands

| command | change |
|---|---|
| `update` | assert the declared set after width; validation fatal before any write; apply failure is a warning on `ok: true`; an mtime-skipped file skips labels too; dry-run reads live labels and reports would-be actions, a read failure being a warning (mirrors `previewWidth`) |
| `create` | validate in preflight (after every server check, like #127's convert), apply in publish |
| `fix` | one `change{field: "labels"}` rendered `[a, b]`, `(none)` when the file has none; written through `UpdateListField`. `change` gains a `newList []string` |
| `info` | `labels` and `labels/unmanaged` rows, the second only when non-empty, per the existing "empty fields omitted" rule |
| `read`, `export` | `labels:` in the rendered frontmatter, global-only and sorted, via `pagedoc.Frontmatter`/`RenderFrontmatter` |
| `check` | `labels.Declared` + `Validate` beside the `pagewidth.Declared` call, `status: failed` with `code: VALIDATION`; case warnings land in `warnings` |

`pagedoc.Frontmatter` fetches the labels best-effort in the shape every other
lookup there already uses: omitted on failure, never fatal. `read`'s `--json`
re-reads them in `buildResult`, which duplicates a request — the same thing it
already does for `page_width`, and consistency with the existing pattern beats
threading a value through `Render` for one command.

### `--json` and the schema

New `$defs`: `labelAction` (`{action, name}`), `labelInfo`
(`{name, prefix, managed}`), and the `…OrNull` wrappers. Per-command fields per
#138's table — `update`/`create` get `labels` (full declared set, `null` when
the file declares no key, would-be actions in a dry-run), `info` gets
`labels` as `labelInfo[]` (`null` when the fetch failed, `[]` when the page has
none), `read` gets `labels` as a plain sorted string array (`null` when the
fetch failed, so "none" and "unknown" stay distinguishable). `fix`, `check` and
`export` get nothing new.

Every field on a typed struct, no `omitempty`, nullability by pointer —
`*[]jsonout.Label`, not a slice that happens to be nil. Schema edits land in the
**same commit** as the command change or `TestSchemaConformance` fails.

## Tests

Beyond the per-function unit tests, the ones that pin a decision:

- **Generality.** A file with `reviewers: [ana, bo]` — a list key markfluence
  knows nothing about — survives a `fix` write untouched. Without it, nothing
  stops `toMaps` from hardcoding `labels` and satisfying every other test here.
- **Per-style quoting.** The probe corpus (`x,y`, `has]bracket`, `? q`,
  `.inf`, a tab, `#hash`, `true`, `""`) written through `UpdateListField` reads
  back as the same strings, **in both styles** — a table test over
  `flow ∈ {true, false}`. `x,y`/`has]bracket` are the values a mapping-context
  check wrongly passes; `? q` is the one a flow-context check wrongly passes for
  a block write. Between them they are what stops the three `readsBack` helpers
  being collapsed into one.
- **Element node kinds.** `[&anch a]`, `[*anch]`, `[!!str a]` and a `- |-`
  element are refused, not read as `&`/`*`/`!!str`/`|-`. The `|-` case is the
  one whose origin does not span lines, so it proves the whitelist is doing the
  work rather than the line check.
- **Both styles read the same.** A block list and the equivalent flow list
  produce identical `Lists` output, as does a flow list wrapped across lines.
- **Element line rule.** A block element continued onto the next line, and a
  multi-line single-quoted element, are both refused; a wrapped flow sequence
  is **not** — that pair is what distinguishes the interior check from
  `spansLines`.
- **A block list survives `Normalize`.** Reordering a file whose `labels:` is a
  block list leaves the list intact (items, indentation, an inline comment) and
  the result re-parses — the property `dropBlankLines`' reworded comment now
  claims.
- **Form survives a rewrite.** `fix` changing the label set on a file whose
  `labels:` is a block list writes a **block** list back (and a flow list stays
  flow). A file whose labels already match is not rewritten at all, so its
  bytes are untouched. Indentation is *not* asserted beyond "parses and is the
  right form" — reindenting is accepted deliberately.
- **Render/UpdateListField agreement** on the same list, extending the existing
  test that pins the two writers against each other.
- **Absent means no request.** `update` on a file with no `labels:` key makes
  zero label calls (httptest, asserting on paths seen).
- **Length boundary.** 255 × `é` valid, 256 × `é` invalid, 128 emoji invalid —
  the three server-measured points, so a rune- or byte-based check fails.
- **The separator class.** `Runbook Two`, `a,b`, `my:foo` all refused, with the
  reject set in the message.
- **Client shapes.** `RemoveLabel` uses `?name=` and never a path segment
  (pinned on the request URL, with `ci/cd`); a 404 from it is success; a
  rejected-credential 404 is not; `ListLabels` follows a `_links.next` cursor.
- **`fix` convergence.** Reconciling a page whose labels differ writes
  `labels: [a, b]`, and a second run reports `consistent` — the property the
  `continuous` + `delivery` case is the absence of.

## Docs

- `docs/confluence/labels.md`, new: the verified table above, marked
  **Verified 2026-09-08**, plus a pointer from `docs/confluence/README.md`'s
  index.
- `docs/confluence/api.md`: the three label routes in the scope table. The v1
  write scope is **unverified** — presumably `write:confluence-content`, which
  that document already records as unlookupable for v1 routes.
- `docs/guarantees.md`: C2's wording gains sequences — a value is a
  single-line scalar, or a sequence (either style) whose every element is one —
  with the flow-context finding recorded as the reason the writer's self-check
  needed one check per style, and the fact that markfluence now emits two
  shapes stated plainly rather than left to be discovered; new **L9**
  `declared-metadata-is-asserted`, status **Partial**, with the
  `create`-default-width exception named.
- `README.md`: a `labels` row in the frontmatter table, notes in the
  `update`/`create`/`fix`/`check` sections, and a pointer from the existing
  `--cql 'label = "runbook"'` example (line 621) noting labels are now
  publishable.
- `CLAUDE.md`: `internal/labels` in the layout list, and the frontmatter
  bullet's "flat key: value" wording.

## Commits

1. `docs: plan for publishing labels from frontmatter` (this file, on `main`)
2. `docs(confluence): record how page labels behave` — `labels.md`, the README
   index row, the `api.md` scope rows. First, so the client's comments can cite
   it.
3. `feat(frontmatter): read and write sequences in either style` — the
   general half, including the generality, both-styles, and quoting tests.
4. `feat(client): list, add, and remove page labels`
5. `feat(labels): validate and reconcile a page's label set`
6. `feat(check): validate labels offline`
7. `feat(update): assert the declared label set`
8. `feat(create): validate labels in preflight and apply them on publish`
9. `feat(fix): reconcile a page's labels into the file`
10. `feat(info): show a page's labels, managed and not`
11. `feat(read): emit labels in rendered frontmatter` — `pagedoc`, so `read` and
    `export` change together
12. `docs: labels in the README and guarantees`

`make check` before each.

## Out of scope

Per #138: no `label-*` subcommand family, no `--labels` flag (a label set is
multi-valued and the only natural CLI separators are exactly the two characters
Confluence splits on; if a flag is ever wanted the spelling is a repeatable
`--label ci/cd --label howto`), no `--label` filter on `find`/`search` (`search
--cql 'label = "runbook"'` covers it), and no space- or project-wide default
labels (#100).

Not settled here: the OAuth scope for the v1 label routes (a scoped-token run
will find out) and whether the 255-unit cap is enforced identically on the v2
read path (irrelevant unless another client bypassed it).

## Follow-ups

- #100 — project-wide settings; default labels for a tree are the obvious next
  step
- #21, #38 — both want frontmatter to carry more than flat scalars; the
  sequence support here is the door they need opened
- #73 — the label paths are v1 writes against a live server that no unit test
  can prove; they want a smoke test
- #29 — the GitHub Action, and the reason supplying frontmatter out of band
  matters
