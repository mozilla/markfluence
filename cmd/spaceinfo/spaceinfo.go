// Package spaceinfo implements the `markfluence space-info` command: print what
// a space is, what this account may do in it, and how big it is.
package spaceinfo

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagestatus"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var sinceDays int

// Cmd is the space-info command.
var Cmd = &cobra.Command{
	Use:   "space-info KEY",
	Short: "Print metadata about a Confluence space",
	Long: "Print what a Confluence space is, what the account you are running as\n" +
		"may do in it, which page statuses it offers, and how big it is.\n\n" +
		"KEY is a space key -- ENG, or a personal space like ~1234abcd -- never a\n" +
		"page or a markdown file. An unknown key is an error rather than an empty\n" +
		"result, since a typo and a space you cannot see should not look alike.\n\n" +
		"'your access' reports what the space grants: whether you can read it and\n" +
		"whether you can create pages in it. It deliberately does not say\n" +
		"\"write\", because permission to edit an existing page is not a space\n" +
		"grant at all -- Confluence decides that per page -- so an account that\n" +
		"can create pages here may still be refused on a particular one.\n" +
		"markfluence page-info PAGE is where you ask about a page.\n\n" +
		"'page statuses' is what a page_status: line in a markdown file may say,\n" +
		"and it comes from one of two places, which the label tells you apart. A\n" +
		"space admin gets the space's own configured list. Everyone else gets what\n" +
		"THEY may set on the space homepage, which is not the same thing:\n" +
		"Confluence decides the list per page and per account, so another page may\n" +
		"allow more or fewer. When neither can be read the field says so.\n\n" +
		"--since counts from midnight UTC that many days ago, so 0 is today only\n" +
		"and 7 is the last week plus today.\n\n" +
		"The page counts are exact, which is why they cost a walk of the space --\n" +
		"one request per 250 pages. They count pages, not edits: a page revised\n" +
		"nine times in the window is one touched page. A page created inside the\n" +
		"window is counted as created AND touched, and the overlap is reported so\n" +
		"the two cannot be added up wrongly.\n\n" +
		"Read-only. Nothing is written to Confluence or to disk.",
	Example: "  # What is this space, and can I publish to it?\n" +
		"  markfluence space-info ENG\n\n" +
		"  # Activity over a month rather than a week\n" +
		"  markfluence space-info ENG --since 30\n\n" +
		"  # Just the statuses a page_status: line may use\n" +
		"  markfluence space-info ENG --json | jq '.results[0].page_statuses'",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.Values(),
	RunE:              run,
}

func init() {
	Cmd.Flags().IntVar(&sinceDays, "since", 7,
		"Days back from midnight UTC for the created/touched page counts (0 is today only).")
}

