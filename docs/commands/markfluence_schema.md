## markfluence schema

Print the JSON Schema for --json output

### Synopsis

Print the JSON Schema (draft 2020-12) that markfluence's --json output
conforms to, so a script, a CI job, or an agent can fetch the contract from
the binary instead of the repository.

The schema is embedded at build time and describes schema_version 1, the
version this binary emits. Both the schema command and the tests that
validate real --json output read that same embedded copy.

The output is the schema document itself, so --json changes nothing here.

```
markfluence schema [flags]
```

### Examples

```
  # Save the schema
  markfluence schema > schema.json

  # Which commands emit a --json envelope
  markfluence schema | jq -r '.properties.command.enum | join(" ")'

```

### Options

```
  -h, --help   help for schema
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

