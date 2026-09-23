## markfluence diff

Show what differs between a page and its local markdown file

### Synopsis

Show what differs between a Confluence page and the local markdown
file that publishes to it. Nothing is written, to disk or to Confluence.

FILE is one markdown file, and its page comes from its own page_id or
from its entry in markfluence.yaml's pages: block. One file, not a glob:
a diff over a whole tree is output nobody reads.

THE OUTPUT IS TWO THINGS, ON TWO STREAMS

stdout carries the body as a unified diff and nothing else, so it is a
patch other tools can use:

  markfluence diff FILE > my.diff && patch -R -p1 < my.diff

It applies to the real file because the frontmatter block is shared
between the two sides rather than diffed, which also keeps hunk line
numbers the ones you would count to in an editor and means no patch can
rewrite a page_id.

The labels name the file relative to the documentation root, however the
command was invoked, so run patch from the root -- not from the
directory the file happens to be in.

stderr carries the frontmatter half as a per-field report, naming for
each field whether the local value came from the file's frontmatter or
from markfluence.yaml. Redirect it away with 2>/dev/null, or keep only
it with >/dev/null.

EXIT CODES ARE diff(1)'s, NOT markfluence's

  0  identical
  1  differs (either half)
  2  trouble -- a bad flag, a file naming no page, a page that is gone,
     a rejected credential, a failed fetch

So `if markfluence diff FILE >/dev/null; then ...` means "in sync".
Every other command reports an operational failure as 1; this one is 2,
because 1 is spoken for.

WHICH FIELDS ARE COMPARED

Only the ones the file declares, because those are the ones publishing
would assert: an absent labels or page_width leaves the page's alone, an
absent title keeps the live title. So a file carrying only page_id and
title reports on its title and its body, and nothing else.

Two fields are compared but not reconciled by any verb today: update
moves a page neither between spaces nor to a new parent, so a space or
parent difference is a disagreement to fix by hand.

DIFFERENCES YOU DID NOT MAKE

The Confluence side is the page rendered back to markdown, and that
round trip is lossy in documented ways (guarantees L5 and L6 are both
Partial). Expect these, none of which are defects:

  - a table's :--- alignment publishes bare and reads back as ---, and
    a column takes its most common declared alignment
  - a bold span containing a link comes back respelled, once the
    Confluence editor has re-serialized the page
  - a soft line break inside a paragraph becomes a space
  - a table cell colour outside the 21 named swatches comes back as a
    literal hex
  - a macro markfluence does not map comes back as raw storage tags

This is why the patch is worth reading before it is worth applying.

```
markfluence diff FILE [flags]
```

### Examples

```
  # What would publishing this file change on the page?
  markfluence diff docs/runbook.md

  # Just the body patch, as a file
  markfluence diff docs/runbook.md 2>/dev/null > my.diff

  # Pull the page's edits into the file, conflicts and all
  markfluence diff --reverse docs/runbook.md | patch -p1

  # Is this file in sync?
  if markfluence diff docs/runbook.md >/dev/null 2>&1; then echo yes; fi

  # Side by side in an external tool
  markfluence read docs/runbook.md > /tmp/page.md && meld /tmp/page.md docs/runbook.md
```

### Options

```
  -h, --help      help for diff
      --reverse   Swap the sides, so the patch applies the page's changes to the file
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug output, such as each request and each retry
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
      --json              Write one JSON document to stdout, and no human output
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

