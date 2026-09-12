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

