## markfluence user-info

Show who the credentials belong to, or who an account id names

### Synopsis

With no argument, show the account that the configured credentials belong to.
Ask this first when:

  - a publish goes to an unexpected place
  - Confluence refuses a token
  - the history of a page names an account that you do not know

No other markfluence command can tell you who you are.

With an ACCOUNT_ID, show that account. Give an id, and not a name. To find an
account by name, use markfluence user-find. The two commands see different
things. user-find searches a directory that cannot see deactivated accounts.
user-info can resolve a deactivated account. Thus an id from an old page names
its person here, and nowhere else.

"type" tells a person (atlassian) from a service account (app). That
difference explains most surprises about permissions. "external" and "guest"
show a restricted account.

With no argument, user-info also surveys every space that the credentials can
see. It shows where they can create pages, and which spaces they administer.
The survey walks the directory of spaces, so this form takes a few seconds.

The ACCOUNT_ID form has no survey, on purpose. Confluence answers the survey
only for the authenticated account, and takes no account id. If user-info
showed it next to the name of a different person, it would give them your
access. To find where a different person can publish, markfluence would have
to read the grants of every space and resolve their groups. That is more than a
thousand requests.

"write access" means that the account can create pages in a space. Permission
to edit a page that exists is not a space grant at all. Thus a space in this
list can still refuse one page.

user-info only reads. It writes nothing to Confluence or to disk.

```
markfluence user-info [ACCOUNT_ID] [flags]
```

### Examples

```
  # Who am I, and where can these credentials publish?
  markfluence user-info

  # Who is this account id on an old page?
  markfluence user-info 60c36d0718e9f60071326951

  # Print only the spaces, as data
  markfluence user-info --json | jq '.results[0].spaces'
```

### Options

```
  -h, --help   help for user-info
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