func run(cmd *cobra.Command, args []string) error {
	if sinceDays < 0 {
		return fatalFail("--since cannot be negative; it is a number of days, and 0 means today only",
			jsonout.CodeValidation)
	}
	spaceKey := strings.TrimSpace(args[0])
	if spaceKey == "" {
		// Before the credentials, like --since above: a local defect should not
		// cost a request or a token, and reporting a missing CONFLUENCE_TOKEN
		// for a blank argument names the wrong problem.
		return fatalFail("no space key given", jsonout.CodeValidation)
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

	space, err := c.GetSpace(spaceKey)
	if err != nil {
		return operationalFail(err.Error(), jsonout.CodeFor(err))
	}
	if space == nil {
		return operationalFail(fmt.Sprintf("space %q not found", spaceKey), jsonout.CodeNotFound)
	}

	rep := buildReport(c, space, sinceDays)
	if ui.IsJSON() {
		env := jsonout.NewEnvelope("space-info", []any{rep.jsonResult()},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}
	fmt.Println(rep.human())
	return nil
}

// fatalFail reports a usage or credential failure and exits **2**;
// operationalFail reports a failure the server gave and exits **1**.
//
// The split is docs/json-output.md's contract, and page-info -- the closest
// sibling -- draws it the same way: 2 means "you invoked it wrong", 1 means
// "the request was fine and the answer was no". A CI job branching on the two
// gets a useful signal only if an unknown space key is not the same code as a
// bad flag. (diff is the one command that departs from this, deliberately, for
// diff(1)'s codes.)
//
// Both write to stderr rather than to a results[0] entry, unlike page-info:
// there is no page id to name, which is find/search/children --space's rule.
// Everything that can fail *partially* here degrades to a null field instead
// (see report).
func fatalFail(msg string, code jsonout.Code) error {
	return emitFailure(msg, code, 2)
}

func operationalFail(msg string, code jsonout.Code) error {
	return emitFailure(msg, code, 1)
}

func emitFailure(msg string, code jsonout.Code, exit int) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "space-info", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(exit)
}

// statusSource says where the page-status list came from, because the two
// sources do not mean the same thing and a reader has to be able to tell.
type statusSource string

const (
	// sourceSpace: the space's own configuration, from state/settings. Only a
	// space admin can see this.
	sourceSpace statusSource = "space"
	// sourcePage: what this caller may set on the probe page. Not the space's
	// list -- Confluence decides it per (caller, page) -- and labelled as such
	// wherever it is rendered.
	sourcePage statusSource = "page"
	// sourceNone: neither could be read.
	sourceNone statusSource = ""
)

// report is everything the command found, feeding both renderers. A field that
// could not be read is zero/nil rather than an error: the space lookup is the
// only fatal one, so a token that can read a space but not walk it still gets a
// useful answer.
type report struct {
	space *client.SpaceSummary

	// statusSource, statuses and statusProbe describe the page-status field.
	statusSource statusSource
	statuses     []string
	statusProbe  string

	// counts are nil when the walk failed. Not partial: a wrong count is worse
	// than no count in a command whose output is mostly counts.
	counts *pageCounts

	sinceDays int
}

// pageCounts is one pass of the space walk.
//
// Touched counts *pages*, not edits -- version.createdAt is only the latest
// version's timestamp, so a page revised nine times in the window contributes
// one. CreatedAndTouched is the overlap, reported because a page created inside
// the window necessarily has its v1 inside it too, so Created and Touched are
// not disjoint and nobody should add them.
type pageCounts struct {
	Current, Archived, Roots         int
	Created, Touched, CreatedTouched int
	LastActivity                     string
}

// buildReport gathers the space's details. Each optional read is best-effort in
// the same shape page-info's width/labels/status rows already are: a failure
// leaves its field empty and the rest of the report stands.
func buildReport(c *client.ConfluenceClient, space *client.SpaceSummary, days int) report {
	r := report{space: space, sinceDays: days}

	r.resolveStatuses(c, space)
	if counts, err := walkCounts(c, space.ID, days); err == nil {
		r.counts = counts
	}
	return r
}

// resolveStatuses fills the page-status field from the better of two sources.
//
// The space's own configuration first, which only an admin can read; then what
// this caller may set on the homepage, which is a different question with a
// similar-looking answer and is labelled accordingly. Both failing is a normal
// outcome for a collaborator who can create pages but cannot edit the homepage
// (measured, #168), not an error.
func (r *report) resolveStatuses(c *client.ConfluenceClient, space *client.SpaceSummary) {
	if states, err := c.SpaceStateSettings(space.Key); err == nil && states != nil {
		r.statusSource, r.statuses = sourceSpace, pagestatus.Names(states)
		return
	}
	if space.HomepageID == "" {
		return
	}
	if states, err := pagestatus.Available(c, space.HomepageID); err == nil {
		r.statusSource = sourcePage
		r.statuses = pagestatus.Names(states)
		r.statusProbe = space.HomepageID
	}
}

// windowStart is the instant the --since window opens: **midnight UTC, days
// days ago**, not "now minus days".
//
// The difference is the whole meaning of `--since 0`. Subtracting zero days
// from now puts the cutoff at this instant, so nothing can be after it and the
// counts are always zero -- which is what this did, while the help promised
// "today only". Counting from midnight makes 0 mean today, 1 mean since
// yesterday morning, and 7 mean the last week, which is what a person asking
// for "the last N days" means.
func windowStart(now time.Time, days int) time.Time {
	midnight := now.UTC().Truncate(24 * time.Hour)
	return midnight.AddDate(0, 0, -days)
}

// walkCounts walks every page in the space once and counts. Exact by
// construction: nothing here reads totalSize, which docs/confluence/search.md
// measures drifting and forbids reporting as a count.
func walkCounts(c *client.ConfluenceClient, spaceID string, days int) (*pageCounts, error) {
	cutoff := windowStart(time.Now(), days)
	var counts pageCounts
	var latest time.Time
	err := c.WalkSpacePages(spaceID, func(p client.Page) error {
		// The route's default includes archived pages, so the split comes from
		// the row rather than from the request.
		if p.Status == client.StatusArchived {
			counts.Archived++
			return nil
		}
		counts.Current++
		if p.ParentID == "" {
			counts.Roots++
		}

		// Parsed, never compared as strings. Confluence sends milliseconds
		// (2026-09-15T10:04:00.000Z) and a formatted cutoff has none, so a
		// lexicographic test puts a row inside the same second *below* the
		// cutoff -- '.' sorts under 'Z' -- and quietly misses it.
		created := inWindow(p.CreatedAt, cutoff)
		// The *latest* version's timestamp, which is why this counts pages
		// rather than edits.
		touched := inWindow(p.Version.CreatedAt, cutoff)
		if created {
			counts.Created++
		}
		if touched {
			counts.Touched++
		}
		if created && touched {
			counts.CreatedTouched++
		}
		if when, err := time.Parse(time.RFC3339, p.Version.CreatedAt); err == nil && when.After(latest) {
			latest = when
			counts.LastActivity = p.Version.CreatedAt
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &counts, nil
}

// inWindow reports whether an API timestamp falls at or after cutoff. A stamp
// that will not parse is treated as outside: it cannot be placed, and counting
// it would put a number in the output that no row supports.
func inWindow(stamp string, cutoff time.Time) bool {
	when, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return false
	}
	return !when.Before(cutoff)
}
