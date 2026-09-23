## markfluence export

Write a Confluence page and its attachments to a directory

### Synopsis

Write a Confluence page and the attachments it uses to a directory.

PAGE is a numeric page id, a Confluence page or folder URL, or a
markdown file whose frontmatter has a page_id. A folder has no content
of its own, so it is a target only with --depth: what is inside it
becomes the top level of the export.

Pass --space KEY instead of a PAGE to export a whole space, whose root
pages become the top level. It needs an explicit --depth, since a space
walk is one pair of requests per page and folder in it and should be
asked for rather than typed by accident.

The page is written as markdown with title/space/parent/page_id/
labels/page_status/page_width frontmatter, so an exported file can be
edited and published back with update. For a page at the top of the export this
is exactly what `read` prints; deeper in a tree the paths in it are
relative to where the file sits, which `read` cannot know.

--depth exports the page's descendants too, mirroring the Confluence
hierarchy: a page becomes <slug>.md with a <slug>/ beside it for its
children, a folder becomes a directory, and each child's parent:
points at its parent's file so the tree can be published into fresh
pages. It costs a pair of requests per page and folder walked, plus
the page's own.

Attachments markfluence published are written to the paths their
images came from; one that originated in Confluence is written under
the page's own directory, since attachment names are unique per page
and not per space. Only attachments the page references are exported;
--all-attachments takes everything on the page.

This is the one-command form of `read` plus `attachment-download`.

```
markfluence export [PAGE] [flags]
```

### Examples

```
  # One page and the attachments it uses
  markfluence export 1234567890 --dest ./out

  # The page and its whole subtree, hierarchy mirrored on disk
  markfluence export 1234567890 --depth all --dest out

  # A whole space; --depth is required for a space walk
  markfluence export --space ENG --depth all --dest out

  # Re-export a tree whose pages changed upstream
  markfluence export 1234567890 --depth all --dest out --force

```

### Options

```
      --all-attachments    Export every attachment on the page, not just the referenced ones.
      --depth string       How deep to export: 0 for the page alone, a positive number, or "all". (default "0")
      --dest string        Directory to write the export into. (default ".")
      --dry-run            Preview what would be written without creating any files.
      --file string        Name for the page file (default: a slug of the title, or the page id if that slugs to nothing).
      --force              Overwrite files that already exist.
  -h, --help               help for export
      --skip-attachments   Write the page file only.
      --space string       Export a whole space, by key, instead of a PAGE.
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

