## markfluence attachment-upload

Upload or replace attachments on a Confluence page

### Synopsis

Upload or replace attachments on a Confluence page.

PAGE is a numeric page id, a Confluence page URL, or a markdown file
whose frontmatter has a page_id.

Each file is attached under its base name, with its path relative to
the documentation root recorded in the attachment's comment. A file
whose contents already match the attachment on the page is skipped,
using the same checksum bookkeeping create/update use, so uploading by
hand and publishing agree on what is current; --force uploads anyway.

--name takes a path, not a name, for a single file: `--name
assets/x.png` produces the attachment an image written as
![](assets/x.png) resolves to -- stored as x.png, recorded as
assets/x.png.

Two files whose base names agree cannot both be uploaded to one page,
since an attachment name is unique per page; that is refused rather
than silently overwriting.

```
markfluence attachment-upload PAGE FILE... [flags]
```

### Examples

```
  # Upload one file, or several
  markfluence attachment-upload 1234567890 diagram.png
  markfluence attachment-upload 1234567890 report.pdf notes.txt

  # Store it under the path a markdown image would reference
  markfluence attachment-upload 1234567890 img.png --name assets/diagram.png

  # Re-upload even though the checksum matches
  markfluence attachment-upload 1234567890 diagram.png --force

```

### Options

```
      --dry-run       Preview what would be uploaded without writing to Confluence.
      --force         Upload even when the checksum shows the attachment is unchanged.
  -h, --help          help for attachment-upload
      --name string   Attachment name, given as a path (requires a single FILE).
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

