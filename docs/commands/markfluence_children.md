## markfluence children

List the pages and folders under a Confluence page, folder, or space

### Synopsis

List the pages and folders under a Confluence page or folder.

PAGE is a numeric id, a Confluence page or folder URL, or a markdown
file whose frontmatter has a page_id.

Pass --space KEY instead of a PAGE to list a whole space. Depth 1 is
then the space's top level, which is usually just its homepage, so
--depth 2 or --depth all is what shows the tree. Walking a space costs
one pair of requests per page and folder in it.

Folders are listed alongside pages, with a TYPE column, because a
folder can hold the only pages in a subtree -- listing pages alone
would show nothing for a folder that contains folders.

A folder counts as a level: at the default --depth 1 a child folder
appears as a row, and --depth 2 shows what is inside it.

```
markfluence children [PAGE] [flags]
```

### Examples

```
  # Direct children of a page
  markfluence children 1234567890

  # Deeper, or the whole subtree
  markfluence children 1234567890 --depth 3
  markfluence children 1234567890 --depth all

  # By folder URL, or by the file that publishes to a page
  markfluence children "https://org.atlassian.net/wiki/spaces/ENG/folder/1234567890"
  markfluence children docs/index.md

  # A whole space, and every page and folder in it
  markfluence children --space ENG
  markfluence children --space ENG --depth all

  # Just the page ids
  markfluence children 1234567890 --json | jq -r '.results[] | select(.type=="page") | .id'

```

### Options

```
      --depth string   How deep to recurse: a positive number, or "all". (default "1")
  -h, --help           help for children
      --space string   List a whole space, by key, instead of a PAGE.
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

