// Package completion holds the shell-completion functions the commands share.
// Cobra generates the completion scripts themselves (`markfluence completion
// <shell>`); what those scripts offer for a given argument or flag value comes
// from here.
//
// Completion runs on every keystroke, so nothing in this package may talk to
// Confluence: an argument whose values only the server knows (an attachment
// name, a page id) completes to nothing rather than stalling the shell.
package completion

import (
	"fmt"

	"github.com/spf13/cobra"
)

// MarkdownFiles completes an argument that names a markdown file: directories
// plus *.md, the only extension markfluence reads. It fits a PAGE argument
// too -- the numeric-id and URL forms are typed out, so filtering to .md just
// makes the third form completable.
func MarkdownFiles(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return []string{"md"}, cobra.ShellCompDirectiveFilterFileExt
}

// PageThenFiles completes a `PAGE FILE...` argument list: markdown for PAGE,
// then any file, since the uploads that follow are attachments of any type.
func PageThenFiles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return MarkdownFiles(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveDefault
}

// PageThenNames completes a `PAGE [NAME...]` argument list: markdown for PAGE,
// then nothing. The names are attachments on the server, which completion may
// not go fetch, and offering local filenames instead would be wrong.
func PageThenNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return MarkdownFiles(cmd, args, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// Directories completes a flag value that names a directory.
func Directories(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return nil, cobra.ShellCompDirectiveFilterDirs
}

// RegisterFlag attaches a completion function to a flag. The only way this
// fails is naming a flag that doesn't exist, which is a wiring mistake in an
// init function -- panicking surfaces it on the first run rather than silently
// dropping the completion.
func RegisterFlag(cmd *cobra.Command, flag string,
	fn func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective)) {
	if err := cmd.RegisterFlagCompletionFunc(flag, fn); err != nil {
		panic(fmt.Sprintf("registering completion for --%s on %q: %v", flag, cmd.Name(), err))
	}
}

// Values completes a flag whose value comes from a fixed vocabulary.
func Values(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return cobra.FixedCompletions(values, cobra.ShellCompDirectiveNoFileComp)
}
