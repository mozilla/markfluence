## markfluence diff

Show what is different between a page and its local Markdown file

### Synopsis

Show what is different between a Confluence page and the local Markdown file
that publishes to it. diff writes nothing, to disk or to Confluence.

FILE is one Markdown file. Its page_id comes from its frontmatter or from its
pages: entry in markfluence.yaml. diff takes one file, and not a glob, because
a diff of a whole tree is too long to read.

OUTPUT

diff writes two things, to two streams.

stdout holds the diff of the body in unified format, and nothing else. Thus it
is a patch that other tools can use:

  markfluence diff FILE > my.diff && patch -R -p1 < my.diff

With --json, stdout holds one JSON document with both parts, and not a patch.

The patch applies to the real file, because diff does not compare the
frontmatter block. Both sides share it. Thus the line numbers in each hunk are
the line numbers in your editor, and no patch can change a page_id.

The patch names the file relative to the documentation root, so run patch from
the root. If there is no markfluence.yaml, the patch names the file as you
typed it.

stderr holds a report on the frontmatter, one field at a time. For each field,
it tells you whether the local value came from the frontmatter or from
markfluence.yaml. To discard the report, add 2>/dev/null. To see only the
report, add >/dev/null.

EXIT CODES

diff uses the exit codes of diff(1), and not the exit codes of markfluence:

  0  the same
  1  different, in the body or in the frontmatter
  2  trouble: a bad flag, a file that names no page, a page that is gone,
     a refused credential, or a failed request

Thus `if markfluence diff FILE >/dev/null; then ...` means "in sync". Every
other command reports an operational failure as 1. diff reports it as 2,
because 1 has a different meaning here.

WHICH FIELDS DIFF COMPARES

diff compares only the fields that the file declares, because a publish asserts
only those. An absent labels or page_width leaves the value on the page alone,
and an absent title keeps the live title. Thus for a file with only page_id and
title, diff reports the title and the body, and nothing else.

diff compares space and parent, but no command changes them now. update does
not move a page to a different space or parent. Thus a difference in either
one is a disagreement that you must correct by hand.

DIFFERENCES THAT YOU DID NOT MAKE

The Confluence side is the page, rendered back to Markdown. That round trip
loses some details, in documented ways. Expect these differences. None of them
is a defect:

  - the alignment :--- of a table column publishes as plain --- and comes
    back as ---, and a column gets its most frequent alignment
  - a bold span that holds a link comes back with a different spelling, after
    the Confluence editor saves the page
  - a soft line break in a paragraph becomes a space
  - a cell color that is not one of the 21 named swatches comes back as a
    literal hex value
  - a macro that markfluence does not map comes back as raw storage tags

Thus read the patch before you apply it.

```
markfluence diff FILE [flags]
```

### Examples

```
  # What would a publish of this file change on the page?
  markfluence diff docs/runbook.md

  # Write only the patch of the body to a file
  markfluence diff docs/runbook.md 2>/dev/null > my.diff

  # Put the edits from the page into the file, conflicts included
  markfluence diff --reverse docs/runbook.md | patch -p1

  # Is this file in sync?
  if markfluence diff docs/runbook.md >/dev/null 2>&1; then echo yes; fi

  # Compare the two in an external tool
  markfluence read docs/runbook.md > /tmp/page.md && meld /tmp/page.md docs/runbook.md
```

### Options

```
  -h, --help      help for diff
      --reverse   Swap the two sides. Then the patch applies the changes on the page to the file
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

