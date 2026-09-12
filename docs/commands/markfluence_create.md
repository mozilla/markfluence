## markfluence create

Create new Confluence pages from markdown files

### Synopsis

Create new Confluence pages from markdown FILEs.

The title comes from frontmatter, or from --title, which overrides it and
requires a single FILE. The space comes from --space or frontmatter. The
parent comes from --parent or frontmatter and may be a page or a Cloud
folder -- give a folder's id the same way you would a page's. Page width
defaults to max.

Every file is checked first -- including converting it -- and if any would
fail, nothing is created. A page_id that resolves to nothing is a failure
too, not a fresh page: create will not publish a second copy and overwrite
an id it cannot explain. Remove the page_id to create a new page, or
correct it.

Once every file passes, a content-less stub is reserved for each,
parents-first, before any of them is converted -- so a link between two
files in the same batch resolves regardless of which direction it points,
or whether the two link to each other. A parent cycle among the given
files is rejected instead. A run interrupted after the reserve phase
leaves an empty page version behind rather than no page; every id is
already written back, so a plain update finishes the job.

A whole tree can be created in one pass: give each child a parent: that
points at its parent's .md file, and creation is ordered parents-first
with the real ids filled in.

Unless --no-persist is given, each created page's
title/space/parent/page_id/page_width/labels are written back into the
frontmatter.

--dry-run makes the same checks as a real run, so it exits non-zero on the
same failures and one unpublishable file aborts the preview for the whole
batch. To lint several files independently, use check instead.

```
markfluence create FILE... [flags]
```

### Examples

```
  # Create one page in a space
  markfluence create docs/new_page.md --space ENG

  # Create it under an existing parent page or folder
  markfluence create docs/child.md --space ENG --parent 123456

  # Create a whole tree, hierarchy taken from each file's parent: path
  markfluence create docs/*.md --space ENG

  # Override the title and width for a single file
  markfluence create note.md --space ENG --title "Ad-hoc note" --page-width wide

  # Create without writing page_id back into the file
  markfluence create note.md --space ENG --no-persist

  # Preview everything, write nothing
  markfluence create docs/*.md --space ENG --dry-run
```

### Options

```
      --dry-run             Preview what would be created without writing to Confluence or files.
  -h, --help                help for create
      --no-persist          Do not write anything back into the frontmatter.
      --page-width string   Override the page width: narrow, wide, or max.
      --parent string       Parent page or folder id for the new page(s).
      --space string        Target space key.
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

