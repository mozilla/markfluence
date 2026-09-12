# Links and heading anchors

## The anchor scheme

markfluence rewrites a heading anchor with `confluenceSlug`: preserve case and
punctuation, collapse runs of whitespace to single hyphens. For a heading
`Café Section` that gives `#Café-Section`.

**Verified 2026-08-07** in a browser, on a published page with a heading
`Café Section`:

- Confluence's own "copy link" for that heading ends `#Caf%C3%A9-Section` —
  which is `Café-Section` percent-encoded.
- Links written by markfluence as `#Café-Section` (unencoded) jump to the
  section. The browser encodes on navigation, so both forms resolve.

Non-ASCII headings need no special handling. Confluence keeps the `é`.

> **Do not verify this with `body-format=view`.** It reports heading ids of the
> form `pagetitlewithnopunctuation-HeadingTextWithNoSpaces` — for this page,
> `markfluencelinkandimageencodingprobe-CaféSection`. That looks like proof that
> `confluenceSlug` is wrong for every heading containing a space, ASCII included.
> It is not; `view` is a legacy renderer. This cost real time on 2026-08-07 and
> produced a confident wrong conclusion that would have "fixed" working code.

Two slug functions exist because the *source* anchor is GitHub's and the
*target* is Confluence's: `githubSlug` lowercases and hyphenates to match what a
`#fragment` in the markdown refers to, and `confluenceSlug` produces what
Confluence will answer to. The map from one to the other is built by scanning
sibling files' headings.

## Destinations are URLs

Both halves of a link — the path and the fragment — arrive percent-encoded and
must be decoded before being matched against anything on disk.

A link to a file named `my doc.md` is spelled `my%20doc.md`, and the page and
anchor maps are keyed by the filename as read off disk, so an undecoded lookup
silently misses. A non-ASCII fragment arrives as `#caf%C3%A9-section` and must
be decoded before it will match a `githubSlug`, which is Unicode-aware.

This was issue **#62**; `docKey` in `internal/convert/links.go` is now the single
lookup key. See `internal/convert/destination.go` for the codec, which images
share.

**Failure here is silent.** An unresolved link is published verbatim as a
relative href — `<a href="my%20doc.md">` — which resolves to nothing on a
Confluence page. Nothing warns, and the command exits 0. Unlike a broken image,
which at least prints `IMAGE BROKEN`.

## A same-page anchor needs two publishes

**Verified 2026-08-07.** On `create`, a same-page anchor comes out as an
unresolved relative href, because rewriting it to a full URL requires the page's
own id and the page has no `page_id` until `create` writes one back. The next
publish resolves it.

Not obviously worth fixing — a second publish is cheap — but it surprises anyone
who checks their links immediately after `create`.

## `<ac:link>`: the editor's own internal link

Everything above is the *forward* direction, where markdown becomes an
`<a href>`. The editor never writes one of those for an internal link. It writes
`<ac:link>`, which `read` and `export` have to read back.

### What it looks like in the wild

**Surveyed 2026-08-21**, the 500 most-recently-modified pages on mozilla-hub
(`GET /wiki/api/v2/pages?sort=-modified-date&body-format=storage`), tallying
every `<ac:link>` by its attributes and its child elements:

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

Four things worth knowing before touching the mapping:

**A mention is 80% of all usage** — `<ac:link><ri:user ri:account-id="…"
ri:local-id="…" /></ac:link>`, no body. Whatever happens to mentions decides
whether handling `<ac:link>` helps or hurts on a typical page.

**`ac:anchor` is percent-encoded**, unlike everything `confluenceSlug` produces.
A real value: `Workstream-2%3A-Cross-functional-%E2%80%9CHow-to-Felt-Privacy%E2%80%9D-guidance`.
So the two spellings of "the same" anchor differ by encoding, and an anchor has
to be decoded before it will match a heading — the same trap as the fragment in
*Destinations are URLs* above.

**A body is optional and has two spellings**: `ac:link-body` (rich text) and
`ac:plain-text-link-body` (CDATA). With neither, Confluence displays the
target's own title.

