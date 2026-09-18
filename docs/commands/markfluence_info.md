## markfluence info

Print metadata about a Confluence page

### Synopsis

Print metadata about a Confluence page.

Id, title, content status, space, parent, version, page width, page
status, labels, the created/updated author stamps, and the page URL. An
empty field is omitted rather than printed blank.

Two of those wear the word status and mean different things.
content_status is current, archived or trashed. page_status is the
coloured lozenge beside the title, and page_status/available lists the
ones this page's space offers -- which is how to find out what a
page_status: line in a markdown file may say, since a space's statuses
are its own configuration rather than a fixed list.

PAGE is a numeric page id, a Confluence page URL, or a markdown file
whose frontmatter has a page_id.

--properties also lists every one of the page's content properties, which
is where Confluence keeps things like the page width.

```
markfluence info PAGE [flags]
```

### Examples

```
  # By page id
  markfluence info 1234567890

  # By the file that publishes to it, with content properties
  markfluence info docs/foo.md --properties
```

### Options

```
  -h, --help         help for info
      --properties   Also list all of the page's content properties.
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

