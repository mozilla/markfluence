## markfluence attachment-download

Download a Confluence page's attachments

### Synopsis

Download a Confluence page's attachments.

PAGE is a numeric page id, a Confluence page URL, or a markdown file
whose frontmatter has a page_id. Each NAME is an attachment name as
attachment-list reports it; with no NAME, every attachment is
downloaded.

An attachment markfluence published records the markdown image path it
came from, and is written back to that path under --dest, so the
downloaded tree matches what the page's markdown references and
previews locally.

An attachment without a recorded path -- one that originated in
Confluence -- is written under a directory named after the page, since
an attachment name is unique per page and not per space: two pages'
diagram.png would otherwise be one file. That is where `read` and
`export` point at it too.

--flat writes everything directly under --dest, under stored names.

A recorded path that would resolve outside --dest is refused for that
attachment, since the path comes from an attachment comment anyone who
can edit the page controls.

A file that already exists is skipped unless --force.

```
markfluence attachment-download PAGE [NAME...] [flags]
```

### Examples

```
  # Every attachment, to the paths they were published from
  markfluence attachment-download 1234567890 --dest ./out

  # Just one, by its stored name
  markfluence attachment-download 1234567890 diagram.png --dest ./out

  # Ignore recorded paths and write everything flat
  markfluence attachment-download 1234567890 --dest ./out --flat

```

### Options

```
      --dest string   Directory to write attachments into. (default ".")
      --dry-run       Preview what would be written without creating any files.
      --flat          Write every attachment under its stored name, ignoring recorded paths.
      --force         Overwrite files that already exist.
  -h, --help          help for attachment-download
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

