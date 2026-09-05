# Attachments

## What a name may contain

`/` is not legal in an attachment name. Everything else we have tried is.

markfluence names an attachment by its **base name** — `assets/diagram.png` is
attached as `diagram.png` — and records the path in the attachment's comment.
See `internal/convert/attachname.go`.

It used to flatten the whole path into the name by percent-encoding `%` → `%25`
then `/` → `%2F`, which was bijective. The measurements below were taken against
those names and are kept, because they are facts about Confluence rather than
about markfluence: a name containing `%2F` still resolves, and someone will
still encounter one on a page published before the change. What they no longer
describe is what markfluence produces.

| in the name | result | |
|---|---|---|
| `/` | illegal — hence the base name | Transcribed |
| `%2F` (encoded slash) | resolves and renders; the image URL re-escapes it to `%252F` | **Verified** |
| a literal space | stored and returned byte-identically; renders | **Verified 2026-08-07** |
| non-ASCII (`é`) | stored and returned byte-identically, NFC preserved | **Verified 2026-08-07** |

**Verified 2026-08-07** on a page carrying attachments named
`assets%2Fprobe image.png` and `assets%2Fprobe-café.png`. Both render. The
rendered `<img src>` shows the stored name re-escaped once more —
`assets%252Fprobe%20image.png` — confirming Confluence matches `ri:filename`
against the stored name literally and escapes only when building the URL.

The space case matters more than it looks. Before the fix for issue #20, a
spaced filename was reachable only through the rarely-used `![](<a b.png>)`
spelling; now the ordinary `%20` spelling reaches it, so these names are common
rather than theoretical.

## How long a name and a comment may be

Both cap at **255 characters**, and they fail very differently.

**Verified 2026-08-28** by uploading to a scratch page, stepping the length of
each field:

| field | 255 | 256 | over-limit response |
|---|---|---|---|
| comment | stored whole | rejected | **400** — `IllegalArgumentException: The comment is longer than 255 characters` |
| name | stored whole | rejected | **500** — a raw `HibernateJdbcException` / `SQLException [25P02]` |

Nothing is truncated. That matters more than the limit itself: a silently
truncated comment would make the stored `path=` disagree with the local source on
every publish, and [a wrong recorded path repairs itself](#a-wrong-recorded-path-repairs-itself)
would re-upload the attachment forever trying to correct it.

The name's failure is an unvalidated constraint violation rather than a checked
one, so it says nothing useful. It is at least not retried — a bare 500 with no
`Retry-After` is not retryable under the rules in [api.md](api.md#retries).

### The comment limit is what bounds a path

The comment carries fixed overhead — `markfluence: ` + `sha256=` + 64 hex +
` path=` is **90 characters** — so a recorded source path may be at most
**165 characters**.

The name limit never binds first, and by a wider margin than it used to: a name
is now a single path component, so it is bounded by the filesystem's own name
limit (255 on every filesystem markfluence runs on) long before Confluence's.

Measured against realistic paths:

```
path=  12  name=  14  comment= 102   images/x.png
path=  74  name=  86  comment= 164   platform/services/authentication/docs/runbooks/images/failover-diagram.png
path= 130  name= 146  comment= 220   engineering/infrastructure/kubernetes/clusters/production/runbooks/…
```

165 characters is enough for a deep tree but not by a wide margin, and the
overhead is mostly the checksum — 64 of the 90 characters.

## An unlabeled multipart text part is decoded as Latin-1

markfluence records the source path and a checksum in the attachment's comment:

```
markfluence: sha256=<hex> path=<source path>
```

A multipart text part sent without a `Content-Type` carries no charset, and
Confluence's servlet stack defaults such a part to ISO-8859-1. UTF-8 bytes sent
that way are read as Latin-1 characters and re-encoded, so a non-ASCII path
comes back double-encoded.

**Verified 2026-08-07** on one upload whose comment went out unlabeled — the
name intact, the comment not:

```
name    bytes: assets%2Fprobe-caf\xc3\xa9.png            <- correct UTF-8 "é"
comment bytes: path=assets/probe-caf\xc3\x83\xc2\xa9.png <- UTF-8 of "Ã©"
```

`\xc3\x83\xc2\xa9` is the UTF-8 encoding of `Ã©`. The name is unaffected because
it does not travel in a text part — it rides in the file part's
`Content-Disposition`, which the same stack reads as UTF-8. ASCII values are
unaffected either way, including a spaced name like `probe image.png`.

So `uploadAttachment` writes every text part through `writeTextField`, which
sets `Content-Type: text/plain; charset=UTF-8` explicitly, and leads the form
with a `_charset_` field — the other conventional remedy for a Java stack, kept
as a belt-and-suspenders. **Anything added to that form must use
`writeTextField`, never `multipart.Writer.WriteField`.**

The label is what fixes it. **Verified 2026-08-12** by uploading two attachments
whose paths differ only in a non-ASCII letter — one replacing an existing
attachment (`POST .../child/attachment/<id>/data`), one new
(`POST .../child/attachment`). Both comments come back byte-identical to what
was sent:

```
comment bytes: path=assets/probe-caf\xc3\xa9.png   <- replace endpoint
comment bytes: path=assets/probe-na\xc3\xafve.png  <- create endpoint
```

Both endpoints also accept the `_charset_` field without complaint. Which of the
two remedies is doing the work is untested — they were added together, and there
is no reason to take either away.

### A wrong recorded path repairs itself

A skip does not rewrite the comment, so an attachment whose stored `path=` is
wrong would keep it until its bytes happened to change. `planAttachments`
therefore treats a *recorded path that disagrees with the local source* as an
update even when the checksum matches: under a matching name, a differing path
means the stored comment does not say what markfluence wrote.

Since the name became the base name this branch does more than repair a mangled
path — it is how an asset that **moved** settles. Moving `assets/flow.png` to
`img/flow.png` keeps the name `flow.png`, so the attachment is found, the bytes
match, and only the path disagrees: one re-upload to restamp the comment, and
nothing orphaned. Under the encoded names the move changed the name too, so the
old attachment was simply abandoned and this branch never saw it.

A legacy comment records no path at all. That is not a disagreement and stays a
skip; re-uploading every one of those is the churn the checksum comparison
exists to avoid. Those attachments gain a path the next time their bytes change,
or under `attachment-upload --force`.

**Verified 2026-08-12** against an attachment left double-encoded by an earlier
upload, re-uploading the same bytes so only the path disagreed: planned as
`updated` where the previous build planned `skipped (unchanged)`, and the stored
comment came back corrected. `attachment-download` then wrote the file to
`assets/probe-café.png` instead of `assets/probe-cafÃ©.png`, and `read` emitted
`assets/probe-caf%C3%A9.png` instead of `assets/probe-caf%C3%83%C2%A9.png`.

## Where the metadata lives

`fileSize` and `mediaType` are under `extensions`, not at the top level.

**Verified 2026-08-07.** A v1 attachment result expanded with
`metadata.comment,version,extensions`:

```json
"extensions": {
  "mediaType": "image/png",
  "fileSize": 171,
  "comment": "markfluence: sha256=… path=…",
  "mediaTypeDescription": "PNG Image",
  "fileId": "…",
  "collectionName": "contentId-<page id>"
}
```

The comment shows up under `extensions` too. Top-level keys are `ari`,
`base64EncodedAri`, `extensions`, `id`, `macroRenderedOutput`, `metadata`,
`status`, `title`, `type`, `version` — `title` being the stored attachment name.

See [api.md](api.md) for how these collections paginate and how downloads
redirect.
