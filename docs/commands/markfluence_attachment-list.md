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

