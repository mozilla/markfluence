## markfluence attachment-upload

Upload or replace attachments on a Confluence page

### Synopsis

Upload files as attachments to a Confluence page, or replace attachments that
are already there.

PAGE is a page id, a Confluence page URL, or a Markdown file that names a
page_id in its frontmatter or in its pages: entry.

markfluence attaches each file with its base name. It records the path of the
file, relative to the documentation root, in the attachment comment.

markfluence skips a file when its content already agrees with the attachment on
the page. create and update use the same checksum, so a manual upload and a
publish agree on which version is current. --force uploads anyway.

--name takes a path, not a name, and it works with one FILE only. For example,
--name assets/x.png makes the attachment that the image ![](assets/x.png)
resolves to. Confluence stores it as x.png, and markfluence records the path
assets/x.png.

An attachment name is unique on a page. Thus markfluence refuses two files that
have the same base name, and it does not overwrite one with the other.

```
markfluence attachment-upload PAGE FILE... [flags]
```

### Examples

```
  # Upload one file, or more than one
  markfluence attachment-upload 1234567890 diagram.png
  markfluence attachment-upload 1234567890 report.pdf notes.txt

  # Store the file with the path that a Markdown image would reference
  markfluence attachment-upload 1234567890 img.png --name assets/diagram.png

  # Upload again, also when the checksum agrees
  markfluence attachment-upload 1234567890 diagram.png --force

```

### Options

```
      --dry-run       Show what attachment-upload would upload, and write nothing to Confluence.
      --force         Upload the file, also when the checksum shows that the attachment did not change.
  -h, --help          help for attachment-upload
      --name string   Attachment path, such as assets/x.png. The name is its base name. Works with one FILE only.
```

### Options inherited from parent commands

```
      --cloud-id string   Atlassian cloud ID. Set it only for a scoped API token. If not set, markfluence uses $CONFLUENCE_CLOUD_ID, then .env
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   Env file to read credentials from. The default is .env in the documentation root of the working directory, or in the working directory if there is no markfluence.yaml
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
      --url string        Confluence site URL. If not set, markfluence uses $CONFLUENCE_URL, then .env
      --username string   Confluence username (your email address). If not set, markfluence uses $CONFLUENCE_USERNAME, then .env
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish Markdown to Confluence

