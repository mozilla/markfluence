// Command gendocs renders every markfluence command's --help into
// docs/commands/ as markdown, so the command reference is browsable on GitHub
// without installing anything.
//
// The output is generated and checked in, which is a second copy of the help
// text -- deliberately, and only because it cannot drift: `make check` fails
// when the checked-in files disagree with the binary. A hand-maintained copy is
// the thing #102 spent its time removing.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra/doc"

	"github.com/mozilla/markfluence/cmd"
)

func main() {
	dir := "docs/commands"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	root := cmd.Root()
	// No timestamp footer: it would make every regeneration a diff and turn the
	// drift check into noise.
	root.DisableAutoGenTag = true
	// The generated files are the reference; a "completion" page for cobra's
	// own generated command is not.
	root.InitDefaultCompletionCmd()
	for _, c := range root.Commands() {
		if c.Name() == "completion" || c.Name() == "help" {
			c.Hidden = true
		}
	}

	if err := doc.GenMarkdownTree(root, dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wrote command docs to %s\n", filepath.Clean(dir))
}
