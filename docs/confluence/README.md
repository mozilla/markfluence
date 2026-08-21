# What we know about Confluence

Atlassian does not document the storage format, and documents parts of the REST
API loosely enough that markfluence's behavior rests on things we established by
experiment. This directory is where those findings live so they survive the
person who ran the experiment.

- [api.md](api.md) — auth, the gateway, v1/v2, pagination, downloads, retries
- [attachments.md](attachments.md) — names, comments, what round-trips
- [storage-format.md](storage-format.md) — tables, macros, what Confluence rewrites
- [links-and-anchors.md](links-and-anchors.md) — heading anchors and page links
- [page-width.md](page-width.md) — the content properties behind `page_width`
- [folders.md](folders.md) — the Cloud folder type, and why child listing is v1
- [search.md](search.md) — finding content by title and by full text, and `/search`'s paging traps

## How to read an entry

Every claim carries its provenance, because "someone wrote this down once" and
"I watched this happen" deserve different amounts of trust:

- **Verified `DATE`** — observed directly, with the observation described. Re-runnable.
- **Transcribed** — carried over from a code comment or commit message. Believed
  accurate and probably verified when written, but not re-checked.
- **Unverified** — asserted somewhere, not confirmed, with the reason it wasn't.

When you verify something, say how. A claim you cannot reproduce from the note is
a claim the next person has to establish from scratch.

## Four traps that produce confident, wrong answers

All of these have cost real time. Read them before designing an experiment.

### A filter you did not verify may not have been applied

Confluence can **silently discard a clause** and answer a broader question with a
200. [search.md](search.md) documents the case: a `siteSearch` clause in the
middle of three is dropped, so a space-scoped search returns every page in the
space, ranked plausibly, with no error.

What made that ship was the probe, not the bug. It was checked against a space
holding **three pages**, where "every page in the space" and "a real result set"
are the same small number — so the broken query returned 3 and looked correct. The
honest answer was 1.

So when probing anything that **narrows** a result set, always compare against the
unnarrowed count, and do it in a corpus big enough for the two to differ:

- Run the query without the filter. If the filtered and unfiltered totals match,
  assume the filter did nothing until proven otherwise.
- Pick a target where the filter should change the number by an order of
  magnitude. A 3-row space proves nothing.
- Check the *contents*, not just the count. The tell was a top hit that did not
  contain the search term anywhere.

### `body-format=view` is not what the browser renders

The REST API's `view` body format is a legacy renderer. Its output can differ
from the live page in ways that look authoritative and are not.

Two cases we hit, both on 2026-08-07:

- **Heading anchors.** `view` reports ids like
  `pagetitlewithoutpunctuation-HeadingTextWithoutSpaces`. The live page uses a
  different scheme entirely (see [links-and-anchors.md](links-and-anchors.md)).
  Reading `view` produces a tidy proof that `confluenceSlug` is broken for every
  heading containing a space. It isn't.
- **Table cell alignment.** `view` happily echoes `<td align="center">` and
  `style="text-align: …"`, suggesting Confluence honors both. ADF shows it
  honors only one of them.

### Storage is not the renderer

Confluence stores much more than it renders. `body.storage` will faithfully
return an attribute the renderer ignores completely — including values that are
outright invalid.

So **`body.storage` can only prove what was stored, never what takes effect.**
To find out what Confluence actually does with markup, read
`body-format=atlas_doc_format` (ADF), which is the model Cloud renders from.
For anything visual that ADF cannot settle — how wide a table draws, whether a
link scrolls — open the page in a browser.

### An auth failure can arrive wearing another status

A rejected credential is a **404** on every v2 route — not 401, not 403 — with a
body that is a perfectly ordinary "not found". A revoked token therefore makes
`read` report `page 2848423944 not found` about a page that exists, and the
obvious next move is to go and check page ids that were all correct.

The tell is that **a genuine v2 404 names what it could not find** and the
authentication one does not. Full table, and what a 401 and a 403 each actually
mean here, in [api.md](api.md#scopes).

So when a probe returns "absent", confirm the credentials reached the API before
believing it. A status code is evidence about the response, not about the cause.

## Verifying against a real instance

Point a `.env` at a site you can write to, publish a scratch page, and read it
back — `body-format=atlas_doc_format` for anything about rendering, a browser for
anything visual.

**Probes are ephemeral. Delete them when you are done.** The finding is the
artifact worth keeping; the `.md` file and the page it published are scratch.
A probe `.md` also hard-codes a `page_id` in whoever ran it, so keeping it around
means the next person's `update` writes to someone else's page.

That puts one obligation on whatever you write here: **the entry has to stand on
its own.** Say what was sent and what came back, concretely enough that someone
can reconstruct the experiment from the note alone.

Do not cite page ids. They mean nothing on anyone else's instance, and the page
they point at is scratch that should already have been deleted. Describe the
setup instead — "a page with three attachments", "a single-column table with a
200px colgroup" — which is what someone re-running the experiment actually needs.
