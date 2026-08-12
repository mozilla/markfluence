# Attachments

## What a name may contain

`/` is not legal in an attachment name. Everything else we have tried is.

markfluence therefore flattens an image's path into a name by percent-encoding
`%` → `%25` first and `/` → `%2F` second — bijective, so distinct paths can never
collide and `read` can recover the original path exactly. See
`internal/convert/attachname.go`.

| in the name | result | |
|---|---|---|
| `/` | illegal — hence the encoding | Transcribed |
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

## The comment field is charset-broken

markfluence records the source path and a checksum in the attachment's comment:

```
markfluence: sha256=<hex> path=<source path>
```

**For a non-ASCII path this comes back corrupted. Verified 2026-08-07** —
same upload, one field intact and one not:

```
name    bytes: assets%2Fprobe-caf\xc3\xa9.png            <- correct UTF-8 "é"
comment bytes: path=assets/probe-caf\xc3\x83\xc2\xa9.png <- UTF-8 of "Ã©"
```

`\xc3\x83\xc2\xa9` is the UTF-8 encoding of `Ã©`: our UTF-8 bytes were read as
two Latin-1 characters and re-encoded.

The cause is on our side. `uploadAttachment` sends the comment with
`multipart.Writer.WriteField`, which emits the part with no `Content-Type` and
therefore no charset; Confluence's servlet stack defaults an unlabeled text part
to ISO-8859-1. The filename survives because it does not go through
`WriteField` — it rides in the file part's `Content-Disposition`.

Consequence: `read` returns a path that does not exist, `export` writes the
wrong filename, and republishing the export uploads a second attachment and
orphans the first. Tracked as **issue #64**.

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
