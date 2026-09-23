## markfluence export

Write a Confluence page and its attachments to a directory

### Synopsis

Write a Confluence page, and the attachments that it uses, to a directory.

PAGE is a page id, or a Confluence page URL or folder URL. It can also be a
Markdown file that names a page_id in its frontmatter or in its pages: entry. A folder has no
content of its own. Thus you can give a folder only with --depth, and its
content becomes the top level of the export.

To export a whole space, give --space KEY and no PAGE. The root pages of the
space become the top level. You must give --depth, because a walk of a space
makes two requests for each page and folder in it. You must ask for that on
purpose.

export writes each page as Markdown, with title, space, parent, page_id,
labels, page_status, and page_width in the frontmatter. Thus you can edit an
exported file and publish it back with update. For a page at the top of the
export, the file is exactly what read prints. Deeper in a tree, its paths are
relative to the location of the file, which read cannot know.

--depth also exports the descendants of the page, in the same hierarchy as in
Confluence. A page becomes <slug>.md, with a <slug>/ directory next to it for
its children. A folder becomes a directory. The parent: of each child names
the file of its parent, so you can publish the tree into new pages. If two
sibling titles make the same slug, export adds -<id> to each of their names.

When export writes more than one page, it also writes a markfluence.yaml into
--dest. Without it, the pages could not reach the attachments that they share.

export writes an attachment that markfluence published to the path that its
image came from. It writes an attachment that came from Confluence into the
directory of its page. An attachment name is unique on a page, but not in a
space. export writes only the attachments that the page references.
--all-attachments writes every attachment on the page.

export is read and attachment-download in one command.

```
markfluence export [PAGE] [flags]
```

### Examples

```
  # Export one page and the attachments that it uses
  markfluence export 1234567890 --dest ./out

  # Export the page and its whole subtree, in the same hierarchy
  markfluence export 1234567890 --depth all --dest out

  # Export a whole space. A space needs --depth
  markfluence export --space ENG --depth all --dest out

  # Export a tree again after its pages changed in Confluence
  markfluence export 1234567890 --depth all --dest out --force

```

### Options

```
      --all-attachments    Export every attachment on the page, and not only the attachments that it references.
      --depth string       How many levels to export: 0 for the page only, a positive number, or "all". (default "0")
      --dest string        Directory to write the export into. (default ".")
      --dry-run            Show what export would write, and write no files.
      --file string        Name of the page file. The default is a slug of the title, or the page id if the title gives an empty slug. Not with --depth.
      --force              Overwrite a file that already exists.
  -h, --help               help for export
      --skip-attachments   Write only the page file, and no attachments.
      --space string       Export a whole space, by its key, and not a PAGE.
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

