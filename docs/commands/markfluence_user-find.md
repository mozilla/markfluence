## markfluence user-find

Find the account id of a Confluence user, and the Markdown to mention them

### Synopsis

Find the Confluence users whose display name matches NAME.

The second line of each result is the answer. Paste it into a Markdown body,
and it publishes as a real Confluence mention. It is easy to get two things
wrong when you write that line by hand. The host is Atlassian Home, and not
your site. The "@" at the start of the link text is what makes it a mention,
and not a plain link to the profile of a person.

NAME matches from the start of a word, in sequence. "kahn" and "william kahn"
both find William Kahn-Greene. "ahn" and "kahn william" find nobody. Thus a
part of a word that starts in the middle gives no matches, and not an error.
Use a full part of the name.

user-find does not show deactivated accounts. Confluence removes them from the
user directory, and no filter brings them back. Thus you cannot find a
colleague who left, although a mention of them on a page still shows their
name.

If user-find finds nobody, that is a success. It says so, and exits with 0.

```
markfluence user-find NAME [flags]
```

### Examples

```
  # Show the account id and the line to paste
  markfluence user-find kahn

  # Show every person with a common surname
  markfluence user-find reid --limit all

  # Print only the mention, for a script
  markfluence user-find kahn --json | jq -r '.results[0].mention'

```

### Options

```
  -h, --help           help for user-find
      --limit string   How many matches to show: a positive number, or "all". (default "10")
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

