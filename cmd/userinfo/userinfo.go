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

// Cmd is the user-info command.
var Cmd = &cobra.Command{
	Use:   "user-info [ACCOUNT_ID]",
	Short: "Show who the credentials belong to, or who an account id names",
	Long: "With no argument, show the account that the configured credentials belong to.\n" +
		"Ask this first when:\n\n" +
		"  - a publish goes to an unexpected place\n" +
		"  - Confluence refuses a token\n" +
		"  - the history of a page names an account that you do not know\n" +
		" No other\n" +
		"markfluence command can tell you who you are.\n\n" +
		"With an ACCOUNT_ID, show that account. Give an id, and not a name. To find an\n" +
		"account by name, use markfluence user-find. The two commands see different\n" +
		"things. user-find searches a directory that cannot see deactivated accounts.\n" +
		"user-info can resolve a deactivated account. Thus an id from an old page names\n" +
		"its person here, and nowhere else.\n\n" +
		"\"type\" tells a person (atlassian) from a service account (app). That\n" +
		"difference explains most surprises about permissions. \"external\" and \"guest\"\n" +
		"show a restricted account.\n\n" +
		"With no argument, user-info also surveys every space that the credentials can\n" +
		"see. It shows where they can create pages, and which spaces they administer.\n" +
		"The survey walks the directory of spaces, so this form takes a few seconds.\n\n" +
		"The ACCOUNT_ID form has no survey, on purpose. Confluence answers the survey\n" +
		"only for the authenticated account, and takes no account id. If user-info\n" +
		"showed it next to the name of a different person, it would give them your\n" +
		"access. To find where a different person can publish, markfluence would have\n" +
		"to read the grants of every space and resolve their groups. That is more than a\n" +
		"thousand requests.\n\n" +
		"\"write access\" means that the account can create pages in a space. Permission\n" +
		"to edit a page that exists is not a space grant at all. Thus a space in this\n" +
		"list can still refuse one page.\n\n" +
		"user-info only reads. It writes nothing to Confluence or to disk.",
	Example: "  # Who am I, and where can these credentials publish?\n" +
		"  markfluence user-info\n\n" +
		"  # Who is this account id on an old page?\n" +
		"  markfluence user-info 60c36d0718e9f60071326951\n\n" +
		"  # Print only the spaces, as data\n" +
		"  markfluence user-info --json | jq '.results[0].spaces'",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.Values(),
	RunE:              run,
}

func run(cmd *cobra.Command, args []string) error {
	accountID := ""
	if len(args) == 1 {
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
	// Surveyed only for the self form, and that is the whole rule rather than a
	// default: GET /space?expand=operations answers for the **authenticated**
	// account and takes no account id, so printing it under somebody else's
	// name would attribute this caller's access to them -- a wrong answer, not
	// a missing one.
	//
	// Answering it properly for another account is possible and deliberately
	// not done. Both pieces exist (/user/memberof for an account's groups,
	// /api/v2/spaces/{id}/permissions for a space's grants) but measured here
	// that is 42 groups against 533 spaces whose grant lists run 250+ rows
	// each and paginate -- over a thousand requests where this costs three --
	// plus a local reimplementation of Confluence's permission resolution,
	// which reports somebody else's access wrongly with full confidence when
	// it is subtly off.
	if rep.self {
		// Best-effort, like the optional reads in page-info and space-info --
		// but *said out loud*, unlike theirs. Those are implicit; this one is
		// part of what the bare command promises, so a silent omission leaves
		// no sign anything was missed.
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
	// spaces is nil for the account-id form -- the survey can only describe
	// the caller -- and also when it failed.
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
	// Deduplicated by space id, which is **defensive rather than a fix**: the
	// walk was measured returning 533 rows and 533 distinct keys, so this
	// route's offset does count rows returned and the advance is right. It is
	// guarded anyway because this is the route that already surprised us once
	// (a short page is not the end), and a duplicate here would inflate a
	// count nobody can sanity-check and print the same key twice.
	seen := map[string]bool{}
	err := c.WalkSpaceOperations(func(space client.SpaceRef, ops []client.SpaceOperation) error {
		if seen[space.ID] {
			return nil
		}
		seen[space.ID] = true
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
