# Plan: convert `<ac:link>` on the way back (#88)

`<ac:link>` is how the Confluence editor writes an internal link. `MdToConfluence`
never emits one, so nothing in the regression suite exercises it, and
`storage_to_md.go` has no case for it. Two things are wrong as a result.

## 1. It breaks the parse

`export`/`read` on any page with an `<ac:link>` that has children fails outright:

```
✗ XML syntax error on line 1: unexpected end element </link>
```

`parseStorage` sets `dec.AutoClose = xml.HTMLAutoClose`, whose list is
`[basefont br area link img param hr input col frame isindex base meta]`. Go
matches that list against `Name.Local` only:

```go
for _, s := range d.AutoClose {
    if strings.EqualFold(s, t1.Name.Local) {
```

The namespace prefix is ignored, so `<ac:link>` collides with the HTML void
element `link` and is auto-closed the instant it opens. Its children become
siblings, the stack desynchronizes, and the real `</ac:link>` arrives against an
empty stack. `Strict = false` cannot rescue this: non-strict mode invents missing
end tags and tolerates mismatched ones, but an end element with nothing on the
stack is a hard error in both modes.

Token trace on page 2820571155, storage
`<ac:link><ri:page ri:content-title="How to: …" ri:version-at-save="12" /><ac:link-body>Requesting Yardstick access</ac:link-body></ac:link>`:

```
START ac:link      @2495
END   ac:link      @2504   <- auto-closed before ri:page is read
START ri:page      @2614
END   ri:page      @2614
START ac:link-body @2614
END   ac:link-body @2655
END   p            @2670
ERR: unexpected end element </link>
```

`link` is the only collision in the list: `ac:image` ≠ `img`, `ac:parameter` ≠
`param`, and every `ri:*` element is self-closing. `<link>` is a `<head>` element
and never legitimately appears in a storage body, so dropping just that one entry
costs nothing and keeps `<br>`/`<hr>` auto-closing for pasted content.

## 2. Once it parses, the target is silently lost

`<ac:link>` falls through to `renderInline`'s default, which renders children
inline: only the body text survives. That is the inverse of the forward
direction's silent dead-link failure, and worse here, because `read`/`export`
output is meant to be publishable back with `update` — a dropped target is a real
edit to the page the next time it is published.

## What the storage actually contains

Not assumed. Surveyed the 500 most-recently-modified pages on
mozilla-hub (`GET /wiki/api/v2/pages?sort=-modified-date&body-format=storage`),
tallying every `<ac:link>` by its attributes and its child elements:

| count | attributes | children |
|---|---|---|
| 3346 | — | `ri:user` |
| 632 | — | `ri:page`, `ac:link-body` |
| 143 | `ac:local-id`, `ac:card-appearance` | `ri:page`, `ac:link-body` |
| 48 | `ac:anchor` | `ri:page`, `ac:link-body` |
| 8 | `ac:local-id`, `ac:card-appearance`, `ac:anchor` | `ri:page`, `ac:link-body` |
| 6 | `ac:anchor` | `ac:link-body` |
| 5 | — | `ri:space`, `ac:link-body` |
| 5 | — | `ri:page` |
| 2 | — | `ac:link-body` |
| 1 | — | *(self-closing, inside an `ac:parameter`)* |

A separate 60-page sample also turned up `ri:attachment` with
`ac:plain-text-link-body`, and `ri:page` with `ac:plain-text-link-body`.

Four things this establishes that guessing would not have:

**A mention is 80% of all usage.** `<ac:link><ri:user ri:account-id="…"
ri:local-id="…" /></ac:link>`, no body. Whatever happens to mentions dominates
whether this feature helps or hurts.

**`ac:anchor` is percent-encoded.** Real value:
`Workstream-2%3A-Cross-functional-%E2%80%9CHow-to-Felt-Privacy%E2%80%9D-guidance`.
The forward path's `confluenceSlug` collapses whitespace to hyphens and encodes
nothing, so the two spellings of "the same" anchor differ, and an anchor must be
decoded before it can be matched against a heading.

**A body is optional, and comes in two spellings.** `ac:link-body` (rich) and
`ac:plain-text-link-body` (CDATA). With neither, Confluence displays the target's
own title.

