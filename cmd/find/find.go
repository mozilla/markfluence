// Package find implements the `markfluence find` command: resolve a title to
// the pages and folders that carry it.
package find

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "find"

var spaceOpt string

// Cmd is the find command.
var Cmd = &cobra.Command{
	Use:   command + " TITLE",
	Short: "Find Confluence pages and folders by their exact title",
	Long: "Find the Confluence pages and folders whose title is TITLE.\n\n" +
		"The match is exact, and it ignores case. It is not a search for part of a\n" +
		"title. To search the text of pages, use search.\n\n" +
		"find reports current pages and archived pages. An archived page is not in the\n" +
		"page tree, but it still reserves its title. Thus you cannot create a page with\n" +
		"that title in the same space.\n\n" +
		"find also reports folders, because a folder id can be a parent. A folder does\n" +
		"not reserve a title. Thus a folder never stops you from creating a page. find\n" +
		"shows it so that you can find it, and not as a warning.\n\n" +
		"If find finds nothing, that is a success. It says so, and exits with 0.",
	Example: "  # Find every page, archived page, and folder with this exact title\n" +
		"  markfluence find \"Deploy runbook\"\n\n" +
		"  # Find only in one space\n" +
		"  markfluence find \"Deploy runbook\" --space ENG\n\n" +
		"  # Print only the ids of the pages\n" +
		"  markfluence find \"Deploy runbook\" --json | jq -r '.results[] | select(.type==\"page\") | .id'\n",
	Args: cobra.ExactArgs(1),
	// Nothing here is completable: a title is free text and a space key lives
	// on the server, which completion may not go ask for.
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&spaceOpt, "space", "",
		"Find only in this space, by its key. An unknown key is an error, and not an "+
			"empty result.")
	completion.RegisterFlag(Cmd, "space", cobra.NoFileCompletions)
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")

	// Before the credential check: an empty title is a usage error and needs no
	// server to recognize. It would also be rejected downstream -- the CQL half
	// 400s on an empty string literal -- but as an API error rather than the
	// usage error it is.
	title := args[0]
	if strings.TrimSpace(title) == "" {
		return fatalFail("no title given: TITLE must not be empty", jsonout.CodeValidation)
	}

	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	matches, err := c.FindByTitle(title, spaceOpt)
	if err != nil {
		// An unknown space key is the user's typo, not a failure of the search.
		if errors.Is(err, client.ErrSpaceNotFound) {
			return fatalFail(fmt.Sprintf("space %q not found", spaceOpt), jsonout.CodeValidation)
		}
		return operationalFail(err, jsonout.CodeFor(err))
	}

	if ui.IsJSON() {
		results := make([]any, 0, len(matches))
		for _, m := range matches {
			results = append(results, buildResult(m))
		}
		env := jsonout.NewEnvelope(command, results,
			map[string]int{"total": len(matches), "succeeded": len(matches), "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}

	if len(matches) == 0 {
		ui.Info("No matches found.")
		return nil
	}
	fmt.Println(table(matches))
	return nil
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

// operationalFail reports the search itself failing, exiting 1.
//
// Unlike the per-page commands this writes an error object rather than a
// results[0] failure: those name the page they were asked about, and there is
// no such id here. Reporting the successful half of the search instead would be
// worse than reporting nothing -- an empty result is what a caller acts on by
// creating the page.
func operationalFail(err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, err.Error(), code)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// table renders the matches as aligned columns. Every column but the last is
// padded, so the output stays greppable without trailing whitespace.
func table(matches []client.TitleMatch) string {
	rows := [][]string{{"TYPE", "ID", "SPACE", "STATUS", "TITLE", "URL"}}
	for _, m := range matches {
		rows = append(rows, []string{
			m.Type, m.ID, orDash(m.Space), m.Status, m.Title, orDash(m.URL),
		})
	}

	widths := make([]int, len(rows[0]))
	for _, r := range rows {
		for i, cell := range r {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, cell := range r {
			if j == len(r)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell + strings.Repeat(" ", widths[j]-len([]rune(cell))) + "  ")
		}
	}
	return b.String()
}

// orDash renders an underivable space key or URL as "-" rather than a blank
// column, so a row with one missing still reads as a row.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
