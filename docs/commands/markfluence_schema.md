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

