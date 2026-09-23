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

