## markfluence user-find

Find a user, and the Markdown that mentions them

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
  -d, --debug             Print debug details, such as each retry decision
      --env-file string   File to read credentials from, before the environment and your credentials file
      --json              Write JSON, and no human output. A result goes to stdout. A fatal error goes to stderr as a JSON error object
      --no-color          Print output with no color
      --root string       Documentation root for every file. The default is the nearest directory above each file that has a markfluence.yaml, or the directory of the file if there is none
```

### SEE ALSO

* [markfluence](markfluence.md)	 - Publish Markdown to Confluence

