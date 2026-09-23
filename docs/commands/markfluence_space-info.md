## markfluence space-info

Show the metadata of a Confluence space

### Synopsis

Show what a Confluence space is, what your account can do in it, which page
statuses it has, and how big it is.

KEY is a space key, such as ENG, or a personal space such as ~1234abcd. It is
never a page or a Markdown file. An unknown key is an error, and not an empty
result. Thus a typo does not look the same as a space that you cannot see.

"your access" shows what the space grants: whether you can read it, and whether
you can create pages in it. It does not say "write", on purpose. Permission to
edit a page that exists is not a space grant at all, because Confluence decides
it for each page. Thus an account that can create pages here can still be
refused on one page. To ask about a page, use markfluence page-info PAGE.

"page statuses" shows what a page_status: line in a Markdown file can say. It
comes from one of two places, and its label tells you which:

  - A space admin gets the list that the space has configured.
  - Any other account gets the statuses that it can set on the homepage of the
    space. That is not the same list, because Confluence decides the list for
    each page and each account. A different page can have more statuses or
    fewer.

If markfluence can read neither list, the field says so.

--since counts from midnight UTC that many days ago. Thus 0 is today only, and
7 is the last week and today.

The page counts are exact, so space-info must walk the space, with one request
for each 250 pages. They count pages, not edits: a page that changed 9 times in
the period is one touched page. A page created in the period counts as created
AND touched, and space-info reports the overlap, so that nobody adds the two
counts together.

space-info only reads. It writes nothing to Confluence or to disk.

```
markfluence space-info KEY [flags]
```

### Examples

```
  # What is this space, and can I publish to it?
  markfluence space-info ENG

  # Show the activity of a month, and not of a week
  markfluence space-info ENG --since 30

  # Show only the statuses that a page_status: line can use
  markfluence space-info ENG --json | jq '.results[0].page_statuses'
```

### Options

```
  -h, --help        help for space-info
      --since int   How many days back from midnight UTC to count created and touched pages. 0 is today only. (default 7)
```

### Options inherited from parent commands

```
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   File to read credentials from, before the environment and your credentials file
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish Markdown to Confluence

