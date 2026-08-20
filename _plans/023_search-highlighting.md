# Plan: highlight matched terms in `search` output

Show which terms each `search` hit matched on.

## `excerpt=highlight` is already requested, and immediately stripped

Not a new capability. `search` already asks for `excerpt=highlight`
(`excerptMode`, `internal/client/search.go`), and the server already wraps every
matched term in `@@@hl@@@` / `@@@endhl@@@`. `cleanExcerpt` strips those markers on
the next pass.

So the work is: stop discarding the positions, carry them to the command, and
render them. The query does not change, the request does not change, and no new
call is added.

## What was probed, and what it established

### The markers record what the server matched, not what was typed

Live query, `siteSearch ~ "deploy runbook" and text ~ "deploy runbook" and type = page`,
`excerpt=highlight`, three rows:

```
row.title    : '@@@hl@@@Runbook@@@endhl@@@: Grafana @@@hl@@@deploys@@@endhl@@@'
content.title: 'Runbook: Grafana deploys'
excerpt      : 'Grafana @@@hl@@@deploys@@@endhl@@@\nStage @@@hl@@@deploys@@@endhl@@@
                happen when Helm chart changes are landed...'
```

Note what got marked. The query said `deploy`; the server marked `deploys` and
`Deploy`. It said `runbook`; the server marked `Runbook`. **The markers carry the
server's stemming and case folding**, which is what rules out matching the
query terms ourselves.

`docs/confluence/search.md` already records the sampling: of 50 rows fetched with
`excerpt=highlight`, **40 carried markers**. So most hits highlight and some do
not.

### ANSI already disappears when output is not a terminal

Measured against `ui.Error` (red), counting ESC bytes:

| | ESC bytes |
|---|---|
| piped | **0** |
| pty | **2** |
| pty + `--no-color` | **0** |
| pty + `NO_COLOR=1` | **0** |

lipgloss/termenv does this on its own. Nothing needs to be written for it, and
`--no-color` already works because `root.go` turns it into `NO_COLOR=1` before any
output happens.

The constraint that follows: **highlighting must go through a lipgloss style in
`internal/ui`**, never a hand-rolled `\033[`. A hand-rolled escape would be the
one thing in the codebase that ignores `--no-color` and corrupts piped output.

## Decisions locked

### Render the server's markers; do not match the query terms

The alternative — take the query string and highlight occurrences of its words —
fails three ways. It would have to reimplement Confluence's stemming to highlight
`deploys` for `deploy`. It has nothing to work from under `--cql`, where there is
no list of user terms at all, yet the server still returns markers. And it would
highlight text the server never matched on, claiming a reason for the hit that
the server did not give.

### `Excerpt` stays exactly as it is; spans travel beside it

Two existing rules make this the only safe shape.

`cleanExcerpt` cleans **in the client** so that "the human output and `--json`
cannot disagree about it" (plan 022). And `excerpt` is schema-locked
(`schema/json-output/v1.json`). Moving marker handling into `cmd/search` would
break the first; letting styled text reach the JSON path would break the second.

So `client.SearchMatch` keeps `Excerpt` byte-for-byte as today, and gains a
parallel field:

```go
// ExcerptSpan is one run of excerpt text, flagged when Confluence marked it as a
// matched term. Concatenating every Text yields Excerpt exactly.
type ExcerptSpan struct {
	Text  string
	Match bool
}
```

`SearchMatch.Spans []ExcerptSpan`. Spans over byte offsets because the
reassembly invariant — concatenating `Text` reproduces `Excerpt` — is one cheap
test that catches every drift, where offsets need bounds-checking at render time
and go wrong silently.

This does not touch the schema. `cmd/search/json.go`'s `jsonSearchResult` is a
separate struct that simply will not carry the new field, so
`schema/json-output/v1.json` does not move and `TestSchemaConformance` is
unaffected.

### Compute spans during cleaning, not by mapping offsets afterward

Cleaning is strip → unescape → collapse, and the last two both change length.
`&#39;` becomes one byte from five; a run of whitespace becomes one space. Any
attempt to locate marker positions in the raw string and map them onto the
cleaned string has to model both transformations, and gets it wrong on the rows
that carry entities *and* newlines *and* markers — which the sampling says is the
common row, not the corner case.

Build the flags alongside the text instead:

1. Walk the raw excerpt, splitting on the markers into `(segment, match)` pairs.
2. `html.UnescapeString` each segment, appending its runes with the segment's
   flag. Unescaping stays per-segment and still happens after stripping, exactly
   as today.
