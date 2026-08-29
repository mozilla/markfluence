// Package attachmentdownload implements the `markfluence attachment-download`
// command: write a page's attachments to the filesystem.
package attachmentdownload

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "attachment-download"

var (
	dest  string
	flat  bool
	force bool

	dryRun bool
)

// Cmd is the attachment-download command.
var Cmd = &cobra.Command{
	Use:   command + " PAGE [NAME...]",
	Short: "Download a Confluence page's attachments",
	Long: "Download a Confluence page's attachments.\n\n" +
		"PAGE is a numeric page id, a Confluence page URL, or a markdown file\n" +
		"whose frontmatter has a page_id. Each NAME is an attachment name as\n" +
		"attachment-list reports it; with no NAME, every attachment is\n" +
		"downloaded.\n\n" +
		"An attachment markfluence published records the markdown image path it\n" +
		"came from, and is written back to that path under --dest, so the\n" +
		"downloaded tree matches what the page's markdown references and\n" +
		"previews locally. An attachment without a recorded path is written\n" +
		"under its stored name. --flat writes everything under stored names.\n\n" +
		"A recorded path that would resolve outside --dest is refused for that\n" +
		"attachment, since the path comes from an attachment comment anyone who\n" +
		"can edit the page controls.\n\n" +
		"A file that already exists is skipped unless --force.",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.PageThenNames,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&dest, "dest", ".", "Directory to write attachments into.")
	Cmd.Flags().BoolVar(&flat, "flat", false,
		"Write every attachment under its stored name, ignoring recorded paths.")
	Cmd.Flags().BoolVar(&force, "force", false, "Overwrite files that already exist.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview what would be written without creating any files.")

	completion.RegisterFlag(Cmd, "dest", completion.Directories)
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.Options{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	attachments, err := c.ListAttachments(pageID)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}

	wanted, missing := selectAttachments(attachments, args[1:])
	if dryRun && !ui.IsJSON() {
		ui.Warn("DRY RUN — no files will be written.")
	}

	root, err := filepath.Abs(dest)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}
	opts := attachfile.Options{Root: root, Flat: flat, Force: force, DryRun: dryRun}

	results := make([]attachfile.Outcome, 0, len(wanted)+len(missing))
	for _, a := range wanted {
		results = append(results, attachfile.Write(c, a, opts))
	}
	// A name the page doesn't have is that name's failure, not the run's.
	for _, name := range missing {
		results = append(results, attachfile.Outcome{
			Name:   name,
			Status: attachfile.StatusFailed,
			Err:    fmt.Errorf("no attachment named %q on page %s", name, pageID),
			Code:   jsonout.CodeNotFound,
		})
	}
	return report(results)
}

// selectAttachments picks the attachments named on the command line, preserving
// the order the names were given, and reports names the page doesn't have. With
// no names, every attachment is selected in server order.
func selectAttachments(attachments []client.Attachment, names []string) (
	wanted []client.Attachment, missing []string,
) {
	if len(names) == 0 {
		return attachments, nil
	}
	byName := make(map[string]client.Attachment, len(attachments))
	for _, a := range attachments {
		byName[a.Title] = a
	}
	for _, n := range names {
		if a, ok := byName[n]; ok {
			wanted = append(wanted, a)
			continue
		}
		missing = append(missing, n)
	}
	return wanted, missing
}

// report prints the per-attachment outcomes and returns the command's exit
// status: 1 if any attachment failed.
func report(results []attachfile.Outcome) error {
	failed := 0
	skipped := 0
	for _, r := range results {
		switch r.Status {
		case attachfile.StatusFailed:
			failed++
		case attachfile.StatusSkipped:
			skipped++
		}
	}

	if ui.IsJSON() {
		out := make([]any, 0, len(results))
		for _, r := range results {
			out = append(out, buildResult(r))
		}
		env := jsonout.NewEnvelope(command, out, map[string]int{
			"total": len(results), "succeeded": len(results) - failed,
			"failed": failed, "skipped": skipped,
		})
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if failed > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	for _, r := range results {
		switch r.Status {
		case attachfile.StatusSkipped:
			ui.Dim(fmt.Sprintf("%-10s %s  (exists; --force to overwrite)", r.Status, r.DestPath))
		case attachfile.StatusFailed:
			ui.Error(fmt.Sprintf("%-10s %s: %s", r.Status, r.Name, r.Err))
		default:
			ui.Success(fmt.Sprintf("%-10s %s", r.Status, r.DestPath))
		}
	}
	if failed > 0 {
		return ui.SilentExit(1)
	}
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

// operationalFail reports a failure against the page: under --json a results[0]
// entry {ok:false,error,code}, else a human error line, exiting 1.
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
	return jsonout.NewEnvelope(command, []any{jsonout.NewSingleOpFailure(pageID, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1, "skipped": 0})
}
