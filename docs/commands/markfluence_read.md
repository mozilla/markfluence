## markfluence read

Fetch a Confluence page and print its body

### Synopsis

Fetch a Confluence page and print its body to stdout.

PAGE is a numeric page id, a Confluence page URL (the modern
/wiki/.../pages/<id>/... form or a legacy ?pageId=<id> URL), or a
markdown file whose frontmatter has a page_id.

It composes with shell redirection.

--format markdown (the default) carries title/space/parent/page_id/
labels/page_status/page_width frontmatter and is a best-effort inverse
of what create/update publish. The Confluence API has
no markdown representation, so the storage body is converted here:
constructs markfluence emits round-trip faithfully, while editor-authored
content degrades gracefully -- a macro markfluence does not map, and a
column layout, pass through as raw storage tags with their bodies kept as
readable markdown, so they publish back unchanged. Some transforms are
lossy (a table cell colour outside the named swatches comes back as a
literal hex), so this is a reading aid rather than a guaranteed source
round-trip.

--format storage prints the raw storage-format XHTML exactly as stored.

```
markfluence read PAGE [flags]
```

### Examples

```
  # Markdown, with frontmatter, to stdout
  markfluence read 1234567890

  # Save it as a file you can edit and publish back
  markfluence read 1234567890 > page.md

  # The raw storage Confluence holds
  markfluence read 1234567890 --format storage > page.storage.xml

  # By URL
  markfluence read "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title"
```

### Options

```
      --format string   Output format: markdown (default) or storage (default "markdown")
  -h, --help            help for read
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

