// Package attachmentdownload implements the `markfluence attachment-download`
// command: write a page's attachments to the filesystem.
package attachmentdownload

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "attachment-download"

// Per-file outcomes, mirroring attachment-upload's verbs.
const (
	statusDownloaded = "downloaded"
	statusSkipped    = "skipped"
	statusFailed     = "failed"
)

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
		"A file that already exists is skipped unless --force.",
	Args: cobra.MinimumNArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&dest, "dest", ".", "Directory to write attachments into.")
	Cmd.Flags().BoolVar(&flat, "flat", false,
		"Write every attachment under its stored name, ignoring recorded paths.")
	Cmd.Flags().BoolVar(&force, "force", false, "Overwrite files that already exist.")
	Cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Preview what would be written without creating any files.")
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

	results := make([]outcome, 0, len(wanted)+len(missing))
	for _, a := range wanted {
		results = append(results, download(c, a, root))
	}
	// A name the page doesn't have is that name's failure, not the run's.
	for _, name := range missing {
		results = append(results, outcome{
			name:   name,
			status: statusFailed,
			err:    fmt.Errorf("no attachment named %q on page %s", name, pageID),
			code:   jsonout.CodeNotFound,
		})
	}
	return report(results)
}

// outcome is what happened to one attachment.
type outcome struct {
	name     string // the stored attachment name
	destPath string // the local path written (or that would be)
	status   string
	err      error
	code     jsonout.Code
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

// download resolves one attachment's destination and writes it.
func download(c *client.ConfluenceClient, a client.Attachment, root string) outcome {
	res := outcome{name: a.Title}

	path, err := destPath(root, a, flat)
	if err != nil {
		res.status, res.err, res.code = statusFailed, err, jsonout.CodeValidation
		return res
	}
	res.destPath = path

	if _, err := os.Stat(path); err == nil && !force {
		res.status = statusSkipped
		return res
	}
	if dryRun {
		res.status = statusDownloaded
		return res
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		res.status, res.err, res.code = statusFailed, err, jsonout.CodeIO
		return res
	}
	f, err := os.Create(path)
	if err != nil {
		res.status, res.err, res.code = statusFailed, err, jsonout.CodeIO
		return res
	}
	defer func() { _ = f.Close() }()
	if err := c.DownloadAttachment(a, f); err != nil {
		res.status, res.err, res.code = statusFailed, err, jsonout.CodeFor(err)
		return res
	}
	res.status = statusDownloaded
	return res
}

// destPath resolves where an attachment is written, and is the only place a
// server-controlled string becomes a filesystem path.
//
// The recorded source path is used, not a decode of the attachment name: there
// is no way to tell a hand-uploaded "a%2Fb.png" from one markfluence published,
// so decoding by default would scatter a literally-named file into a/b.png. An
// attachment with no recorded source keeps its stored name.
//
// The result must stay inside root. A source path may legitimately contain ".."
// -- an image in a directory above its page is a supported layout -- so ".."
// cannot simply be refused; the resolved path is compared against root instead.
// Escaping is an error rather than a silent clip, because the path comes from an
// attachment comment, which anyone who can edit the page controls.
func destPath(root string, a client.Attachment, flat bool) (string, error) {
	rel := a.Title
	if !flat {
		if src := a.Meta().Source; src != "" {
			rel = src
		}
	}
	// A stored name is a single path element by construction; guard anyway, since
	// it is server data.
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("attachment %q resolves to the absolute path %q", a.Title, rel)
	}

	path := filepath.Join(root, filepath.FromSlash(rel))
	if path != root && !strings.HasPrefix(path, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("attachment %q resolves to %q, outside the destination directory",
			a.Title, path)
	}
	if path == root {
		return "", fmt.Errorf("attachment %q has no filename", a.Title)
	}
	return path, nil
}

// report prints the per-attachment outcomes and returns the command's exit
// status: 1 if any attachment failed.
func report(results []outcome) error {
	failed := 0
	skipped := 0
	for _, r := range results {
		switch r.status {
		case statusFailed:
			failed++
		case statusSkipped:
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
		switch r.status {
		case statusSkipped:
			ui.Dim(fmt.Sprintf("%-10s %s  (exists; --force to overwrite)", r.status, r.destPath))
		case statusFailed:
			ui.Error(fmt.Sprintf("%-10s %s: %s", r.status, r.name, r.err))
		default:
			ui.Success(fmt.Sprintf("%-10s %s", r.status, r.destPath))
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
		res := map[string]any{"ok": false, "page_id": pageID, "error": err.Error(), "code": code}
		env := jsonout.NewEnvelope(command, []any{res},
			map[string]int{"total": 1, "succeeded": 0, "failed": 1, "skipped": 0})
		_ = jsonout.Emit(os.Stdout, env)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}
