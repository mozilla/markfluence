// Package read implements the `markfluence read` command: fetch a page and
// print its body to stdout.
package read

import (
	"fmt"
	"os"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// Output formats. Markdown (the default) is the best-effort inverse of
// MdToConfluence; storage is the raw stored XHTML.
const (
	formatMarkdown = "markdown"
	formatStorage  = "storage"
)

var formatFlag string

// Cmd is the read command.
var Cmd = &cobra.Command{
	Use:   "read PAGE",
	Short: "Get a Confluence page and print its body",
	Long: "Get a Confluence page and print its body to stdout. You can redirect the\n" +
		"output to a file.\n\n" +
		"PAGE is a page id, a Confluence page URL, or a Markdown file that names a\n" +
		"page_id in its frontmatter or in its pages: entry. A URL can have the usual\n" +
		"/wiki/.../pages/<id>/... form, or the older ?pageId=<id> form.\n\n" +
		"--format markdown is the default. The output has title, space, parent,\n" +
		"page_id, labels, page_status, and page_width in the frontmatter. It is the\n" +
		"inverse of what create and update publish, as far as that is possible.\n\n" +
		"The Confluence API has no Markdown form, so markfluence converts the storage\n" +
		"format itself. The constructs that markfluence writes come back correctly. For\n" +
		"content from the Confluence editor, some details change:\n\n" +
		"  - A macro that markfluence does not map, and a column layout, come back as\n" +
		"    raw storage tags. Their bodies stay as Markdown that you can read, and they\n" +
		"    publish back with no change.\n" +
		"  - Some conversions lose detail. For example, a cell color that is not one of\n" +
		"    the named swatches comes back as a literal hex value.\n\n" +
		"Thus the output helps you read a page, but it is not a guaranteed round trip.\n\n" +
		"--format storage prints the raw storage format XHTML exactly as Confluence\n" +
		"stores it.",
	Example: "  # Print Markdown, with frontmatter, to stdout\n" +
		"  markfluence read 1234567890\n\n" +
		"  # Save it as a file that you can edit and publish back\n" +
		"  markfluence read 1234567890 > page.md\n\n" +
		"  # Print the raw storage format that Confluence holds\n" +
		"  markfluence read 1234567890 --format storage > page.storage.xml\n\n" +
		"  # Give a URL\n" +
		"  markfluence read \"https://org.atlassian.net/wiki/spaces/ENG/pages/1234567890/Title\"",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&formatFlag, "format", formatMarkdown,
		"Output format: markdown (the default) or storage")

	completion.RegisterFlag(Cmd, "format", completion.Values(formatMarkdown, formatStorage))
}

func run(cmd *cobra.Command, args []string) error {
	if formatFlag != formatMarkdown && formatFlag != formatStorage {
		return fatalFail(fmt.Sprintf("unsupported --format %q (supported: %s, %s)",
			formatFlag, formatMarkdown, formatStorage), jsonout.CodeValidation)
	}

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
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

	page, err := c.GetPageBodyOrNil(pageID)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}
	if page == nil {
		return operationalFail(pageID, fmt.Errorf("page %s not found", pageID), jsonout.CodeNotFound)
	}
	if page.Body.Storage.Value == "" {
		return operationalFail(pageID, fmt.Errorf(
			"page %s has no readable body (it may be a folder or an unsupported content type)",
			pageID), jsonout.CodeValidation)
	}

	body := page.Body.Storage.Value
	if formatFlag == formatMarkdown {
		// The empty position: read prints to stdout, where there is no tree and
		// no directory for a path to be relative to. An attachment with no
		// recorded path therefore reads as <slug>/<name>, which is where
		// attachment-download puts it.
		// A fresh cache: read handles one page, so there is nothing for it to
		// share with, and a cache scoped to the command cannot outlive it.
		body, err = convert.StorageToMarkdown(page.Body.Storage.Value,
			pagedoc.Options(c, page, pagedoc.Placement{}, pagedoc.NewUserCache()))
		if err != nil {
			return operationalFail(pageID, err, jsonout.CodeConvert)
		}
	}

	if ui.IsJSON() {
		env := jsonout.NewEnvelope("read", []any{buildResult(c, page, formatFlag, body)},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}

	if formatFlag == formatStorage {
		fmt.Println(body)
		return nil
	}
	fmt.Print(pagedoc.Frontmatter(c, page, "") + "\n" + body)
	return nil
}

// fatalFail reports a config/usage/pre-flight failure: a JSON error object on
// stderr under --json, else a human error line, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "read", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports an operational failure for the single target: under
// --json a results[0] entry {ok:false,error,code}, else a human error line,
// exiting 1.
func operationalFail(pageID string, err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.Emit(os.Stdout, failEnvelope(pageID, err, code))
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// failEnvelope is the document operationalFail writes, split out so the schema
// conformance test can validate the envelope this command really emits instead
// of a hand-copied duplicate of it.
func failEnvelope(pageID string, err error, code jsonout.Code) jsonout.Envelope {
	return jsonout.NewEnvelope("read", []any{jsonout.NewSingleOpFailure(pageID, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
}
