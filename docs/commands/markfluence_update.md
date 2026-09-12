## markfluence update

Publish one or more markdown files to Confluence pages

### Synopsis

Publish one or more markdown FILEs to Confluence pages.

Title and page id are read from each file's YAML frontmatter; --title and
--page-id override the frontmatter (and require a single FILE). A page id is
required (from --page-id or frontmatter); update errors if none is set.

Page width is asserted only when set via --page-width, a page_width
frontmatter line, or a page_width: in markfluence.yaml -- otherwise the
live page's width is left untouched.
Labels work the same way: a labels: line is asserted exactly (anything on
the page the file does not list is removed), and no labels: line means the
page's labels are left alone, not even read.

update never writes back to the file, so fixing a wrong page_id is always
safe: the file is exactly as you left it. A page_id that no longer resolves
fails that file and says what to do about it; one that is not a numeric id
at all is reported without asking Confluence.

A file that has not changed since the page's last version is skipped,
compared by mtime, unless --force is given. Each file is processed
independently; the command exits non-zero if any file failed.

--dry-run previews the version bump, attachment uploads and any width or
label change without writing to Confluence. It honours the mtime skip and
--force exactly as a real run does, so its forecast matches.

```
markfluence update FILE... [flags]
```

### Examples

```
  # Publish a file, taking the page id from its frontmatter
  markfluence update docs/managing_an_incident.md

  # Publish a batch with a version message
  markfluence update docs/*.md --message "Bulk update"

  # Republish even though the file has not changed
  markfluence update docs/foo.md --force

  # Override the target page, or rename it
  markfluence update page.md --page-id 123456
  markfluence update page.md --title "New Title"

  # Set the width across a batch
  markfluence update docs/*.md --page-width wide

  # Preview, write nothing
  markfluence update docs/*.md --dry-run
```

### Options

```
      --dry-run             Preview what would be published without writing to Confluence.
      --force               Skip the file-mtime check and always update the page.
  -h, --help                help for update
      --message string      Version message. (default "Updated via markfluence")
      --page-id string      Override the target page id (requires a single FILE).
      --page-width string   Override the page width: narrow, wide, or max.
      --title string        Override the page title (requires a single FILE).
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

