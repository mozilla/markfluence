# Same Page Anchor Unpublished

This file has no `page_id` yet, so a same-page anchor resolves to a real
heading but can't be turned into an absolute URL -- a Warning distinct from
the "link not resolved" one a genuinely unpublished sibling produces (#118),
since the target here is this file itself.

Jump to [Getting Started](#getting-started).

A self-reference whose fragment matches no heading at all is a different
case -- it must not claim the anchor resolved, since it didn't:
[bad self anchor](main.md#does-not-exist).

## Getting Started

Some getting-started content.
