## markfluence find

Find Confluence pages and folders by exact title

### Synopsis

Find Confluence pages and folders whose title matches TITLE.

The match is exact and case-insensitive -- not a substring search.

Both current and archived pages are reported. An archived page is
invisible in the page tree but still reserves its title, so it will
block creating a page with that title in the same space.

Folders are reported too, since a folder id is a legitimate parent.
A folder does not reserve a title, so a folder hit is never a reason
a page cannot be created -- it is there to be found, not to warn.

Finding nothing is a success: the command says so and exits 0.

```
markfluence find TITLE [flags]
```

### Examples

```
  # Every page, archived page and folder with this exact title
  markfluence find "Deploy runbook"

  # Scoped to one space
  markfluence find "Deploy runbook" --space ENG

  # Just the current page ids
  markfluence find "Deploy runbook" --json | jq -r '.results[] | select(.type=="page") | .id'

```

### Options

```
  -h, --help           help for find
      --space string   Restrict the search to a space, by key (an unknown key is an error, not an empty result).
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

