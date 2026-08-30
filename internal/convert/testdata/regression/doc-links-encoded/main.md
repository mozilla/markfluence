---
title: Encoded Doc Links
page_id: 4345
---
# Encoded Doc Links

A link destination is a URL, so a sibling whose filename contains a space has
to be percent-encoded to be linked at all. This is the spelling editors and
previews produce, and it must resolve to the same page as the angle-bracket
form below:

[percent-encoded](my%20sibling.md)

[angle brackets](<my sibling.md>)

A bare space is not a valid destination, so this is not a link at all and stays
literal text -- the same as GitHub and a local preview render it:

[bare space](my sibling.md)

The fragment is a URL too. An encoded anchor has to be decoded before it can be
matched against a heading slug, which is Unicode-aware:

[encoded fragment](plain.md#caf%C3%A9-section)

[literal fragment](plain.md#café-section)

A same-page anchor takes the same path through the anchor map:

[same page, encoded](#caf%C3%A9-section)

[same page, literal](#café-section)

Both halves at once -- encoded filename and encoded fragment:

[both encoded](my%20sibling.md#caf%C3%A9-section)

A sibling that is not in this set is Broken; the reported message still
echoes the destination as written, encoding and all, rather than a resolved
answer:

[unknown](nosuch%20file.md)

An absolute URL keeps its encoding untouched:

[external](https://example.net/a%20b.md)

## Café Section

Content under a heading whose slug is not ASCII.
