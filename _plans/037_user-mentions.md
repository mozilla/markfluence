# Plan: render `<ri:user>` mentions readably, and republish them as mentions

Make a Confluence mention survive a round trip as something a reader can read.
Implements #91, and follows #88, which established the `<ac:link>` mapping and
the passthrough this replaces.

A mention is **80% of all `<ac:link>` usage** — 3346 of ~4200 occurrences in the
survey behind #88, against 783 page links. Today it passes through as raw
storage, which is round-trip safe and unreadable:

```
| FXMS-79 | 1pm | <ac:link><ri:user ri:account-id="712020:0000…" ri:local-id="4df0…" /></ac:link> | DONE |
```

The one thing a reader wants from a mention is *who*, and the account id is the
one thing the storage does not say. On the pages where mentions cluster —
on-call rotations, intake queues, meeting notes — most of the cells look like
that.

## The markdown spelling: A′

```markdown
Ping [@Ada Lovelace](https://home.atlassian.com/people/712020:0e5f…)
```

An ordinary markdown link to the profile URL, with the display name as the link
text and **`@` as the marker that makes it a mention**. A link to the same URL
whose text does *not* begin with `@` stays a plain link and publishes as an
`<a href>`.

The destination is **Atlassian Home, not the Confluence site.** Confluence's own
renderer still emits `{site}/wiki/people/{accountId}`, but that URL no longer
goes anywhere useful: in a browser it bounces through a separate login and lands
on a blank page. `https://home.atlassian.com/people/{accountId}` works, with no
`cloudId` parameter needed — it redirects to an org-scoped form and arrives.
Confirmed in a browser, which is the only thing that can confirm it: both of
these are client-rendered, and `curl` returns an identical 200 shell for a real
profile, a nonexistent one, and a URL that redirects to a blank page. Two wrong
conclusions were drawn from `curl` here before that was understood.

That the URL is **not site-scoped** is what makes this the good version rather
than the tolerable one. The markdown carries only durable facts — a globally
unique account id, and a display name that is admitted decoration — with no
site, no cloud id, and therefore no config dependency, no discovery request,
and one output shape on every machine. `CONFLUENCE_CLOUD_ID` is optional
config; a spelling that needed it would either make an optional setting
mandatory for this feature or produce different markdown depending on whose
config ran the export, which is the objection that rules out a disk cache too.

Four constraints picked this, and each one eliminates something:

**The account id has to survive.** A display name is not stable — people change
their names and the id does not — and the forward direction needs the id to emit
`ri:user` at all. That rules out a bare `@Ada Lovelace`, which would need a
name→id lookup on publish: ambiguous between two people with the same name, and
a typo would silently mention nobody.

**The name has to be visible**, which is the whole point, so passthrough plus a
decorative comment is not an answer.

**Both directions or neither**, per #88's rule: convert only when the markdown
republishes to a link resolving to the same target. A spelling that reads
nicely but cannot be recognised on the way back is a lossy rendering, and
passthrough beats that.

**It has to be a fixed point.** `TestRoundTripMarkdownIsAFixedPoint` means
export → publish → export has to be identical markdown.

### Why the `@`, and what it costs

Recognition is by **URL**, not by the text: the forward path matches
`{site}/wiki/people/{id}`. The `@` is what disambiguates *intent*, and there are
two real intents that otherwise collapse into one spelling — "mention this
person" and "link to this person's profile". Without the marker, someone who
deliberately wrote a plain profile link gets a mention instead, silently.

The cost is that link *text* now carries semantics, so stripping the `@` changes
what publishes. Accepted, because the alternative is a silent reinterpretation
and this one is at least visible in the diff. It also earns a warning the plain
spelling could not justify: see "A mangled id" below.

### Rejected alternatives

