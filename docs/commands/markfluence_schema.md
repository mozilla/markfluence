## markfluence schema

Print the JSON Schema of the --json output

### Synopsis

Print the JSON Schema (draft 2020-12) of the --json output of markfluence.
Thus a script, a CI job, or an agent can get the contract from the binary, and
not from the repository.

The build puts the schema into the binary. It describes schema_version 1,
which is the version that this binary writes. The schema command, and the tests
that check real --json output, read the same copy.

The output is the schema document itself, so --json has no effect here.

```
markfluence schema [flags]
```

### Examples

```
  # Save the schema
  markfluence schema > schema.json

  # Show which commands write a --json envelope
  markfluence schema | jq -r '.properties.command.enum | join(" ")'

```

### Options

```
  -h, --help   help for schema
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

