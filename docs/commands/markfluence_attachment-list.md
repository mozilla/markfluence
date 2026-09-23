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

