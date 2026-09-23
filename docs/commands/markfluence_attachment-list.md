## markfluence attachment-list

List the attachments of a Confluence page

### Synopsis

List the attachments of a Confluence page.

PAGE is a page id, a Confluence page URL, or a Markdown file that names a
page_id in its frontmatter or in its pages: entry.

The NAME column is the name that Confluence stores. attachment-download takes
this name. For an image that markfluence published, it is the base name of the
file. The SOURCE column shows the path that the Markdown image used.

SOURCE is a dash when no path is recorded. Either a person uploaded the
attachment by hand, or markfluence published it before it recorded paths. The
managed field of --json tells you which.

```
markfluence attachment-list PAGE [flags]
```

### Examples

```
  # List every attachment on a page
  markfluence attachment-list 1234567890

  # List the attachments of the page that a file publishes to
  markfluence attachment-list docs/foo.md

```

### Options

```
  -h, --help   help for attachment-list
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