**`<ac:link>` occurs inside macro parameters** (the `pagetree` macro's `root`).
Those must not become markdown links. They already cannot: an unknown macro goes
through `renderRawBlock`/`serialize`, so the walk never reaches `renderInline`
for anything inside one. Worth a test so it stays that way.

## Decisions locked

### Convert when the republished link resolves to the same target; pass through when it would not

This is the rule the whole mapping follows, and every row below is an application
of it rather than a separate judgment call. Raw passthrough is not a failure
mode here: the forward path shields `ac:`/`ri:` tags, so passed-through storage
republishes byte-identical and keeps working. It is the same escape hatch the
file's header already documents for unknown macros.

| storage | markdown | why |
|---|---|---|
| `ri:page` (+`ri:space-key`, +`ac:anchor`), resolved | `[body](URL#anchor)` | absolute URL, republishes as `<a href>` to the same page |
| `ri:page`, unresolved | passthrough | no URL to write |
| `ac:anchor` alone, heading recovered | `[body](#github-slug)` | the forward path rewrites `#slug` through the anchor map |
| `ac:anchor` alone, no matching heading | passthrough | a `#slug` matching no heading publishes as a dead relative href, silently |
| `ri:space` | `[body](SITE/wiki/spaces/KEY)` | absolute URL, no lookup needed |
| `ri:attachment` | passthrough | only images register attachments (`images.go:78`), so `[x](Deck.ppt)` would publish as a dead relative href |
| `ri:user` | passthrough | markdown has no mention |
| `ri:blog-post` | passthrough | `SearchPagesByTitle` does not see blog posts |
| no target at all | passthrough | nothing to link to |

Attachment links deserve better than passthrough — the file *is* exported, at its
recorded path, and a markdown link to it would preview locally. What blocks it is
the forward path, which uploads images only. File that as its own issue rather
than shipping a link that breaks on the next publish.

### The page id comes from a title lookup, and `internal/convert` stays client-free

`<ri:page>` names a **title**, not an id, and a Confluence URL needs the id. The
lookup therefore happens in `internal/pagedoc` (which already holds a client for
page width and attachments) and arrives at the converter as a map, exactly the way
`Sources` already does. `internal/convert` gains no client.

`StorageToMarkdown(storage, sources)` becomes `StorageToMarkdown(storage, opts)`
with a `StorageOptions{Sources, PageLinks, SiteURL}`. Three positional maps would
be unreadable, and the package is internal and unreleased, so there is no reason
to grow a second entry point instead.

The lookup is best-effort in the same way `Sources` is: guarded by a
`strings.Contains(storage, "<ri:page")` so a page without one costs nothing, and a
failure logs to `ui.Debug` and omits that entry, which falls back to passthrough.
A read is worth completing without it.

Space keys are resolved once and cached across targets — a page with five links
into the same space must not pay five `ResolveSpaceID` calls. `FindByTitle` is the
wrong call to reuse: it also runs the folder CQL half, which is a request per
target for rows that can never be a link target. Its page half factors out as
`PagesByTitle(title, spaceID)`, which `FindByTitle` then calls, so `contextURL`
stays private and the URL is still built from `SiteURL` rather than the gateway.

A title is unique among current pages in a space, so the lookup returns at most
one current page; prefer it over an archived page of the same title.

### An anchor is decoded to match a heading, and left encoded in a URL

Cross-page: the fragment is appended to the URL **verbatim**, still
percent-encoded, because that is what belongs in a URL and it is what Confluence
itself writes.

Same-page: decode it, then recover the original heading from the page being
converted — collect the document's own headings, key them by
`confluenceSlug(text)`, and emit `githubSlug(text)`. This is exact where inverting
the slug would not be: `confluenceSlug` loses the distinction between a space and
a hyphen, so `DOM-Security-Team` cannot be turned back into `DOM Security Team`
by string surgery, but it matches the heading that produced it. No match, no link.

### `ac:card-appearance` is dropped, on purpose

An inline card renders as a chip with the page icon; a markdown link republishes
as plain text. The target is unchanged and the link still resolves, so by the rule
above it converts. Recording it here so the loss is a decision rather than a
surprise; the design target has always been semantic, not byte-for-byte.

## Steps

1. `docs(plans): plan ac:link conversion` — this file.
2. `fix(convert): stop the XML parser auto-closing <ac:link>` — `autoCloseElems`,
   plus the parse test. This is the crash fix and stands alone.
3. `feat(client): add PagesByTitle` — factor the page half out of `FindByTitle`.
4. `feat(convert): convert <ac:link> to a markdown link` — `StorageOptions`,
   `PageLinkTargets`, the `ac:link` case and its heading map, both callers
   updated.
5. `feat(pagedoc): resolve the pages an <ac:link> points at` — `PageLinks`, the
   space-id cache, wired into `Render`.
6. `docs(confluence): record what <ac:link> looks like` — the survey table and
   the auto-close trap in `docs/confluence/links-and-anchors.md`.
7. `docs: note ac:link handling` — the `internal/convert` and `internal/pagedoc`
   bullets in CLAUDE.md.

## Testing

**The parse bug**, first and separately: `<p>x <ac:link><ri:page …/><ac:link-body>y
</ac:link-body></ac:link> z</p>` currently errors and must not. Assert on the
parse, not just the output, or a later refactor that swallows the error passes.

**Every row of the mapping table**, using the *real* fragments from the survey
above rather than invented ones — the mention with its `ri:local-id`, the card
with its `ac:local-id`, the percent-encoded cross-page anchor, the CDATA
plain-text body, the bodyless `ri:page`, the bodyless-and-targetless degenerate.

**Passthrough round-trips**: for each passthrough row, feed the emitted markdown
back through `MdToConfluence` and assert the storage comes out unchanged. That is
the entire justification for passthrough, and `TestRoundTripPassthrough` already
establishes the pattern.

**The lookup is best-effort**: a page whose title resolves to nothing renders as
passthrough rather than erroring, and a client failure does the same.

**No lookup when there is nothing to look up**: a body with no `<ri:page` makes
zero requests. The guard is the only thing keeping `read` cheap.

**`<ac:link>` inside a macro parameter** stays inside the serialized macro and
does not become a markdown link.

**Same-page anchors**: a heading that matches, a heading that does not (→
passthrough), and a percent-encoded anchor whose heading contains a colon and
curly quotes — the FIREFOX row above, which is the case that proves decoding
happens before matching.
