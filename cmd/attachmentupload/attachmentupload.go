// Package attachmentupload implements the `markfluence attachment-upload`
// command: upload or replace a page's attachments.
package attachmentupload

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "attachment-upload"

var (
	nameFlag string
	force    bool
	dryRun   bool
)

// Cmd is the attachment-upload command.
var Cmd = &cobra.Command{
	Use:   command + " PAGE FILE...",
	Short: "Upload or replace attachments on a Confluence page",
	Long: "Upload or replace attachments on a Confluence page.\n\n" +
		"PAGE is a numeric page id, a Confluence page URL, or a markdown file\n" +
		"whose frontmatter has a page_id.\n\n" +
		"Each file is attached under its base name. A file whose contents\n" +
		"already match the attachment on the page is skipped, using the same\n" +
		"checksum bookkeeping create/update use, so uploading by hand and\n" +
		"publishing agree on what is current; --force uploads anyway.\n\n" +
		"--name sets the attachment name for a single file, and takes a path:\n" +
		"markfluence encodes it the way publishing would, so `--name\n" +
		"assets/x.png` produces the attachment an image written as\n" +
		"![](assets/x.png) resolves to.",
	Args:              cobra.MinimumNArgs(2),
	ValidArgsFunction: completion.PageThenFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&nameFlag, "name", "",
		"Attachment name, given as a path (requires a single FILE).")
	Cmd.Flags().BoolVar(&force, "force", false,
		"Upload even when the checksum shows the attachment is unchanged.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview what would be uploaded without writing to Confluence.")
}

func run(cmd *cobra.Command, args []string) error {
	files := args[1:]
	if nameFlag != "" && len(files) != 1 {
		return fatalFail("--name applies to a single attachment; pass exactly one FILE",
			jsonout.CodeValidation)
	}

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

	attachments, err := localAttachments(files, nameFlag)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}

	if dryRun && !ui.IsJSON() {
		ui.Warn("DRY RUN — no changes will be written.")
	}

	actions, err := plan(c, pageID, attachments)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}
	return report(actions)
}

// plan performs the upload, or -- under --dry-run -- only the classification.
// --force turns every non-skip into an upload by classifying nothing as
// unchanged, which is why it is applied here rather than inside the client.
func plan(c *client.ConfluenceClient, pageID string, attachments []client.LocalAttachment) (
	[]client.SyncAction, error,
) {
	if dryRun {
		actions, err := c.PlanAttachments(pageID, attachments)
		if err != nil {
			return nil, err
		}
		if force {
			actions = forced(actions)
		}
		return actions, nil
	}
	if force {
		return c.ForceUploadAttachments(pageID, attachments)
	}
	return c.SyncAttachments(pageID, attachments)
}

// forced rewrites a dry-run plan for --force: what would have been skipped is
// instead updated, matching what a forced run actually does.
func forced(actions []client.SyncAction) []client.SyncAction {
	out := make([]client.SyncAction, len(actions))
	for i, a := range actions {
		if a.Action == "skipped" {
			a.Action = "updated"
		}
		out[i] = a
	}
	return out
}

// localAttachments resolves each file into an upload, checking readability up
// front so a batch fails before it has half-uploaded.
//
// The attachment name is the file's base name, or the encoding of --name. The
// recorded source is always the decode of the name, never the local path: if
// the two disagreed, a later publish would upload a second attachment under the
// name it computes while a download restored this one somewhere the markdown
// never references.
func localAttachments(files []string, name string) ([]client.LocalAttachment, error) {
	out := make([]client.LocalAttachment, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a directory", f)
		}
		source := filepath.Base(f)
		if name != "" {
			source = name
		}
		filename := convert.AttachmentFilename(source)
		if filename == "" {
			return nil, fmt.Errorf("%q is not a usable attachment name", source)
		}
		// Round-trip the name so source is exactly what a decode yields.
		if decoded, ok := convert.AttachmentSource(filename); ok {
			source = decoded
		}
		out = append(out, client.LocalAttachment{Path: f, Filename: filename, Source: source})
	}
	return out, nil
}

// report prints the per-file actions and returns the command's exit status.
func report(actions []client.SyncAction) error {
	if ui.IsJSON() {
		results := make([]any, 0, len(actions))
		skipped := 0
		for _, a := range actions {
			if a.Action == "skipped" {
				skipped++
			}
			results = append(results, buildResult(a))
		}
		env := jsonout.NewEnvelope(command, results, map[string]int{
			"total": len(actions), "succeeded": len(actions), "failed": 0, "skipped": skipped,
		})
		return jsonout.Emit(os.Stdout, env)
	}
	for _, a := range actions {
		line := fmt.Sprintf("%-8s %s", a.Action, a.Filename)
		if a.Action == "skipped" {
			ui.Dim(line + "  (unchanged)")
			continue
		}
		ui.Success(line)
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
		res := map[string]any{"ok": false, "page_id": pageID, "error": err.Error(), "code": code}
		env := jsonout.NewEnvelope(command, []any{res},
			map[string]int{"total": 1, "succeeded": 0, "failed": 1, "skipped": 0})
		_ = jsonout.Emit(os.Stdout, env)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// decodeName is convert.AttachmentSource, wrapped so tests can assert the
// lockstep invariant without importing the converter.
func decodeName(filename string) (string, bool) { return convert.AttachmentSource(filename) }