| | why not |
|---|---|
| bare `@Ada Lovelace` | no id; needs a name lookup that two same-named people make ambiguous |
| `[@Ada](confluence-user:712020:…)` | unambiguous, but renders as a dead link everywhere except markfluence, against C1 |
| `@Ada Lovelace<!-- ri:user 712020:… -->` | has precedent (`tables.go`'s `bg:` cell comment) and is unambiguous, but no click-through, and the id is invisible so an author can edit the name into disagreement with it |
| reference-style `[@Ada][user-712020]` | strictly more machinery than A′ for the same properties |

The comment form is the serious runner-up: it reads as prose rather than as a
link, which is nicer inside a sentence. Revisit if the `@`-in-link-text
convention turns out to annoy people in practice.

## What the probe settled — verified 2026-09-11

Recorded in [links-and-anchors.md](../docs/confluence/links-and-anchors.md); the
three findings that shape the work.

**`ri:local-id` is not required.** A mention published with only
`ri:account-id` comes back as an ADF `mention` node resolving to the same
display name as one carrying a local-id. Read as ADF on purpose — `body.storage`
was byte-identical for every spelling, and storage proves only what was stored.
Had the local-id been required, nothing markdown can hold would republish as a
mention and this issue would close as "passthrough is correct".

So the local-id is dropped on republish, joining `ac:local-id`/`ac:macro-id` in
`droppedAttrs`' category: a per-instance server-generated id.

**An unresolvable id publishes happily**, as `@Unlicensed user`, while
`GET /user?accountId=…` 404s for it. Confluence will not tell you an id is
wrong. That decides both fallbacks — see below.

**ADF names every mention for free** (`attrs.text`), which is a better answer
than the bulk user route #91 wondered about — except that `body-format` takes
one value, and asking for two answers **200 with an empty body object**. So it
costs a second page fetch. See "The lookup" below.

## Decisions

**The inverse direction resolves names through `client.GetUser`, not ADF.**
One `GET /user` per distinct account id, gathered the way
`PageLinkTargets`/`PageLinks` already gathers page titles: walk the parsed body,
collect distinct ids, resolve once each, hand a map back through
`StorageOptions`. A page with no mention makes no request, which is the guard
every other lookup in `pagedoc` already has.

Rejected: harvesting names from an ADF fetch. It is one request per *page*
rather than per *user*, so it wins on a rotation table and loses on the common
page with one mention — but the deciding factor is that it means parsing a
second body format, in a package whose whole job is the storage one, to obtain a
string that is decoration. `internal/convert` would gain an ADF reader for the
benefit of the name in a link's text. Worth revisiting only if the per-id cost
is measured to be a problem; noted in the issue rather than built.

**The name cache is per run, cross-page, and passed in — not built per page.**
This is the one place the obvious structure is wrong. `PageLinks` builds its
`spaceIDs` map inside itself, once per page, and `Options` is constructed per
page too; a user map written in that shape re-resolves the same twelve on-call
people on every page of a 200-page export, which is 2400 requests to learn
twelve names. So the cache is threaded in from the caller, the way
`project.Cache` and `linkindex.Cache` already are, and the precedent for its
shape is `cmd/info`'s `authorName(c, accountID, cache)` and `cmd/create`'s
`spaceCache`: an explicit map argument, per invocation, no package-level state.

**A miss is cached too.** `authorName` already does this, storing the id itself
when the lookup fails. Without it a page mentioning three deactivated people
costs three requests *per page*, forever, to learn the same three failures.
The cached value has to distinguish "resolved to this name" from "does not
resolve", since the two lead to different renderings.

**Not persisted to disk**, and the reason is a guarantee rather than effort.
**L2** (`invocation-independent`) says output depends only on the files on disk
-- not the working directory, nor which files were passed in the same command.
A disk cache adds "nor what your cache happens to hold": two people exporting
the same page would get different names, and a re-export would change a name
with nothing in the diff to explain it.

It is also the one part of this that is not static. The account id never changes
-- which is exactly why it is the source of truth here -- but the id→name
mapping does, rarely, with no invalidation signal available. A rename would
stick until someone cleared the cache by hand, and the failure would be silent
and cosmetic, which is the kind that survives for years. markfluence has no
persistent state at all today, so a cache directory would bring its whole tail
(where it lives, how it is invalidated, whether `--no-cache` exists, what a
shared CI runner does with it) to save a handful of requests.

**The cache is what makes the forward-path warning affordable.** Publishing
needs only the id; the name is never used. So the sole reason the forward path
calls `GetUser` at all is the mangled-id warning below, and without a
cross-page cache that warning costs a request per distinct mention on every
`update` -- hard to justify on a rotation table. With one, a batch update of 50
files sharing 20 people costs 20 requests once.

**An id `GetUser` cannot resolve passes through as storage.** There is no name
to render, and `[@712020:0e5f…](…)` shows a reader nothing the raw storage did
not. This is the existing behaviour for an unresolved `ri:page`, and it is why
the mapping table's `ri:user` row now has two rows rather than one.

**A mangled id is reported, not published silently.** Confluence accepts any
id and renders `@Unlicensed user`, so the server will never tell an author that
the id they hand-edited is wrong. Nor will the profile link: verified
2026-09-12, `{site}/wiki/people/{id}` returns the same static SPA shell for a
real account and for `utter-nonsense`, so a dead mention link is
indistinguishable from a live one by anything but a human clicking it. The
`GetUser` check is the only signal available.

The forward path therefore resolves the id
before publishing (`GetUser`) and, when it does not resolve, emits a
**warning** — not a `Broken`, since the mention still publishes and still
names a person to anyone who can see the account; and not silence, since a
mention nobody receives is exactly the failure #91 is about avoiding.

This is what the `@` marker buys that plain-URL recognition could not: without
it, every plain link to a profile would earn the same lookup and the same
warning, and most of them are not mentions at all.

**The forward path needs no new input at all, and the validation lives in the
caller.** Emitting a mention needs only the account id, which is in the URL,
and the base URL, which `MdToConfluence` already takes — so the converter needs
neither a client nor an options struct, and its signature does not change. That
also keeps the regression suite client-free.

Only the *warning* needs a lookup, so `ConfluencePage` gains
`Mentions []string` — the ids the conversion emitted — and `update`/`create`
resolve and warn. That is the `Attachments` shape exactly: the converter
discovers, the caller acts.

**Neither Confluence profile form is emitted**, only recognised.
`{site}/wiki/display/~{accountId}` 302s to `{site}/wiki/people/{accountId}`,
and that one in turn no longer renders usefully — so both are read on the way
in and neither is written on the way out. A file exported before this decision
keeps working; it just gets rewritten to the Home URL the next time it is
exported, since the URL is regenerated rather than preserved.

**`?ref=confluence` and `?cloudId=…` are recognised and never emitted.** Both
are decoration on a URL whose only load-bearing part is the id. Unresolved and
deliberately so: the one difference between a blank profile page and a working
one, in the browser session where this was found, was `ref=confluence`. Most
likely that was a login interstitial rather than the parameter. markfluence
emits neither, so nothing here depends on the answer.

**Matching is on the path, ignoring host and query.** The rule is "any URL whose
path ends `/people/{id}`", plus the legacy `display/~{id}` form. This is not a
convenience: there are genuinely several spellings of the same target in
circulation, and every one of them will be pasted into a file by somebody.

| form | where it comes from |
|---|---|
| `home.atlassian.com/people/{id}` | what markfluence emits |
| `home.atlassian.com/people/{id}?cloudId=…` | the copy link in Confluence's own person modal |
| `home.atlassian.com/o/{orgId}/people/{id}?cloudId=…` | the redirect target, equally pasteable |
| `{site}/wiki/people/{id}` | what Confluence's renderer emits, so any link copied from a rendered page |
| `{site}/wiki/display/~{id}` | legacy; 302s to the Confluence form |
| `/people/{id}` or `/wiki/people/{id}` | root-relative, for the same reason |

Ignoring the query follows from the same logic as ignoring the host: neither
`cloudId` nor `ref` nor `/o/{orgId}` identifies the person. Only the id does,
and `<ri:user ri:account-id="…"/>` stores nothing else — so requiring any of
them to match would invent a constraint the stored data does not have.

What it buys, and this is now by construction rather than by concession:
**`check` agrees with `update`.** The emitted URL contains no site, so
recognising a mention needs no client and no site knowledge — `check`'s
hardcoded `https://wiki.example.net` is simply irrelevant to it. An earlier
draft of this plan had to argue host-agnostic matching *in order to* stop
`check` reporting an `<a href>` where `update` publishes a mention; with a
site-independent URL there is nothing left to reconcile.

What it costs is that a link to another instance's Confluence-form profile URL
publishes as a mention here. Defensible: an Atlassian account id is global, so
it names the same human. They may lack access to this site, in which case
Confluence renders `@Unlicensed user` — the same outcome as any unresolvable
id, which the warning below covers.

A consequence worth stating: the fixed point is now **site-independent**, where
an earlier draft of this plan had it per-site. Export → publish → export is
identical regardless of which site it ran against, because nothing site-specific
survives into the markdown.

**The account id is not pattern-validated.** Ids come in at least two shapes —
`60c36d0718e9f60071326951` (24 hex characters, no prefix) and
`712020:0e5f8a21-3c4d-4e5f-a6b7-c8d9e0f1a2b3` (prefix, colon, UUID), both
observed on the live instance. So the id is "the last path segment, non-empty,
no slash" and nothing narrower; a shape check would reject real ids. The only
real validation is the `GetUser` lookup, which is what the warning reports.

**`mdLink` escapes its text, which fixes a bug older than this issue.**
`mdLink` is a bare `fmt.Sprintf("[%s](%s)")` with no escaping, so a page title
containing `]` already emits a broken link today, on the page-link and
space-link paths. Mentions turn that from theoretical into likely: display names
carry brackets, and a `|` inside a table cell breaks the row — with a mention in
a table being the case this issue is named for. Fixed here rather than filed
separately, because the feature is not correct without it.

Parens need no escaping (`(she/her)` is fine in link text); `[`, `]`, `\` do,
and `|` does inside a table cell.

## Implementation

### `internal/convert` — inverse (`storage_to_md.go`, `aclink.go`)

- `StorageOptions.UserNames map[string]string` — account id → display name, nil
  or missing meaning "pass this mention through".
- `MentionTargets(storage string) []string` — the distinct account ids in a
  document, in document order, the sibling of `PageLinkTargets` and with the
  same rationale for reporting ids inside raw macros (one wasted lookup beats
  keeping two copies of the macro rules in step).
- `renderACLink`'s `default` branch gains an `ri:user` case ahead of it,
  rendering `[@Name](SITE/wiki/people/{id})` when the name is known and
  `serialize(n)` when it is not. `ri:attachment`/`ri:blog-post` keep the
  default.
- The `@` is part of the *text*, not the URL.
- `mdLink` gains text escaping (see the decision above), which changes the
  page-link and space-link paths too — so the regression goldens may move for a
  title that needs it, and that movement is the fix rather than a surprise.

### `internal/convert` — forward (`links.go`)

- A mention branch in `rewriteHref`, **before** the absolute-URL fall-through in
  `rewriteDocLink`: a destination matching `{site}/wiki/people/{id}` (or the
  legacy `display/~{id}`) whose link text begins with `@` renders
  `<ac:link><ri:user ri:account-id="{id}" /></ac:link>` and skips the `<a>`
  element entirely — the same shape `images.go` uses when it replaces a whole
  element.
- Text not beginning with `@` falls through untouched and publishes as an
  ordinary link. Pinned by a test, since this is the decision A′ exists for.
- `MentionIDs(md)` equivalent for the forward direction so the caller can
  resolve ids and pass names/validity in; an id that does not resolve publishes
  anyway and warns.

### `internal/pagedoc`

- Gather → resolve → pass, beside `PageLinks`: `UserNames(c, page, cache)`
  resolving each distinct id once, best-effort, omitting what fails. `Options`
  wires it in. No request when the body holds no mention.
- The **cache is a parameter, not a local**, which is the one structural thing
  to get right — see the caching decision above.

### Commands

`read`, `export` get it through `pagedoc` automatically. `update`/`create` get
the forward direction and the new warning. `check` cannot resolve an id
offline, so a mention link is left alone there — worth a note in its own docs,
since "check validates offline" and "a mention needs the server" do not compose.

## Tests

- **Fixed point**, extending `TestRoundTripMarkdownIsAFixedPoint`: a mention
  exports, republishes, and re-exports identically.
- **Round trip through storage**: mention → markdown → storage is a `ri:user`
  with the same account id and **no** `ri:local-id`.
- **The `@` rule, both ways**: `[@Ada](profile)` publishes as a mention;
  `[Ada's profile](profile)` publishes as an `<a href>`. This pair is the plan.
- **Unresolved name passes through** byte-identical, which the shield already
  guarantees — the test is that the *decision* to pass through is taken.
- **A mangled id warns** and still publishes, since Confluence accepts it.
- **The legacy URL is recognised** on the forward path and not emitted on the
  inverse.
- **Host and query are ignored**: the same mention publishes from every row of
  the recognition table above — the Home URL, the Home URL with `cloudId`, the
  `/o/{orgId}` redirect form, both Confluence forms, and the root-relative
  ones. This is the decision, so it is the test that states it.
- **What is emitted is the Home URL with no query**, asserted exactly, since
  that is the only spelling verified to work in a browser.
- **Both account-id shapes** round-trip: bare 24-hex and `prefix:uuid`.
- **A name needing escapes** survives a round trip: a display name holding `]`
  or `|`, the second inside a table cell.
- **No mention, no request** — the `pagedoc` guard.
- **One request per distinct id across a whole walk**, not per page: an export
  of several pages mentioning the same person resolves them once. Asserted on a
  request count, since this is the difference between twelve requests and
  several thousand and nothing else in the output would show it.
- **A miss is not retried** per page.
- Regression cases under `internal/convert/testdata/regression/` for a mention
  in a paragraph and in a table cell, the latter because `renderCellLines` joins
  a cell's children and a mention inside one is the shape #91 is about.

## Docs

- `links-and-anchors.md` — already landed: the probe findings and the split
  `ri:user` mapping rows.
- `README.md` — the mention spelling in the `read`/`export` sections, and the
  `@` rule stated where an author will look for it.
- `docs/guarantees.md` — L5/L6 stay Partial; this narrows the gap rather than
  closing it, and the note there should say so rather than implying a mention
  was the last thing missing.

## Out of scope

- **Mentions in a `create`/`update` from scratch**, i.e. an author writing a
  mention for a person whose id they do not know. That needs a name→id search
  and belongs with whatever solves the ambiguity of two people sharing a name.
- **`ri:attachment` and `ri:blog-post`**, which stay passthrough for the reasons
  #88 recorded.
- **Harvesting names from ADF.** Noted above; revisit only if the per-id lookup
  is measured to hurt.
