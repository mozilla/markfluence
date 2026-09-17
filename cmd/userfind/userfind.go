// Package userfind implements the `markfluence user-find` command: resolve a
// person's name to the account id a mention needs, and to the markdown line
// that mentions them.
package userfind

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "user-find"

// limitAll is the --limit value meaning "every match".
const limitAll = "all"

// defaultLimit is the --limit default.
//
// Low for search's reason -- a hit is a two-line block rather than a row -- and
// a bound rather than "all" because a name fragment matches far more people
// than an author expects: a single letter matched 304 accounts on the instance
// this was measured against.
const defaultLimit = "10"

var limitOpt string

// Cmd is the user-find command.
var Cmd = &cobra.Command{
	Use:   command + " NAME",
	Short: "Find a Confluence user's account id and mention markdown",
	Long: "Find Confluence users whose display name matches NAME.\n\n" +
		"The second line of each hit is the answer: paste it into a markdown\n" +
		"body and it publishes as a real Confluence mention. Two things about\n" +
		"that line are easy to get wrong by hand -- the host is Atlassian Home\n" +
		"and not your site, and the \"@\" on the link text is what makes it a\n" +
		"mention rather than an ordinary link to somebody's profile.\n\n" +
		"NAME matches from the start of a word, in order. \"kahn\" and\n" +
		"\"william kahn\" both find William Kahn-Greene; \"ahn\" and \"kahn\n" +
		"william\" find nobody. A fragment that starts mid-word therefore\n" +
		"reports no matches rather than an error, so try a whole name part.\n\n" +
		"Deactivated accounts are not reported. Confluence leaves them out of\n" +
		"the user directory entirely, and no filter brings them back -- so a\n" +
		"departed colleague cannot be found here, even though a mention of one\n" +
		"already in a page still resolves to their name.\n\n" +
		"Finding nobody is a success: the command says so and exits 0.",
	Example: "  # The account id and the line to paste\n" +
		"  markfluence user-find kahn\n\n" +
		"  # A common surname, all of them\n" +
		"  markfluence user-find reid --limit all\n\n" +
		"  # Just the mention, for a script\n" +
		"  markfluence user-find kahn --json | jq -r '.results[0].mention'\n",
	Args: cobra.ExactArgs(1),
	// A name is free text, and completion may not ask Confluence for one:
	// completion runs on every keystroke.
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&limitOpt, "limit", defaultLimit,
		fmt.Sprintf("How many matches to show: a positive number, or %q.", limitAll))
	completion.RegisterFlag(Cmd, "limit", completion.Values("5", "10", "25", limitAll))
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")

	// Both checks are usage errors needing no server. The empty-name one
	// especially: the route answers an empty match with a 500 carrying a Java
	// exception, which would surface as an operational failure with the wrong
	// exit code (docs/confluence/users.md).
	name := args[0]
	if strings.TrimSpace(name) == "" {
		return fatalFail("no name given: NAME must not be empty", jsonout.CodeValidation)
	}
	limit, err := parseLimit(limitOpt)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	matches, more, err := c.SearchUsers(name, limit)
	if err != nil {
		return operationalFail(err, jsonout.CodeFor(err))
	}
	return report(matches, more)
}

// report writes the matches, in --json or for a human.
func report(matches []client.UserMatch, more bool) error {
	if ui.IsJSON() {
		results := make([]any, 0, len(matches))
		for _, m := range matches {
			results = append(results, buildResult(m))
		}
		return jsonout.Emit(os.Stdout, jsonout.NewEnvelope(command, results,
			buildSummary(matches, more)))
	}

	if len(matches) == 0 {
		ui.Info("No users found.")
		return nil
	}
	fmt.Println(blocks(matches))
	if more {
		// ui.Hint, not ui.Info, so stdout stays nothing but the hits. That
		// matters more here than it does for search, whose hits nobody pipes:
		// these lines exist to be redirected into a file or a clipboard, and a
		// notice about --limit is not a line anybody wants pasted into a page.
		// children --space hints for the same reason. Hint supplies its own
		// leading blank line, so the notice cannot read as part of a block.
		ui.Hint(fmt.Sprintf("Showing %d matches; more exist (use --limit %s).",
			len(matches), limitAll))
	}
	return nil
}

// blocks renders the matches as a block per hit: who they are, then the line to
// paste. A block rather than a table row because the mention line is far too
// long for a column, and it is what the author came for.
func blocks(matches []client.UserMatch) string {
	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.DisplayName + "  " + m.AccountID + "\n")
		b.WriteString("  " + convert.MentionMarkdown(m.DisplayName, m.AccountID) + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// parseLimit turns the --limit value into a row bound, 0 meaning unbounded.
//
// 0 is refused rather than read as unlimited, exactly as search's --limit and
// children's --depth refuse it: elsewhere 0 often means "unlimited", and
// dumping every match for somebody who meant "none" is worse than an error
// naming the word that does mean unlimited.
func parseLimit(v string) (int, error) {
	if v == limitAll {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("invalid --limit %q: want a positive number or %q", v, limitAll)
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

// operationalFail reports the lookup itself failing, exiting 1.
//
// Like find and search, and unlike the per-page commands, this writes an error
// object rather than a results[0] failure: those name the page they were asked
// about, and a name is not an id this could report as having failed.
func operationalFail(err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, err.Error(), code)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}
