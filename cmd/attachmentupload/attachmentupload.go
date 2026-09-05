// Package attachmentupload implements the `markfluence attachment-upload`
// command: upload or replace a page's attachments.
package attachmentupload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/project"
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
		"Each file is attached under its path relative to the documentation\n" +
		"root (its base name, with no markfluence.yaml above it). A file whose\n" +
		"contents already match the attachment on the page is skipped, using the same\n" +
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
	rootOverride, _ := cmd.Flags().GetString("root")
	roots := project.NewCache(rootOverride)
	defer roots.Close()
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile, Roots: roots,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	attachments, err := localAttachments(files, nameFlag, roots)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}
	for _, dir := range roots.Roots() {
		ui.Info("root: " + dir)
	}

	if dryRun && !ui.IsJSON() {
		ui.Warn("DRY RUN — no changes will be written.")
	}

	actions, err := plan(c, pageID, attachments)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err), roots)
	}
	return report(actions, roots)
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
// The attachment name is the encoding of --name, or -- with no override -- the
// file's source resolved root-relative (internal/project), the same way a
// published image's Source is: a page-specific upload of sub/img.png (no
// project file above it) still records "img.png," but a shared one under a
// declared root records "sub/img.png," matching what publishing a page that
// references the same file would record. The recorded source is always the
// decode of the name, never the local path: if the two disagreed, a later
// publish would upload a second attachment under the name it computes while a
// download restored this one somewhere the markdown never references.
func localAttachments(files []string, name string, roots *project.Cache) ([]client.LocalAttachment, error) {
	out := make([]client.LocalAttachment, 0, len(files))
	for _, f := range files {
		info, err := os.Stat(f)
		if err != nil {
			return nil, err
		}
		if info.IsDir() {
			return nil, fmt.Errorf("%s is a directory", f)
		}
		source := name
		if source == "" {
			source, err = rootRelativeSource(f, roots)
			if err != nil {
				return nil, err
			}
		}
		filename := convert.AttachmentFilename(source)
		if filename == "" {
			return nil, fmt.Errorf("%q is not a usable attachment name", source)
		}
		// source is recorded as given, not as a decode of the name. The name is
		// now the base name, so decoding it back would throw the path away and
		// record "x.png" for an asset at "docs/assets/x.png" -- which is the one
		// copy of the path there is.
		out = append(out, client.LocalAttachment{Path: f, Filename: filename, Source: source})
	}
	return out, nil
}

// rootRelativeSource resolves f's root -- discovered from f's own directory,
// cached across the batch -- and returns f's path relative to it, in slash
// form. With no markfluence.yaml anywhere above f, the root falls back to f's
// own directory, so this reduces to f's bare basename exactly as before.
//
// An explicit --root can name a directory that isn't an ancestor of f at all
// (project.Resolve applies it uniformly, with no containment check of its
// own), so rel can climb above root; refuse that the same way
// internal/convert/images.go's rootRelative and create's resolveParent do,
// rather than encoding a "../"-prefixed source into the attachment name.
func rootRelativeSource(f string, roots *project.Cache) (string, error) {
	abs, err := filepath.Abs(f)
	if err != nil {
		return "", err
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return "", fmt.Errorf("resolving the documentation root: %w", err)
	}
	rel, err := filepath.Rel(root.Dir, abs)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", fmt.Errorf("%s resolves outside the documentation root (%s)", f, root.Dir)
	}
	return rel, nil
}

// report prints the per-file actions and returns the command's exit status.
func report(actions []client.SyncAction, roots *project.Cache) error {
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
		env.Roots = roots.Roots()
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
func operationalFail(pageID string, err error, code jsonout.Code, roots *project.Cache) error {
	if ui.IsJSON() {
		_ = jsonout.Emit(os.Stdout, failEnvelope(pageID, err, code, roots))
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// failEnvelope is the document operationalFail writes, split out so the schema
// conformance test can validate the envelope this command really emits instead
// of a hand-copied duplicate of it.
func failEnvelope(pageID string, err error, code jsonout.Code, roots *project.Cache) jsonout.Envelope {
	env := jsonout.NewEnvelope(command, []any{jsonout.NewSingleOpFailure(pageID, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1, "skipped": 0})
	env.Roots = roots.Roots()
	return env
}
