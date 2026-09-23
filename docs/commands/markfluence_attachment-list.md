## markfluence attachment-list

List a Confluence page's attachments

### Synopsis

List a Confluence page's attachments.

PAGE is a numeric page id, a Confluence page URL, or a markdown file
whose frontmatter has a page_id.

The NAME column is the name Confluence stores, which is what
attachment-download takes. For an image markfluence published that is
the encoded source path, and the SOURCE column shows the markdown
image path it came from.

SOURCE is a dash when no source path is recorded: the attachment was
uploaded by hand, or it was published before markfluence recorded one.
Use --json, whose managed field tells those two apart.

```
markfluence attachment-list PAGE [flags]
```

### Examples

```
  # Every attachment on a page
  markfluence attachment-list 1234567890

  # By the file that publishes to it
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

