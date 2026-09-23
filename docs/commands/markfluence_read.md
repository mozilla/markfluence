## markfluence read

Get a Confluence page and print its body

### Synopsis

Get a Confluence page and print its body to stdout. You can redirect the
output to a file.

PAGE is a page id, a Confluence page URL, or a Markdown file that names a
page_id in its frontmatter or in its pages: entry. A URL can have the usual
/wiki/.../pages/<id>/... form, or the older ?pageId=<id> form.

--format markdown is the default. The output has title, space, parent,
page_id, labels, page_status, and page_width in the frontmatter. It is the
inverse of what create and update publish, as far as that is possible.

The Confluence API has no Markdown form, so markfluence converts the storage
format itself. The constructs that markfluence writes come back correctly. For
content from the Confluence editor, some details change:

  - A macro that markfluence does not map, and a column layout, come back as
    raw storage tags. Their bodies stay as Markdown that you can read, and they
    publish back with no change.
  - Some conversions lose detail. For example, a cell color that is not one of
    the named swatches comes back as a literal hex value.

Thus the output helps you read a page, but it is not a guaranteed round trip.

--format storage prints the raw storage format XHTML exactly as Confluence
stores it.

```
markfluence read PAGE [flags]
```

### Examples

```
  # Print Markdown, with frontmatter, to stdout
  markfluence read 1234567890

  # Save it as a file that you can edit and publish back
  markfluence read 1234567890 > page.md

  # Print the raw storage format that Confluence holds
  markfluence read 1234567890 --format storage > page.storage.xml

  # Give a URL
  markfluence read "https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title"
```

### Options

```
      --format string   Output format: markdown (the default) or storage (default "markdown")
  -h, --help            help for read
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

