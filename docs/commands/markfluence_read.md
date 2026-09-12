## markfluence read

Fetch a Confluence page and print its body

### Synopsis

Fetch a Confluence page and print its body to stdout.

PAGE is a numeric page id, a Confluence page URL (the modern
/wiki/.../pages/<id>/... form or a legacy ?pageId=<id> URL), or a
markdown file whose frontmatter has a page_id.

It composes with shell redirection.

--format markdown (the default) carries
title/space/parent/page_id/labels/page_width frontmatter and is a
best-effort inverse of what create/update publish. The Confluence API has
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

