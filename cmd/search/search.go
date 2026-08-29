// Package search implements the `markfluence search` command: find pages by
// full-text query.
package search

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "search"

// limitAll is the --limit value meaning "every match".
const limitAll = "all"

// defaultLimit is the --limit default.
//
// A bound rather than "all", which is the one place this command deliberately
// departs from #44. The issue's argument for unlimited -- that returning 10 of 40
// matches silently is a trap -- holds for `find`, where the match set is small
// and every row is load-bearing. Full text is different: one word matches
// thousands of pages, so a relevance-ranked top-N *is* the answer rather than a
// truncation of it, and defaulting to unlimited would mean a dozen sequential
// requests for someone checking whether a page exists. Nothing is truncated
// quietly -- see the "more exist" line in report.
//
// Ten rather than a larger bound because a hit is a *block*, not a row: title,
// identity, URL and excerpt run to 5-6 lines each. Twenty-five of those is around
// 140 lines, which is several screens and past the point where relevance ordering
// is doing anyone any good.
const defaultLimit = "10"

var (
	spaceOpt string
	typeOpt  string
	limitOpt string
	cqlOpt   bool
)

// Cmd is the search command.
var Cmd = &cobra.Command{
	Use:   command + " QUERY",
	Short: "Find Confluence pages by full-text search",
	Long: "Find Confluence pages whose text matches QUERY.\n\n" +
		"QUERY is matched against the page's full text, not just its title.\n" +
		"Multiple words are ANDed: every word must appear somewhere in the\n" +
		"page, in any order. It is not a phrase search, so quoting a phrase\n" +
		"does not require the words to be adjacent.\n\n" +
		"Results come back in Confluence's own relevance order, best first,\n" +
		"and are capped at --limit. When more matches exist than were shown,\n" +
		"the command says so rather than truncating silently.\n\n" +
		"Archived pages are never returned: the search index cannot see them.\n" +
		"Neither are folders, which have no text to match -- use `find` for\n" +
		"both of those.\n\n" +
		"Finding nothing is a success: the command says so and exits 0.",
	Args: cobra.ExactArgs(1),
	// A query is free text and a space key lives on the server, which completion
	// may not go ask for.
	ValidArgsFunction: cobra.NoFileCompletions,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&spaceOpt, "space", "",
		"Restrict the search to a space, by key.")
	Cmd.Flags().StringVar(&typeOpt, "type", client.SearchTypePage,
		fmt.Sprintf("Content type to search: %q, %q, or %q.",
			client.SearchTypePage, client.SearchTypeBlogpost, client.SearchTypeAll))
	Cmd.Flags().StringVar(&limitOpt, "limit", defaultLimit,
		fmt.Sprintf("How many matches to show: a positive number, or %q.", limitAll))
	Cmd.Flags().BoolVar(&cqlOpt, "cql", false,
		"Treat QUERY as a raw CQL query instead of text to search for; "+
			"cannot be combined with --space or an explicit --type (put those clauses in the query).")

	completion.RegisterFlag(Cmd, "space", cobra.NoFileCompletions)
	completion.RegisterFlag(Cmd, "type", completion.Values(
		client.SearchTypePage, client.SearchTypeBlogpost, client.SearchTypeAll))
	completion.RegisterFlag(Cmd, "limit", completion.Values("5", "10", "25", limitAll))
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")

	// Everything in this block is a usage error and needs no server to recognize.
	// The empty-query check especially: the API answers an empty match with a 500
	// carrying a Java exception message, which would surface as an operational
	// failure with the wrong exit code (docs/confluence/search.md).
	query := args[0]
	if strings.TrimSpace(query) == "" {
		return fatalFail("no query given: QUERY must not be empty", jsonout.CodeValidation)
	}
	if err := checkCQLFlags(cmd); err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}
	if err := checkType(typeOpt); err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
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

	var res client.SearchResults
	if cqlOpt {
		res, err = c.SearchRawCQL(query, limit)
	} else {
		res, err = c.SearchText(query, spaceOpt, typeOpt, limit)
	}
	// The query as sent, on both paths, and before the error check so a failing
	// search still shows what it asked. It stays out of --json: the envelope
	// reports results, and --debug is where "what did you actually ask?" belongs.
	// Empty when the failure came before a query was built, e.g. an unknown space.
	if res.CQL != "" {
		ui.Debug("cql: " + res.CQL)
	}
	if err != nil {
		// An unknown space key is the user's typo, not a failure of the search.
		if errors.Is(err, client.ErrSpaceNotFound) {
			return fatalFail(fmt.Sprintf("space %q not found", spaceOpt), jsonout.CodeValidation)
		}
		return operationalFail(err, jsonout.CodeFor(err))
	}

	return report(res, limit)
}

// report writes the results, in --json or for a human.
func report(res client.SearchResults, limit int) error {
	if ui.IsJSON() {
		results := make([]any, 0, len(res.Matches))
		for _, m := range res.Matches {
			results = append(results, buildResult(m))
		}
		return jsonout.Emit(os.Stdout, jsonout.NewEnvelope(command, results, buildSummary(res)))
	}

	if len(res.Matches) == 0 {
		ui.Info("No matches found.")
		warnSkipped(res.Skipped)
		return nil
	}
	fmt.Println(blocks(res.Matches))
	if res.More {
		// Blank line first, or the notice reads as part of the last hit's block.
		fmt.Println()
		ui.Info(fmt.Sprintf("Showing %d matches; more exist (use --limit %s).",
			len(res.Matches), limitAll))
	}
	warnSkipped(res.Skipped)
	return nil
}

