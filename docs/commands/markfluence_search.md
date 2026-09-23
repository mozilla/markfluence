## markfluence search

Find Confluence pages by full-text search

### Synopsis

Find Confluence pages whose text matches QUERY.

QUERY is matched against the page's full text, not just its title.
Multiple words are ANDed: every word must appear somewhere in the
page, in any order. It is not a phrase search, so quoting a phrase
does not require the words to be adjacent.

Results come back in Confluence's own relevance order, best first,
and are capped at --limit. When more matches exist than were shown,
the command says so rather than truncating silently.

Archived pages are never returned: the search index cannot see them.
Neither are folders, which have no text to match -- use `find` for
both of those.

Finding nothing is a success: the command says so and exits 0.

```
markfluence search QUERY [flags]
```

### Examples

```
  # Full-text search; every word must appear somewhere
  markfluence search "deploy runbook"

  # Scoped, with a bigger page of results
  markfluence search "deploy runbook" --space ENG --limit 25

  # Every match, ids only
  markfluence search deploy --limit all --json | jq -r '.results[].id'

  # Raw CQL, passed through untouched
  markfluence search 'type = page and label = "runbook"' --cql

```

### Options

```
      --cql            Treat QUERY as a raw CQL query instead of text to search for; cannot be combined with --space or an explicit --type (put those clauses in the query).
  -h, --help           help for search
      --limit string   How many matches to show: a positive number, or "all". (default "10")
      --space string   Restrict the search to a space, by key.
      --type string    Content type to search: "page", "blogpost", or "all". (default "page")
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