**`<ac:link>` occurs inside macro parameters** (the `pagetree` macro's `root`),
where it is a value and not a link. Those reach `serialize` through the
unknown-macro path and never the renderer, which is what keeps them intact.

### A mention needs only the account id — verified 2026-09-11

The question #91 turns on: can a mention be reconstructed from the account id
alone, or does `ri:local-id` carry something? If the local-id were required,
nothing markdown can hold would republish as a mention and passthrough would be
the only honest answer.

Three spellings published to a scratch page and read back as **ADF**
(`body-format=atlas_doc_format`), which is what the editor itself uses:

| published storage | ADF node |
|---|---|
| `<ac:link><ri:user ri:account-id="60c36…" ri:local-id="4df0b1cc-…" /></ac:link>` | `mention` `{"id":"60c36…","localId":"4df0b1cc-…","text":"@William Kahn-Greene"}` |
| `<ac:link><ri:user ri:account-id="60c36…" /></ac:link>` | `mention` `{"id":"60c36…","text":"@William Kahn-Greene"}` |
| the same, self-closing `<ac:link>` | identical to the above |

**The local-id is not required**, and its absence changes nothing a reader sees:
the mention resolves to the same display name either way. So it belongs in
`droppedAttrs`' category — a per-instance server-generated id — and dropping it
on republish is correct rather than merely tolerated.

Read as **ADF on purpose**. `body.storage` came back byte-identical for all
three, and this file's own warning is that storage proves only what was stored;
a `mention` node in ADF is what proves Confluence understood it as a mention
rather than inert markup it happened to keep.

### Two things the same probe turned up

**ADF names every mention, for free.** Each node carries
`attrs.text` — `"@William Kahn-Greene"` — so one ADF fetch of a page names every
person mentioned on it, with no per-user lookup at all. That is a better answer
than the bulk user route #91 wondered about. It is not free-free: `body-format`
takes **one** value, and asking for two is the silently-discarded-filter trap
again — `body-format=storage,atlas_doc_format` answers **200 with an empty
`body` object** rather than an error. So harvesting names from ADF costs a
second page fetch, against one `GET /user` per distinct account id.

**An unresolvable account id publishes happily.** A syntactically plausible but
nonexistent id (`712020:00000000-0000-0000-0000-000000000000`) was accepted,
stored verbatim, and renders as a `mention` reading **`@Unlicensed user`**.
Meanwhile `GET /wiki/rest/api/user?accountId=…` answers **404** for it
(`No user found with key : null`), so `GetUser`'s best-effort `""` is the
signal that an id cannot be resolved.

Both directions follow from that. Inbound, an id `GetUser` cannot resolve has no
name to render, so it passes through as storage. Outbound, **Confluence will not
tell you an id is wrong** — a mangled one publishes as `@Unlicensed user`
instead of failing — so a markdown mention whose id no longer resolves is worth
reporting locally before publishing it, since the server never will.

### The mapping

One rule: **convert when the markdown republishes to a link resolving to the
same target; pass the storage through when it would not.** Passthrough is not a
failure — the `ac:`/`ri:` shield republishes it byte-identical — so it is the
right answer wherever a markdown link would break.

| storage | markdown | why |
|---|---|---|
| `ri:page` (+`ri:space-key`, +`ac:anchor`), resolved | `[body](URL#anchor)` | absolute URL, republishes to the same page |
| `ri:page`, unresolved | passthrough | no URL to write |
| `ac:anchor` alone, heading recovered | `[body](#github-slug)` | the forward path rewrites `#slug` through the anchor map |
| `ac:anchor` alone, no matching heading | passthrough | a `#slug` matching no heading publishes as a dead relative href, silently |
| `ri:space` | `[body](SITE/wiki/spaces/KEY)` | absolute URL, no lookup needed |
| `ri:attachment` | passthrough | only images are uploaded, so `[x](Deck.ppt)` would publish as a dead relative href |
| `ri:user`, name resolved | `[@Display Name](SITE/wiki/people/{accountId})` | the id round-trips in the URL; only the account id is required to republish it as a mention (#91) |
| `ri:user`, name unresolved | passthrough | no name to render, and the id alone says nothing to a reader |
| `ri:blog-post` | passthrough | `SearchPagesByTitle` does not see blog posts |
| no target at all | passthrough | nothing to link to |

`ac:card-appearance` is dropped: an inline card renders as a chip with the page
icon, and a markdown link republishes as plain text. The target is unchanged and
the link still resolves, so by the rule above it converts. The design target has
always been semantic, not byte-for-byte.

`<ri:page>` names a **title**, never an id, so a URL takes a lookup —
`pagedoc.PageLinks`, since `internal/convert` holds no client. A same-page
anchor takes no lookup but cannot be inverted either: `confluenceSlug` turns
both a space and a hyphen into `-`, so `DOM-Security-Team` could have come from
either. The heading that produced it is in the document being converted, so it
is recovered by matching rather than by string surgery.

### The trap: `xml.HTMLAutoClose` swallows `<ac:link>`

`encoding/xml` matches an auto-close name against `Name.Local` alone and ignores
the namespace prefix:

```go
for _, s := range d.AutoClose {
    if strings.EqualFold(s, t1.Name.Local) {
```

`xml.HTMLAutoClose` contains `link`, the HTML void element. So `<ac:link>` was
closed the instant it opened, its children became siblings, and the real
`</ac:link>` arrived against an empty stack:

```
✗ XML syntax error on line 1: unexpected end element </link>
```

`Strict = false` does not help. Non-strict mode invents *missing* end tags and
tolerates mismatched ones; it cannot absorb a surplus one. This was issue **#88**,
and it made `export` and `read` fail outright on any page carrying an
editor-authored internal link.

`link` is the only collision in that list — `ac:image` ≠ `img`, `ac:parameter` ≠
`param`, and every `ri:*` element is self-closing — and `<link>` is a `<head>`
element that never appears in a storage body, so `autoCloseElems` in
`internal/convert/storage_to_md.go` drops just that entry.
