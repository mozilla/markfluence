// Package userinfo implements the `markfluence user-info` command: who the
// configured credentials belong to, or who an account id names.
package userinfo

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var withSpaces bool

// Cmd is the user-info command.
var Cmd = &cobra.Command{
	Use:   "user-info [ACCOUNT_ID]",
	Short: "Print who the credentials belong to, or who an account id names",
	Long: "With no argument, print the account the configured credentials belong\n" +
		"to. This is the question to ask first when a publish lands somewhere\n" +
		"unexpected, a token is refused, or a page's history names an account you\n" +
		"do not recognise: markfluence otherwise cannot tell you who it is.\n\n" +
		"With an ACCOUNT_ID, print that account instead. An id, never a name --\n" +
		"resolving a name is markfluence user-find. The two see different things,\n" +
		"which is why both exist: user-find searches a directory that cannot see\n" +
		"deactivated accounts at all, while this route resolves one, so an id from\n" +
		"an old page names its person here and nowhere else.\n\n" +
		"'type' tells a person (atlassian) from a service account (app), which is\n" +
		"most of why permissions surprise people, and 'external'/'guest' name a\n" +
		"restricted account directly.\n\n" +
		"--spaces additionally surveys every space **the credentials you are\n" +
		"running as** can see, and reports where they may create pages and which\n" +
		"they administer. It describes the authenticated account and nothing\n" +
		"else, so it cannot be combined with an ACCOUNT_ID: Confluence has no\n" +
		"route that answers \"where may this other person publish\". That is a\n" +
		"walk of the space directory rather than one request, which is why it is\n" +
		"opt-in.\n\n" +
		"'write access' means creating pages in a space: permission to edit an\n" +
		"existing page is not a space grant at all, so a space listed here may\n" +
		"still refuse a particular page.\n\n" +
		"Read-only. Nothing is written to Confluence or to disk.",
	Example: "  # Who am I, and can this token do anything?\n" +
		"  markfluence user-info\n\n" +
		"  # Who is this account id on an old page?\n" +
		"  markfluence user-info 60c36d0718e9f60071326951\n\n" +
		"  # Where can these credentials publish?\n" +
		"  markfluence user-info --spaces",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.Values(),
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&withSpaces, "spaces", false,
		"Also survey which spaces the account may create pages in and administer.")
}

func run(cmd *cobra.Command, args []string) error {
	accountID := ""
	if len(args) == 1 {
		if withSpaces {
			// The survey answers for the **credentials**, not for the named
			// account: GET /space?expand=operations takes no accountId and
			// reports what the authenticated user may do. Running both would
			// print this caller's writable spaces underneath somebody else's
			// name, which is a wrong answer rather than a missing one. There
			// is no route that answers it for another account -- the space
			// permissions API lists principals and groups, and resolving those
			// to "can this person publish" is a different command.
			return fatalFail(
				"--spaces describes the account you are authenticated as, not the one you "+
					"named, so the two cannot be combined; run it without an account id",
				jsonout.CodeValidation)
		}
		accountID = strings.TrimSpace(args[0])
		if accountID == "" {
			// Before the credentials: a local defect should not cost a request
			// or report itself as a missing token.
			return fatalFail("no account id given", jsonout.CodeValidation)
		}
	}
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	user, err := lookup(c, accountID)
	if err != nil {
		return operationalFail(err.Error(), jsonout.CodeFor(err))
	}
	if user == nil {
		return operationalFail(fmt.Sprintf("no account with id %q", accountID), jsonout.CodeNotFound)
	}

	rep := report{user: user, self: accountID == ""}
	if withSpaces {
		// Best-effort, like the optional reads in page-info and space-info --
		// but *said out loud*, unlike theirs. Those are implicit; this one the
		// caller asked for by name, so a silent omission is byte-identical to
		// not having passed the flag at all.
		survey, err := surveySpaces(c)
		switch {
		case err == nil:
			rep.spaces = survey
		case ui.IsJSON():
			// stderr under --json is a schema-validated document, so the note
			// travels in the envelope's warnings array instead.
			jsonout.AddWarning("could not survey spaces: " + err.Error())
		default:
			ui.Warn("could not survey spaces: " + err.Error())
		}
	}

	if ui.IsJSON() {
		env := jsonout.NewEnvelope("user-info", []any{rep.jsonResult()},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}
	fmt.Println(rep.human())
	return nil
}

// lookup asks about the caller when accountID is empty, and about that account
// otherwise. Two routes, one shape.
func lookup(c *client.ConfluenceClient, accountID string) (*client.User, error) {
	if accountID == "" {
		return c.CurrentUser()
	}
	return c.UserInfo(accountID)
}

// fatalFail reports a usage or credential failure and exits 2;
// operationalFail reports one the server gave and exits 1 -- the split
// docs/json-output.md sets out and page-info and space-info both draw.
//
// Both go to stderr rather than a results[0] entry: there is no page id to
// name, which is find/search/children --space's rule.
func fatalFail(msg string, code jsonout.Code) error { return emitFailure(msg, code, 2) }

func operationalFail(msg string, code jsonout.Code) error { return emitFailure(msg, code, 1) }

func emitFailure(msg string, code jsonout.Code, exit int) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "user-info", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(exit)
}

// report is what the command found, feeding both renderers.
type report struct {
	user *client.User
	// self records which question was asked: the human output marks the
	// no-argument form, and --json carries it as a field, so neither leaves a
	// reader guessing whether the account described is their own.
	self bool
	// spaces is nil without --spaces, and also when the survey failed.
	spaces *spaceSurvey
}

// spaceSurvey is one pass over the space directory.
//
// Write and Admin are space **keys** rather than counts: an operator asking
// "where can this publish?" wants to know which, and the count is one len()
// away. Visible is every space the account can see at all, which is what makes
// "97 of 525" meaningful rather than just "97".
type spaceSurvey struct {
	Visible int
	Write   []string
	Admin   []string
}

// surveySpaces walks the space directory once, recording where the account may
// create pages and what it administers.
//
// create:page is the write predicate and administer:space the admin one. An
// admin's row also carries archive/delete/export/manage_*, so any of them would
// do; the one Atlassian names for the permission is the one that will not drift.
func surveySpaces(c *client.ConfluenceClient) (*spaceSurvey, error) {
	survey := &spaceSurvey{}
	err := c.WalkSpaceOperations(func(space client.SpaceRef, ops []client.SpaceOperation) error {
		survey.Visible++
		for _, op := range ops {
			switch {
			case op.Operation == "create" && op.TargetType == "page":
				survey.Write = append(survey.Write, space.Key)
			case op.Operation == "administer" && op.TargetType == "space":
				survey.Admin = append(survey.Admin, space.Key)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Sorted, because the directory's own order is not meaningful and an
	// operator comparing two runs should not see a reshuffle.
	sort.Strings(survey.Write)
	sort.Strings(survey.Admin)
	return survey, nil
}
