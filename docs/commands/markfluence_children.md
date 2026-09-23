## markfluence children

List the pages and folders under a Confluence page, folder, or space

### Synopsis

List the pages and folders under a Confluence page or folder.

PAGE is a page id or folder id, or a Confluence page URL or folder URL. It can
also be a Markdown file that names a page_id in its frontmatter or in its
pages: entry.

To list a whole space, give --space KEY and no PAGE. Then depth 1 is the top
level of the space, which is usually only its homepage. Thus use --depth 2 or
--depth all to see the tree. A walk of a space makes two requests for each page
and folder in it.

The list shows folders next to pages, with a TYPE column. A folder can hold the
only pages in a part of the tree. If children listed only pages, a folder that
holds only folders would show nothing.

A folder counts as a level. At the default --depth 1, a child folder is a row,
and --depth 2 shows what is in it.

```
markfluence children [PAGE] [flags]
```

### Examples

```
  # List the direct children of a page
  markfluence children 1234567890

  # Go deeper, or list the whole subtree
  markfluence children 1234567890 --depth 3
  markfluence children 1234567890 --depth all

  # Give a folder URL, or the file that publishes to a page
  markfluence children "https://org.atlassian.net/wiki/spaces/ENG/folder/1234567890"
  markfluence children docs/index.md

  # List a whole space, and every page and folder in it
  markfluence children --space ENG
  markfluence children --space ENG --depth all

  # Print only the page ids
  markfluence children 1234567890 --json | jq -r '.results[] | select(.type=="page") | .id'

```

### Options

```
      --depth string   How many levels to list: a positive number, or "all". (default "1")
  -h, --help           help for children
      --space string   List a whole space, by its key, and not a PAGE.
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

