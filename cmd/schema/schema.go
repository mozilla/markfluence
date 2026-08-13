// Package schema implements the `markfluence schema` command: print the JSON
// Schema for --json output.
package schema

import (
	"fmt"

	schemadoc "github.com/mozilla/markfluence/schema"
	"github.com/spf13/cobra"
)

// Cmd is the schema command.
var Cmd = &cobra.Command{
	Use:   "schema",
	Short: "Print the JSON Schema for --json output",
	Long: fmt.Sprintf("Print the JSON Schema (draft 2020-12) that markfluence's --json output\n"+
		"conforms to, so a script, a CI job, or an agent can fetch the contract from\n"+
		"the binary instead of the repository.\n\n"+
		"The schema is embedded at build time and describes schema_version %d, the\n"+
		"version this binary emits. Both the schema command and the tests that\n"+
		"validate real --json output read that same embedded copy.\n\n"+
		"The output is the schema document itself, so --json changes nothing here.",
		schemadoc.Version),
	Args: cobra.NoArgs,
	// The command takes no arguments; without this, completion would offer every
	// file in the directory.
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              run,
}

func run(cmd *cobra.Command, _ []string) error {
	// Verbatim, newline-terminated bytes: what is printed is byte-identical to
	// the published schema file, which is what lets a consumer diff or cache it.
	if _, err := fmt.Fprint(cmd.OutOrStdout(), schemadoc.Latest); err != nil {
		return fmt.Errorf("writing schema: %w", err)
	}
	return nil
}
