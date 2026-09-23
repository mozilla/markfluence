## markfluence find

Find Confluence pages and folders by their exact title

### Synopsis

Find the Confluence pages and folders whose title is TITLE.

The match is exact, and it ignores case. It is not a search for part of a
title. To search the text of pages, use search.

find reports current pages and archived pages. An archived page is not in the
page tree, but it still reserves its title. Thus you cannot create a page with
that title in the same space.

find also reports folders, because a folder id can be a parent. A folder does
not reserve a title. Thus a folder never stops you from creating a page. find
shows it so that you can find it, and not as a warning.

If find finds nothing, that is a success. It says so, and exits with 0.

```
markfluence find TITLE [flags]
```

### Examples

```
  # Find every page, archived page, and folder with this exact title
  markfluence find "Deploy runbook"

  # Find only in one space
  markfluence find "Deploy runbook" --space ENG

  # Print only the ids of the pages
  markfluence find "Deploy runbook" --json | jq -r '.results[] | select(.type=="page") | .id'

```

### Options

```
  -h, --help           help for find
      --space string   Find only in this space, by its key. An unknown key is an error, and not an empty result.
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

