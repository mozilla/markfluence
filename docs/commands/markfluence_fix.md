## markfluence fix

Reconcile each markdown file's frontmatter to its live Confluence page

### Synopsis

Reconcile each markdown file's frontmatter to its live Confluence page.

Populates/refreshes page_id, space, parent, page_width and labels (and
fills a missing title) from the live page. The page is located by page_id,
or by searching for the title when page_id is absent. fix never creates,
updates or moves pages -- it is read-only on the server. Each file is
processed independently; the command exits non-zero if any file failed.

It writes a file when a field changed, and also when the frontmatter keys
are out of canonical order (title, space, parent, page_id, then the rest
alphabetically), which is reported separately as reordered. --dry-run
reports both without writing.

Labels are reconciled even for a file with no labels: line, which is how
you adopt a page somebody labeled in the UI. That is the one place fix
fills in a field update would have left alone, because fix reconciles the
file to the page rather than the page to the file.

parent is written as the live page's parent id. In a tree written by
`export --depth`, where parent points at the parent's own .md file,
fix therefore replaces that path with an id -- consistent with
reconciling to the live page, and worth knowing before running it over
an exported tree.

```
markfluence fix FILE... [flags]
```

### Examples

```
  # Reconcile a batch of files to their live pages
  markfluence fix docs/*.md

  # Report what would change, write nothing
  markfluence fix docs/foo.md --dry-run
```

### Options

```
      --dry-run   Report the changes fix would make without writing any files.
  -h, --help      help for fix
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

