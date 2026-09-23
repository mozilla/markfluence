## markfluence search

Find Confluence pages by a search of their text

### Synopsis

Find the Confluence pages whose text matches QUERY.

search compares QUERY with all the text of each page, and not only the title.
Each word must be somewhere in the page, in any sequence. This is not a phrase
search, so quotes around a phrase do not make the words come next to each other.

The results come in the relevance order of Confluence, best first. --limit sets
how many search shows. When there are more matches, search says so.

search never returns archived pages, because the search index cannot see them.
It also never returns folders, because a folder has no text to match. Archived
pages and folders are not in the results, so use find for both.

If search finds nothing, that is a success. It says so, and exits with 0.

```
markfluence search QUERY [flags]
```

### Examples

```
  # Search the text. Each word must be somewhere in the page
  markfluence search "deploy runbook"

  # Search only in one space, and show more results
  markfluence search "deploy runbook" --space ENG --limit 25

  # Show every match, and print only the ids
  markfluence search deploy --limit all --json | jq -r '.results[].id'

  # Give a raw CQL query, which markfluence sends with no change
  markfluence search 'type = page and label = "runbook"' --cql

```

### Options

```
      --cql            Send QUERY as a raw CQL query, and not as text to search for. Not with --space, or with a --type that you set. Put those clauses in the query.
  -h, --help           help for search
      --limit string   How many matches to show: a positive number, or "all". (default "10")
      --space string   Search only in this space, by its key. An unknown key is an error, and not an empty result.
      --type string    Type of content to search: "page", "blogpost", or "all". (default "page")
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