3. Collapse whitespace over the flagged runes together. A collapsed space is a
   match **only if the runes on both sides of it are matches**, so
   `@@@hl@@@foo bar@@@endhl@@@` highlights as one block while a space between a
   matched and an unmatched word stays unmatched. Trim the ends.
4. Coalesce adjacent runes with equal flags into spans.

`Excerpt` is then the concatenation, produced by the same pass, so the two cannot
drift.

### Highlight with bold

`internal/ui` already has `bold`, and the four colors it defines are all
semantically spoken for: green success, yellow warning, red error, gray dim.
Reusing one of those would say something false about a matched word. Bold adds no
palette entry, survives light and dark terminals, and degrades to nothing when
color is off.

New helper `ui.Match(s string) string`, returning styled text rather than
printing, since `blocks()` builds a string.

Recorded alternative: reverse video reads as a selection and is louder than a
search result warrants.

### `blocks()` renders through an injected highlighter, so it stays testable

Styling inside `blocks()` would be untestable as written. `blocks()` is
unit-tested by comparing its returned string against expected output, and those
tests run with stdout not a terminal — where lipgloss emits **no** escape codes. A test asserting
highlighted output would pass against completely unhighlighted text.

So the rendering splits in two:

- a pure `renderSpans(spans []client.ExcerptSpan, hl func(string) string) string`,
  tested with a visible stand-in such as `func(s string) string { return "[" + s + "]" }`;
- `blocks()` calling it with `ui.Match`.

The span computation is tested in the client against the real observed payloads.
Neither test depends on a terminal.

## Out of scope (deliberately)

- **Highlighting the title.** The row-level `title` carries markers, but
  markfluence deliberately reads `content.title`, because the row title is
  HTML-escaped *and* marked — `internal/client/search.go` says "Nothing here may
  be switched back to the row." Highlighting titles means deriving spans from one
  string and mapping them onto a differently-escaped other one. That is a
  separate decision, and the excerpt is where the question "why did this match?"
  is answered.
- **Spans in `--json`.** A schema change, for data a consumer can neither render
  nor act on. Addable later if something asks.
- **A `--highlight`/`--no-highlight` flag.** `--no-color` already turns it off,
  along with everything else colored. Nothing asks for a second switch covering
  one command.
- **Wrapping the excerpt to terminal width.** Out of scope in plan 022 for the
  same reason and unchanged here: `internal/ui` has no width detection.
- **Anything about the query.** Clause order stays pinned. This plan does not go
  near `buildTextCQL`.

## Steps

1. `docs(plans): plan search term highlighting` — this file.
2. `feat(client): carry highlight spans on a search match` — `ExcerptSpan`,
   `SearchMatch.Spans`, `cleanExcerpt` reworked to produce text and flags in one
   pass, and its tests. `Excerpt` output must be unchanged, which the existing
   cleaning tests already assert.
3. `feat(ui): add a highlight style for matched terms` — `ui.Match`.
4. `feat(search): highlight matched terms in excerpts` — `renderSpans`, wired
   into `blocks()`.
5. `docs: note excerpt highlighting` — the README's `search` section, and the
   `cmd/search/` bullet in CLAUDE.md.

## Testing

**Span computation**, against the real payload shapes from
`docs/confluence/search.md` rather than invented ones: markers alone; markers plus
`&#39;`; markers plus embedded newlines; all three together; a marked run
containing a space; adjacent marked runs; a marked term at the very start and at
the very end; an excerpt with no markers at all (one span, `Match` false); an
empty excerpt (no spans).

**The reassembly invariant**: for every case above, concatenating each span's
`Text` equals `Excerpt`. This is the test that keeps the two fields honest.

**Unchanged `Excerpt`**: the existing cleaning tests must pass untouched. If any
expected string in them changes, the rework broke the canonical excerpt and the
`--json` output moved with it.

**Rendering**: `renderSpans` with a visible stand-in highlighter — spans in order,
only `Match` spans wrapped, no wrapping of an empty span, a spanless excerpt
rendering as plain text.

**Command**: a hit whose excerpt has no markers still prints its excerpt line; a
hit with an empty excerpt still prints no excerpt line; `--json` output is
byte-identical to before the change, which is the regression that matters most.

**Not tested, on purpose**: that ANSI appears on a terminal. Unit tests do not run
on one, lipgloss's degradation is measured above, and a test that shells out to a
pty to assert escape codes would be testing lipgloss rather than markfluence.