// warnSkipped reports index rows that could not become results.
//
// Only reachable through --cql or --type all: a `type = space` query answers with
// hundreds of rows carrying no content object. It is a warning rather than
// silence because otherwise such a query reports "No matches found." and exits 0,
// which is indistinguishable from a genuine miss -- and a caller's next move on a
// miss is to create what it believes is absent.
func warnSkipped(n int) {
	if n == 0 {
		return
	}
	ui.Warn(fmt.Sprintf(
		"Skipped %d result(s) that are not addressable content (a space, for example).", n))
}

// checkCQLFlags refuses the flag combinations raw CQL cannot honor.
//
// --space and --type would have to be ANDed onto the caller's query, and doing
// that to a query containing `or` regroups it and silently answers a different
// question. Refusing is better than rewriting: "raw CQL" that is not raw is worse
// than an error. --limit is fine, since it bounds the pager rather than the query.
//
// --type is checked with Changed because it has a non-empty default, so its
// value alone cannot say whether the caller asked for it.
func checkCQLFlags(cmd *cobra.Command) error {
	if !cqlOpt {
		return nil
	}
	if spaceOpt != "" {
		return errors.New("--cql cannot be combined with --space: " +
			"put the space clause in the query, e.g. 'space = \"ENG\" and text ~ \"deploy\"'")
	}
	if cmd.Flags().Changed("type") {
		return errors.New("--cql cannot be combined with --type: " +
			"put the type clause in the query, e.g. 'type = page and text ~ \"deploy\"'")
	}
	return nil
}

// checkType validates --type.
//
// The vocabulary is markfluence's, not Confluence's: the index also holds
// attachments, comments, databases and whiteboards, all of which match a
// full-text query and none of which is an id any command here accepts.
//
// "folder" is named in the error rather than accepted, because accepting it would
// always return nothing -- full text cannot see a folder, which has no text.
// Silently answering "no matches" to a question that cannot have one is the
// failure mode `children --depth 0` refuses for the same reason.
func checkType(v string) error {
	switch v {
	case client.SearchTypePage, client.SearchTypeBlogpost, client.SearchTypeAll:
		return nil
	case "folder":
		return errors.New(`invalid --type "folder": full text cannot match a folder, ` +
			"which has no text -- use `markfluence find` to look a folder up by title")
	default:
		return fmt.Errorf("invalid --type %q: want %q, %q, or %q",
			v, client.SearchTypePage, client.SearchTypeBlogpost, client.SearchTypeAll)
	}
}

// parseLimit turns the --limit value into a row bound, 0 meaning unbounded.
//
// 0 and negatives are refused rather than reinterpreted, matching `children
// --depth`: elsewhere 0 often means "unlimited", and silently walking every match
// for someone who meant "none" is worse than an error naming the value that does
// mean unlimited.
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

// operationalFail reports the search itself failing, exiting 1.
//
// Like find, and unlike the per-page commands, this writes an error object rather
// than a results[0] failure: those name the page they were asked about, and there
// is no such id here. Emitting an empty results array instead would be worse than
// emitting nothing, since an empty result is what a caller acts on.
func operationalFail(err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, err.Error(), code)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// blocks renders the matches as one indented block per hit.
//
// Not a table, unlike find: the excerpt is the point of a full-text hit -- it is
// what answers "why did this match?" -- and at 150-450 characters no aligned
// column survives it. The excerpt arrives already collapsed to one line and is
// deliberately not wrapped: nothing here detects terminal width, and the terminal
// soft-wraps.
//
// Order is the server's relevance order, best first, and must stay that way.
func blocks(matches []client.SearchMatch) string {
	var b strings.Builder
	for i, m := range matches {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(m.Title + "\n")
		b.WriteString("  " + m.Type + " " + m.ID + "  " + orDash(m.Space) + "\n")
		if m.URL != "" {
			b.WriteString("  " + m.URL + "\n")
		}
		if m.Excerpt != "" {
			b.WriteString("  " + excerptLine(m) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// excerptLine renders a hit's excerpt with its matched terms highlighted.
//
// A match carrying no spans falls back to the plain excerpt, which covers an
// excerpt the server returned without markers (10 of 50 rows sampled) and any
// SearchMatch built without them.
func excerptLine(m client.SearchMatch) string {
	if len(m.Spans) == 0 {
		return m.Excerpt
	}
	return renderSpans(m.Spans, ui.Match)
}

// renderSpans concatenates excerpt spans, passing the matched runs through hl.
//
// hl is a parameter rather than a direct ui.Match call so this stays testable.
// Tests run with stdout not a terminal, where lipgloss emits no escape codes at
// all -- so a test that asserted highlighted output against a hard-wired
// ui.Match would pass just as happily against text that was never highlighted.
// A visible stand-in makes the assertion mean something.
func renderSpans(spans []client.ExcerptSpan, hl func(string) string) string {
	var b strings.Builder
	for _, s := range spans {
		if s.Match {
			b.WriteString(hl(s.Text))
			continue
		}
		b.WriteString(s.Text)
	}
	return b.String()
}

// orDash renders an underivable space key as "-" rather than a blank, so the
// identity line still reads as one.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
