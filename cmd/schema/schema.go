// Package schema implements the `markfluence schema` command: print the JSON
// Schema for --json output.
package schema

import (
	"fmt"

	"github.com/mozilla/markfluence/internal/jsonout"
	schemadoc "github.com/mozilla/markfluence/schema"
	"github.com/spf13/cobra"
)

// Cmd is the schema command.
var Cmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the JSON Schema of the --json output",
	Long: fmt.Sprintf("Print the JSON Schema (draft 2020-12) of the --json output of markfluence.\n"+
		"Thus a script, a CI job, or an agent can get the contract from the binary, and\n"+
		"not from the repository.\n\n"+
		"The build puts the schema into the binary. It describes schema_version %d,\n"+
		"which is the version that this binary writes. Validate against this copy, and\n"+
		"not against a copy from another release.\n\n"+
		"The schema is open: an object can have keys that the schema does not list. A\n"+
		"later release can add a key and keep the same schema_version, so a consumer must\n"+
		"ignore a key that it does not know. A change that can break a consumer, such as\n"+
		"a key that is removed or renamed, increases schema_version. docs/json-output.md\n"+
		"has the whole rule.\n\n"+
		"The output is the schema document itself, so --json has no effect here.",
		jsonout.SchemaVersion),
	Example: "  # Save the schema\n" +
		"  markfluence schema > schema.json\n\n" +
		"  # Show which commands write a --json envelope\n" +
		"  markfluence schema | jq -r '.properties.command.enum | join(\" \")'\n",
	Args: cobra.NoArgs,
	// The command takes no arguments; without this, completion would offer every
	// file in the directory.
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              run,
}

func run(cmd *cobra.Command, _ []string) error {
	// Verbatim, newline-terminated bytes: what is printed is byte-identical to
	// the published schema file, which is what lets a consumer diff or cache it.
	if _, err := fmt.Fprint(cmd.OutOrStdout(), schemadoc.V1); err != nil {
		return fmt.Errorf("writing schema: %w", err)
	}
	return nil
}
