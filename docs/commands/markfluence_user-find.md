## markfluence user-find

Find a Confluence user's account id and mention markdown

### Synopsis

Find Confluence users whose display name matches NAME.

The second line of each hit is the answer: paste it into a markdown
body and it publishes as a real Confluence mention. Two things about
that line are easy to get wrong by hand -- the host is Atlassian Home
and not your site, and the "@" on the link text is what makes it a
mention rather than an ordinary link to somebody's profile.

NAME matches from the start of a word, in order. "kahn" and
"william kahn" both find William Kahn-Greene; "ahn" and "kahn
william" find nobody. A fragment that starts mid-word therefore
reports no matches rather than an error, so try a whole name part.

Deactivated accounts are not reported. Confluence leaves them out of
the user directory entirely, and no filter brings them back -- so a
departed colleague cannot be found here, even though a mention of one
already in a page still resolves to their name.

Finding nobody is a success: the command says so and exits 0.

```
markfluence user-find NAME [flags]
```

### Examples

```
  # The account id and the line to paste
  markfluence user-find kahn

  # A common surname, all of them
  markfluence user-find reid --limit all

  # Just the mention, for a script
  markfluence user-find kahn --json | jq -r '.results[0].mention'

```

### Options

```
  -h, --help           help for user-find
      --limit string   How many matches to show: a positive number, or "all". (default "10")
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

