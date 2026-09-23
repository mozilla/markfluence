## markfluence page-info

Show the metadata of a Confluence page

### Synopsis

Show the metadata of a Confluence page. This is its id, title, content status,
space, parent, version, page width, page status, and labels. It also shows the
created and updated stamps, and the URL. page-info does not print an empty
field.

PAGE is a page id, a Confluence page URL, or a Markdown file that names a
page_id in its frontmatter or in its pages: entry.

Two fields use the word "status", and they are different things:

  content_status  current, archived, or trashed
  page_status     the colored lozenge next to the title

page_status/available lists the statuses that you can give to THIS page, with
the account that runs the command. That list is not a property of the space.
Confluence decides it for each page and each account, so a different page in
the same space can have more statuses or fewer. To see what a page_status: line
can say for a page, ask about that page.

--properties also lists all the content properties of the page. Confluence
keeps data such as the page width in them.

```
markfluence page-info PAGE [flags]
```

### Examples

```
  # Give a page id
  markfluence page-info 1234567890

  # Give the file that publishes to the page, and show its content properties
  markfluence page-info docs/foo.md --properties
```

### Options

```
  -h, --help         help for page-info
      --properties   Also list all the content properties of the page.
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

* [markfluence](markfluence.md)	 - Publish markdown to Confluence

