# Plan: render an `ac:adf-extension` once, and make the callout map colour-faithful

Two changes that turn out to be one. `export`/`read` emit an editor-authored
Note panel's content **twice** (#125), because `<ac:adf-extension>` falls
through a transparent-wrapper default that renders both the authoritative node
and its pre-rendered fallback. Fixing it means teaching the converter what an
`ac:adf-extension` is — and once it knows, the purple panel that has no
Confluence macro becomes reachable in *both* directions, which is exactly what
the callout map has been missing.

Closes #125.

Reported against
[Cloud Engineering QBR KPIs](https://mozilla-hub.atlassian.net/wiki/spaces/SRE/pages/2496725010/Cloud+Engineering+QBR+KPIs),
which is the live fixture for the read direction.

## Current state of the codebase

### The bug

`storage_to_md.go`'s `renderBlock` ends in

```go
default:
    // Unknown element: render its children as blocks (transparent wrapper).
    return strings.Join(r.blockStrings(n.kids, listIndent), "\n\n")
```

which is right for a wrapper that adds nothing, and exactly wrong for
`<ac:adf-extension>`, whose entire purpose is to carry the same content twice:

```xml
<ac:adf-extension>
  <ac:adf-node type="panel">
    <ac:adf-attribute key="panel-type">note</ac:adf-attribute>
    <ac:adf-attribute key="local-id">54e36e4937ac</ac:adf-attribute>
    <ac:adf-content>…the content…</ac:adf-content>
  </ac:adf-node>
  <ac:adf-fallback>
    <div class="panel …"><div class="panelContent" …>…the same content…</div></div>
  </ac:adf-fallback>
</ac:adf-extension>
```

Both children are unknown, both are transparent, so both render. `inlineString`
has the same defect for the same reason: its `default` is
`renderInlineChildren`. `renderCallout` is never reached — this is not a
callout bug.

### The callout map

`callouts.go` maps a GitHub alert to a Confluence macro, and
`storage_to_md.go` inverts it:

```go
var calloutMacro = map[string]string{        var calloutMacroInverse = map[string]string{
    "note":      "info",                         "info":    "NOTE",
    "tip":       "tip",                          "tip":     "TIP",
    "important": "note",                         "note":    "IMPORTANT",
    "warning":   "warning",                      "warning": "WARNING",
    "caution":   "warning",                  }
}
```

Many-to-one, so `CAUTION` is unrecoverable — `README.md:438` says so, and
`calloutMacroInverse`'s comment calls it out. It is also **colour-wrong in two
of five places**, which nobody had measured until now (finding 10).

### Reusable pieces

- `renderRawBlock` emits a block-level element for passthrough in a round-trip
  safe form: each wrapper tag on its own line (a CommonMark type-7 HTML block),
  a *content container*'s body converted to markdown and set off by blank
  lines, leaf children serialized raw on one line. The content-container set is
  hardcoded to `ac:rich-text-body` and `ac:layout-cell`.
- `serialize` re-emits a node as storage XML for inline passthrough.
- `droppedAttrs` (`ac:macro-id`, `ac:local-id`) omits server-generated
  per-instance ids from passthrough serialization.
- `renderBlockquote` in `renderer.go` writes the macro when a blockquote
  carries the `calloutAttr` node attribute; `calloutTransformer` sets it.
- `renderCallout` in `storage_to_md.go` reads `ac:rich-text-body`.
- `shieldStorage`, `findChild`, `prefixLines`.

## What was verified live (2026-09-01)

Against `mozilla-hub.atlassian.net` through the API gateway. Scratch pages in
the personal space carried the probes and were trashed afterwards. The evidence
goes into `docs/confluence/storage-format.md`.

1. **The four callout macros map onto four of the five ADF panel types.**
   Published as storage, read back as `atlas_doc_format`:

   | macro published | ADF `panelType` | colour |
   |---|---|---|
   | `info` | `info` | blue |
   | `tip` | `success` | green |
   | `note` | `warning` | yellow |
   | `warning` | `error` | red |

   **The two vocabularies collide on three strings and agree on one.** This is
   the trap in the whole area:

   | string | as `ac:name` on a macro | as an ADF `panelType` |
   |---|---|---|
   | `info` | blue | blue — *the only agreement* |
   | `note` | **yellow** | **purple** |
   | `warning` | **red** | **yellow** |
   | `tip` | green | not a panel type |
   | `success` | not a macro | green |
   | `error` | not a macro | red |

2. **Purple `note` is the only panel type with no macro, and the only one that
   serializes as an `ac:adf-extension`.** PUTting all five panel types as ADF —
   which is what the editor does on any save — and reading the storage back
   gave four `ac:structured-macro`s and one `ac:adf-extension`.

3. **markfluence's current callout output survives an editor save unchanged.**
   Follows from 1 and 2: every macro it emits round-trips as a macro.

4. **A bare `ac:adf-extension` with no `ac:adf-fallback` is accepted on a
   storage PUT**, reads back as a real ADF `panel` node with
   `panelType: "note"`, and stores byte-identical. So `MdToConfluence` can emit
   a purple panel, and passthrough without the fallback is lossless.

5. **Confluence does not regenerate `ac:adf-fallback` in storage.** The page
   from 4 still had no fallback on a later read.

6. **`ac:adf-fallback` is a cache of a derived rendering, not a source of
   truth.** `body-format=export_view` on that fallback-less page returns the
   styled div in full — correct `#EAE6FF` background, `#998DD9` border,
   complete body — and `body-format=view` likewise. PDF/Word export is built
   from `export_view`, so the one consumer the stored fallback plausibly served
   is served without it.

7. **The bug is a live L5 violation with content loss, not just an ugly
   export** (#125). `check --show-html` on the QBR export reports **zero**
   `adf-extension` and **two** copies of each panel's text: republishing that
   export would delete both purple panels and duplicate their prose into the
   body.

8. **An `ac:adf-extension` carrying a panel type that *does* have a macro is
   accepted, stored verbatim, and produces identical ADF.** Publishing
   `panel-type` `info`/`success`/`warning`/`error` as extensions was not
   normalized on write.

9. **But an editor save rewrites those four back to macros.** Follows from 8
   and 2: the ADF is the same either way, and serializing that ADF to storage
   yields the macro. So `ac:structured-macro` is the **canonical** storage
   spelling that Confluence's own serializer picks, not a deprecated one it
   tolerates — "legacy" is the wrong word for it and is avoided throughout.
   The extension is what Confluence falls back to when no macro exists.

10. **GitHub's alert colours, and what two publishers do with them.** GitHub
    renders NOTE blue, TIP green, IMPORTANT **purple**, WARNING orange,
    CAUTION red
    ([changelog](https://github.blog/changelog/2023-12-14-new-markdown-extension-alerts-provide-distinctive-styling-for-significant-content/)).
    `kovetskiy/mark` is publish-only — no `adf`, `panelType` or
    `ac:adf-extension` anywhere in it, and no storage→markdown direction, so it
    cannot have #125 — and emits the same canonical macros
    (`renderer/blockquote.go`, `renderer/gh_alerts_title_test.go`):

    | alert | GitHub | mark → macro | mark's colour | markfluence → macro | our colour |
    |---|---|---|---|---|---|
    | NOTE | blue | `info` | blue ✓ | `info` | blue ✓ |
    | TIP | green | `tip` | green ✓ | `tip` | green ✓ |
    | IMPORTANT | **purple** | `info` | blue ✗ | `note` | yellow ✗ |
    | WARNING | orange | `note` | yellow ✓ | `warning` | red ✗ |
    | CAUTION | red | `warning` | red ✓ | `warning` | red ✓ |

    mark gets four of five and misses only IMPORTANT, because purple is
    unreachable from a macro. markfluence gets three of five. Finding 4 says
    purple *is* reachable for us.

## Decisions

### The double render

**An `ac:adf-extension` renders its `ac:adf-node` and never its
`ac:adf-fallback`** — type-agnostic, no `type="panel"` condition, because a
fallback is by definition a second rendering of the content the node already
carries. Whatever extension type Atlassian ships next is fixed in advance.

**One exception: no node, render the fallback.** If an extension has no
`ac:adf-node`, it passes through intact, fallback included. Never observed and
possibly nonexistent; the reason to write the condition is that the failure
mode without it is silent content deletion on export and then, on the next
`update`, from the page.

**`ac:adf-content` becomes a content container**, joining
`ac:rich-text-body`/`ac:layout-cell`. Otherwise a panel's prose and bullets
serialize as one raw element per line and the export is XML nobody can edit.

**`<ac:adf-attribute key="local-id">` is dropped.** Same rationale as
`droppedAttrs`; finding 4 shows a panel with none is accepted. It is an
*element* keyed by `key`, not an attribute, so it needs its own line.

**Both block and inline context.** `inlineString`'s default duplicates the same
way. Mirrors `ac:structured-macro`'s existing presence in both switches.

**markfluence does not synthesize `ac:adf-fallback`.** Findings 5 and 6: it is
never coming back on its own, and nothing needs it. Generating one would mean
hardcoding a colour table inferred from one sample. Preserving the original was
also rejected — it is stale the moment anyone edits the body, and a page that
renders new text while a fallback consumer sees old text is worse than a clean
absence.

### The colour-faithful remap

**The map becomes a bijection that matches GitHub's own colours:**

| alert | GitHub | publishes as | ADF panel | recovered as |
|---|---|---|---|---|
| NOTE | blue | `<ac:structured-macro ac:name="info">` | `info` | NOTE |
| TIP | green | `<ac:structured-macro ac:name="tip">` | `success` | TIP |
| IMPORTANT | purple | `<ac:adf-extension>` `panel-type=note` | `note` | IMPORTANT |
| WARNING | orange | `<ac:structured-macro ac:name="note">` | `warning` | WARNING |
| CAUTION | red | `<ac:structured-macro ac:name="warning">` | `error` | CAUTION |

Three things fall out at once. Every alert now publishes the colour GitHub
draws it in, so a page looks the same on both sides. Nothing is many-to-one any
more, so **`CAUTION` stops being unrecoverable** and `README.md:438`'s
statement of that goes away. And the purple panel — the construct this whole
plan is about — gets the one job it is actually right for.

**This is why the purple read branch is not speculative.** markfluence now
emits a purple extension, so reading one back as `IMPORTANT` is required for
L5, not insurance. It is the only `type="panel"` branch in the change.

**No branch for the other four panel types.** An extension carrying
`panel-type` `info`/`success`/`warning`/`error` is not something Confluence
produces (finding 9) and not something markfluence emits, so converting it
would be unreachable code. Those pass through with the rest.

**The two maps stay separate and disagree on purpose.** Per finding 1,
`warning` is a key in the macro vocabulary (red, CAUTION under the new map) and
in the panel vocabulary (yellow, WARNING). There is deliberately no shared map,
and a test pins the disagreement so a later tidy-up cannot merge them.

**Published pages change appearance on their next `update`.** IMPORTANT goes
yellow→purple, WARNING goes red→orange, CAUTION stays red. markfluence is
unreleased, so this is a free correction rather than a migration — but it is an
intended, visible output change and the commit message says so.

### Everything else

**L5 stays Partial; its stated reason is corrected.** Multi-page export is
still missing, so the status does not move. But the paragraph's claim that
"single-page export's round-trip already works" is false — finding 7 is a
counterexample this change removes.

**No new guarantee id.** This is a defect against L5 plus an output change, not
a new property.

## Implementation

### `internal/convert/callouts.go`

`calloutMacro` becomes a target table, because IMPORTANT no longer names a
macro:

```go
// calloutTarget is what a GitHub alert publishes as. Exactly one field is set:
// Confluence has a macro for four of the five colours GitHub draws alerts in,
// and purple exists only as an ADF panel (docs/confluence/storage-format.md).
type calloutTarget struct {
    macro     string // ac:name on an ac:structured-macro
    panelType string // panel-type on an ac:adf-extension
}

// calloutTargets maps a GitHub alert to the Confluence construct that renders
// in the same colour GitHub uses. Bijective -- every alert has its own target
// and every target its own alert -- which is what makes read/export able to
// recover the alert exactly.
var calloutTargets = map[string]calloutTarget{
    "note":      {macro: "info"},      // blue
    "tip":       {macro: "tip"},       // green
    "important": {panelType: "note"},  // purple; no macro exists
    "warning":   {macro: "note"},      // orange/yellow
    "caution":   {macro: "warning"},   // red
}
```

`calloutTransformer` stores the **alert type** in the `calloutAttr` node
attribute rather than the macro name, so the renderer decides the element.

### `internal/convert/renderer.go`

`renderBlockquote` looks the alert up and writes one of two shapes:

```go
<ac:structured-macro ac:name="%s" ac:schema-version="1"><ac:rich-text-body>  …  </ac:rich-text-body></ac:structured-macro>

<ac:adf-extension><ac:adf-node type="panel"><ac:adf-attribute key="panel-type">%s</ac:adf-attribute><ac:adf-content>  …  </ac:adf-content></ac:adf-node></ac:adf-extension>
```

No `ac:adf-fallback` and no `local-id` — findings 4-6.

### `internal/convert/storage_to_md.go`

```go
// calloutMacroInverse maps a Confluence callout macro back to a GitHub alert.
// Now one-to-one (calloutTargets is bijective), so nothing is unrecoverable.
var calloutMacroInverse = map[string]string{
    "info":    "NOTE",
    "tip":     "TIP",
    "note":    "WARNING",   // was IMPORTANT
    "warning": "CAUTION",   // was WARNING
}

// adfPassthrough returns the ac:adf-extension to serialize: a copy without the
// ac:adf-fallback (a second rendering of the content the node already carries;
// Confluence regenerates it for export_view on demand and does not store one
// back) and without the node's server-generated local-id. An extension with no
// ac:adf-node is returned unchanged: the fallback is then the only copy there
// is, and dropping it would silently delete the content.
func adfPassthrough(n *snode) *snode

// adfAttr reads an <ac:adf-attribute key="..."> child's text.
func adfAttr(node *snode, key string) string
```

`renderBlock` and `inlineString` each gain:

```go
case "ac:adf-extension":
    // A purple panel is the one extension with a markdown spelling: it is what
    // MdToConfluence publishes for IMPORTANT. Everything else passes through.
    if node := findChild(n, "ac:adf-node"); node != nil &&
        node.attrs["type"] == "panel" && adfAttr(node, "panel-type") == "note" {
        return r.renderCallout(node, "IMPORTANT")   // block context only
    }
    return r.renderRawBlock(adfPassthrough(n))      // serialize(...) when inline
```

`renderCallout` takes the alert directly instead of looking a macro up, and
finds its body under `ac:rich-text-body` *or* `ac:adf-content`, so both
spellings of a callout produce identical markdown from one code path.

`renderRawBlock`'s content-container set is tested in two places; extract
`isContentContainer(name string) bool` so the set exists once.

`adfPassthrough` copies rather than mutating — `headingSlugs` has already
walked the tree and `renderBlock` may be re-entered.

## Tests

- **`testdata/regression/callouts/`** — regenerate. The golden now shows the
  new colour targets and an `ac:adf-extension` for IMPORTANT. This is the
  clearest single artifact of the remap; review it closely.
  `make regen-regressions`.
- **`testdata/storage2md/callouts/`** — regenerate: `note`→WARNING,
  `warning`→CAUTION, and a purple extension → `> [!IMPORTANT]`.
- **`testdata/storage2md/adf-panel/`** — new. The QBR page's first panel
  verbatim (node + fallback + `local-id`), a non-panel extension, and an
  extension with a fallback and no node. Pins that content appears once, that
  the body is markdown, and that no `local-id` survives.
- **`testdata/regression/adf-panel/`** — new, the write half for the
  *passthrough* form: `main.md` holds a hand-written non-panel extension and
  the golden asserts `MdToConfluence` reproduces it through the shield.
- **Round-trip unit test** — for each of the five alerts, `MdToConfluence` then
  `StorageToMarkdown` returns the same alert. This is the property the remap
  buys and the one thing that would catch a half-applied change to either map.
- **Unit** — the maps disagree on purpose: `calloutMacroInverse["warning"]` is
  `CAUTION` while panel type `warning` is orange/WARNING, and `"note"` means
  different colours in each vocabulary. Without this a later tidy-up merges
  them and silently repaints panels.
- **Unit** — `adfPassthrough`: fallback dropped, `local-id` dropped,
  `panel-type` kept, node-less extension returned intact, input not mutated.
- `make check`.
- **One live round trip**, pasted into the PR body: publish a file with all
  five alerts, confirm the colours in the editor, `export`, `update` from the
  export, and diff the stored storage. Expect equality modulo server-generated
  ids. Then the same for the QBR page's editor-authored purple panel.

## Docs

- **`docs/confluence/storage-format.md`** — new section holding findings 1-9:
  the macro ↔ `panelType` table, the vocabulary collision, purple being the
  only extension, the fallback being a regenerable cache, and Confluence's own
  serializer picking the macro. Written as a trap, in the register the file
  already uses for `data-layout` and cell colour, and carrying two: a fallback
  looks like content and is not, and `note`/`warning` mean different colours
  depending on which vocabulary you are in.
- **`docs/guarantees.md`** — amend the L5/L6 paragraph. Status stays Partial;
  the sentence claiming single-page round-trip already works gains this
  construct as the second thing that was breaking it, with finding 7's
  measurement.
- **`README.md`** — line 1131's "become info/tip/note/warning panels" becomes
  the five-row colour table; line 438's "`CAUTION` alerts … cannot be
  recovered" is deleted, since it no longer is. Table cell background colours
  stay listed as lossy.
- **Deliberately not `CLAUDE.md`.** Close call — flagged so it can be
  overridden on review — but the `internal/convert` bullet is already the
  longest in the file and it already directs a reader to `docs/confluence/`.

## Commits

1. `docs(plans): plan ac:adf-extension passthrough and a colour-faithful callout map`
2. `docs(confluence): how ADF panels serialize, and what an adf-fallback is`
3. `fix(convert): render an ac:adf-extension once, from its node` — the
   passthrough half. Closes #125.
4. `feat(convert): publish each GitHub alert in GitHub's own colour` — the
   remap, both directions, regenerated goldens. Notes the visible output
   change.
5. `docs: the callout colour map, and L5's single-page claim` — README +
   `guarantees.md`.

## Out of scope

- Publishing an alert's name as the macro's `title` parameter, which `mark`
  does and markfluence does not, so Confluence draws it as a styled header
  rather than as the body's first line. Noticed while reading `mark`; worth its
  own issue.
- A property test for L5. The Laws table says laws are verified by property
  tests, there is none for L5, and that is why #125 went unnoticed.
- Every other unknown storage element still falling through the transparent
  wrapper. The default is right for a genuine wrapper; `ac:adf-extension` is
  the one known element for which it is wrong.
