// Package children implements the `markfluence children` command: list the pages
// and folders under a page or folder.
package children

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagetree"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "children"

// depthAll is the --depth value meaning "however deep it goes".
const depthAll = "all"

var (
	depthOpt string
	spaceOpt string
)

// Cmd is the children command.
var Cmd = &cobra.Command{
	Use:   command + " [PAGE]",
	Short: "List the pages and folders under a Confluence page, folder, or space",
	Long: "List the pages and folders under a Confluence page or folder.\n\n" +
		"PAGE is a numeric id, a Confluence page or folder URL, or a markdown\n" +
		"file whose frontmatter has a page_id.\n\n" +
		"Pass --space KEY instead of a PAGE to list a whole space. Depth 1 is\n" +
		"then the space's top level, which is usually just its homepage, so\n" +
		"--depth 2 or --depth all is what shows the tree. Walking a space costs\n" +
		"one pair of requests per page and folder in it.\n\n" +
		"Folders are listed alongside pages, with a TYPE column, because a\n" +
		"folder can hold the only pages in a subtree -- listing pages alone\n" +
		"would show nothing for a folder that contains folders.\n\n" +
		"A folder counts as a level: at the default --depth 1 a child folder\n" +
		"appears as a row, and --depth 2 shows what is inside it.",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&depthOpt, "depth", "1",
		`How deep to recurse: a positive number, or "all".`)
	Cmd.Flags().StringVar(&spaceOpt, "space", "",
		"List a whole space, by key, instead of a PAGE.")
	completion.RegisterFlag(Cmd, "depth", completion.Values("1", "2", "3", depthAll))
	// A space key lives on the server, and completion runs on every keystroke,
	// so it completes to nothing rather than stalling the shell.
	completion.RegisterFlag(Cmd, "space", cobra.NoFileCompletions)
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")

	// Before the credential check: neither of these needs a server to be
	// recognized as a usage error.
	if err := checkTarget(args, spaceOpt); err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}
	depth, err := parseDepth(depthOpt)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	var nodes []pagetree.Node
	if spaceOpt != "" {
		// The key is resolved rather than handed straight to the walk, even
		// though the route it feeds takes a key: an unknown key is the user's
		// typo and deserves to be named as one, and the v1 route reports it as a
		// 404 -- which is also what a rejected credential looks like.
		spaceID, err := c.ResolveSpaceID(spaceOpt)
		if err != nil {
			return spaceFail(err, jsonout.CodeFor(err))
		}
		if spaceID == "" {
			return fatalFail(fmt.Sprintf("space %q not found", spaceOpt), jsonout.CodeValidation)
		}
		nodes, err = pagetree.WalkSpace(c, spaceOpt, depth)
		if err != nil {
			return spaceFail(err, jsonout.CodeFor(err))
		}
	} else {
		id, err := pageref.Resolve(args[0])
		if err != nil {
			return fatalFail(err.Error(), jsonout.CodeValidation)
		}
		nodes, err = pagetree.Walk(c, id, depth)
		if err != nil {
			return operationalFail(id, err, jsonout.CodeFor(err))
		}
	}

	if ui.IsJSON() {
		results := make([]any, 0, len(nodes))
		for _, n := range nodes {
			results = append(results, buildResult(n))
		}
		env := jsonout.NewEnvelope(command, results,
			map[string]int{"total": len(nodes), "succeeded": len(nodes), "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}

	if len(nodes) == 0 {
		ui.Info("No children.")
		return nil
	}
	fmt.Println(tree(nodes))
	if spaceOpt != "" && !cmd.Flags().Changed("depth") {
		// A space's top level is usually one row -- its homepage -- which reads
		// like the whole answer. On stderr, so it explains the table without
		// joining it; a --json consumer never sees it at all, and the row it
		// would explain is already in the array.
		ui.Hint("Showing the space's top level. Use --depth 2, or --depth all for the whole tree.")
	}
	return nil
}

// checkTarget requires exactly one of PAGE and --space.
//
// They are alternatives rather than a filter and a target: --space names the
// root, so combining them would mean two roots, and neither of them means
// nothing to walk.
func checkTarget(args []string, space string) error {
	switch {
	case len(args) == 0 && space == "":
		return fmt.Errorf("no page given: pass a PAGE, or --space KEY to list a whole space")
	case len(args) > 0 && space != "":
		return fmt.Errorf("PAGE and --space cannot be combined: --space lists a whole space")
	}
	return nil
}

// parseDepth turns the --depth value into a pagetree depth.
//
// 0 and negatives are refused rather than reinterpreted: elsewhere 0 often means
// "unlimited", and silently launching an unbounded walk for someone who meant
// "none" is worse than an error naming the value that does mean unlimited.
func parseDepth(v string) (int, error) {
	if v == depthAll {
		return pagetree.AllDepths, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid --depth %q: want a positive number or %q", v, depthAll)
	}
	return n, nil
}

// fatalFail reports a config/usage/pre-flight failure: a JSON error object on
// stderr under --json, else a human error line, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports an operational failure for the node: under --json a
// results[0] entry {ok:false,error,code}, else a human error line, exiting 1.
func operationalFail(id string, err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.Emit(os.Stdout, failEnvelope(id, err, code))
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// spaceFail reports a --space walk failing, exiting 1.
//
// An error object on stderr rather than the results[0] failure operationalFail
// writes, for the reason find and search do the same: that shape names the page
// it was asked about, and a space walk was asked about no page. Putting the
// space key in its page_id would hand a --json consumer an id that resolves to
// nothing.
func spaceFail(err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, err.Error(), code)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// failEnvelope is the document operationalFail writes, split out so the schema
// conformance test can validate the envelope this command really emits instead
// of a hand-copied duplicate of it.
func failEnvelope(id string, err error, code jsonout.Code) jsonout.Envelope {
	return jsonout.NewEnvelope(command, []any{jsonout.NewSingleOpFailure(id, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
}

// tree renders the nodes as an indented listing, depth-first order as walked.
// Titles are indented by depth so the shape is visible; the other columns stay
// aligned, which is what makes the output greppable as well as readable.
func tree(nodes []pagetree.Node) string {
	type row struct{ kind, id, title string }
	rows := []row{{"TYPE", "ID", "TITLE"}}
	for _, n := range nodes {
		rows = append(rows, row{
			kind:  n.Type,
			id:    n.ID,
			title: strings.Repeat("  ", n.Depth-1) + n.Title,
		})
	}

	var kindW, idW int
	for _, r := range rows {
		if n := len([]rune(r.kind)); n > kindW {
			kindW = n
		}
		if n := len([]rune(r.id)); n > idW {
			idW = n
		}
	}

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(r.kind + strings.Repeat(" ", kindW-len([]rune(r.kind))) + "  ")
		b.WriteString(r.id + strings.Repeat(" ", idW-len([]rune(r.id))) + "  ")
		// Last column: no padding, so there are no trailing spaces.
		b.WriteString(r.title)
	}
	return b.String()
}
