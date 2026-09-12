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

