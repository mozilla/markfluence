---
page_id: 9001
title: Anchor Not Found
---
# Anchor Not Found

A same-page anchor that matches no heading is a Warning; the href renders
exactly as written since nothing was resolved:

[bad same-page anchor](#no-such-heading)

A cross-file anchor that matches no heading on an otherwise-resolvable
sibling warns the same way -- distinct from the sibling not existing at all,
which rewriteDocLink already reports separately:

[bad cross-file anchor](sibling.md#no-such-heading)

## Real Section

The only real heading in this document.
