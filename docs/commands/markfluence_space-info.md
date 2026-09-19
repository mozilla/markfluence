## markfluence space-info

Print metadata about a Confluence space

### Synopsis

Print what a Confluence space is, what the account you are running as
may do in it, which page statuses it offers, and how big it is.

KEY is a space key -- ENG, or a personal space like ~1234abcd -- never a
page or a markdown file. An unknown key is an error rather than an empty
result, since a typo and a space you cannot see should not look alike.

'your access' reports what the space grants: whether you can read it and
whether you can create pages in it. It deliberately does not say
"write", because permission to edit an existing page is not a space
grant at all -- Confluence decides that per page -- so an account that
can create pages here may still be refused on a particular one.
markfluence page-info PAGE is where you ask about a page.

'page statuses' is what a page_status: line in a markdown file may say,
and it comes from one of two places, which the label tells you apart. A
space admin gets the space's own configured list. Everyone else gets what
THEY may set on the space homepage, which is not the same thing:
Confluence decides the list per page and per account, so another page may
allow more or fewer. When neither can be read the field says so.

The page counts are exact, which is why they cost a walk of the space --
one request per 250 pages. They count pages, not edits: a page revised
nine times in the window is one touched page. A page created inside the
window is counted as created AND touched, and the overlap is reported so
the two cannot be added up wrongly.

Read-only. Nothing is written to Confluence or to disk.

```
markfluence space-info KEY [flags]
```

### Examples

```
  # What is this space, and can I publish to it?
  markfluence space-info ENG

  # Activity over a month rather than a week
  markfluence space-info ENG --since 30

  # Just the statuses a page_status: line may use
  markfluence space-info ENG --json | jq '.results[0].page_statuses'
```

### Options

```
  -h, --help        help for space-info
      --since int   Window in days for the created/touched page counts (0 means today only). (default 7)
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID; set to use a scoped API token via the api.atlassian.com gateway (falls back to $CONFLUENCE_CLOUD_ID, then .env)
  -d, --debug             Enable verbose debug output
      --env-file string   Path to an env file to read (default: .env at the discovered project root, or the working directory if none)
      --json              Emit machine-readable JSON to stdout instead of human output
      --no-color          Disable colored output
      --root string       Documentation root, overriding discovery (default: the directory holding markfluence.yaml, found by walking up from each file, or the file's own directory if none)
      --url string        Confluence base URL (falls back to $CONFLUENCE_URL, then .env)
      --username string   Confluence username/email (falls back to $CONFLUENCE_USERNAME, then .env)
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

