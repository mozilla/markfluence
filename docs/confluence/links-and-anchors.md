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
