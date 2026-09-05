// Package export implements the `markfluence export` command: write a
// Confluence page and the attachments it uses to a directory.
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "export"

// statusSkippedUnreferenced marks an attachment left behind because the page
// does not reference it. It is not attachfile's concern -- that package writes
// what it is given -- so it only ever appears in this command's output.
const statusSkippedUnreferenced = "skipped_unreferenced"

var (
	dest           string
	fileFlag       string
	allAttachments bool
	skipAttachs    bool
	force          bool
	dryRun         bool
)

// Cmd is the export command.
var Cmd = &cobra.Command{
	Use:   command + " PAGE",
	Short: "Write a Confluence page and its attachments to a directory",
	Long: "Write a Confluence page and the attachments it uses to a directory.\n\n" +
		"PAGE is a numeric page id, a Confluence page URL, or a markdown file\n" +
		"whose frontmatter has a page_id.\n\n" +
		"The page is written as markdown with title/space/parent/page_id/\n" +
		"page_width frontmatter -- the same output `read` prints -- so an\n" +
		"exported file can be edited and published back with update.\n\n" +
		"Attachments are written to the paths the page's images were published\n" +
		"from, so the exported tree matches the source repo's layout and\n" +
		"previews locally. Only attachments the page references are exported;\n" +
		"--all-attachments takes everything on the page.\n\n" +
		"This is the one-command form of `read` plus `attachment-download`.",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&dest, "dest", ".", "Directory to write the export into.")
	Cmd.Flags().StringVar(&fileFlag, "file", "",
		"Name for the page file (default: a slug of the title, or the page id if that slugs to nothing).")
	Cmd.Flags().BoolVar(&allAttachments, "all-attachments", false,
		"Export every attachment on the page, not just the referenced ones.")
	Cmd.Flags().BoolVar(&skipAttachs, "skip-attachments", false,
		"Write the page file only.")
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
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	root, err := filepath.Abs(dest)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
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

	if dryRun && !ui.IsJSON() {
		ui.Warn("DRY RUN — no files will be written.")
	}

	res := export(c, page, root)
	return report(res)
}

// result is everything one export produced.
type result struct {
	page        *client.Page
	destPath    string
	pageStatus  string // "wrote" or attachfile.StatusSkipped
	attachments []attachment
	warnings    []string
	err         error
	code        jsonout.Code
}

// attachment is one attachment's outcome, including the unreferenced ones this
// command reports but does not write.
type attachment struct {
	name     string
	destPath string
	status   string
	err      error
	code     jsonout.Code
}

// export writes the page file and its attachments.
func export(c *client.ConfluenceClient, page *client.Page, root string) result {
	res := result{page: page}

	atts, err := attachmentsFor(c, page)
	if err != nil {
		// The listing is needed both to resolve image paths and to know what to
		// download, so unlike read -- which tolerates a failure and falls back to
		// decoding names -- an export cannot quietly produce a partial tree.
		res.err, res.code = err, jsonout.CodeFor(err)
		return res
	}

	doc, err := pagedoc.Render(c, page)
	if err != nil {
		res.err, res.code = err, jsonout.CodeConvert
		return res
	}

	res.destPath = filepath.Join(root, pageFilename(page, fileFlag))
	res.pageStatus, err = writePage(res.destPath, doc.String())
	if err != nil {
		res.err, res.code = err, jsonout.CodeIO
		return res
	}

	referenced := convert.ReferencedAttachmentNames(page.Body.Storage.Value)
	res.warnings = missingReferences(referenced, atts)
	if skipAttachs {
		return res
	}
	res.attachments = writeAttachments(c, atts, referenced, root)
	return res
}

// attachmentsFor lists the page's attachments, skipping the call when the body
// references none and every attachment would be filtered out anyway.
func attachmentsFor(c *client.ConfluenceClient, page *client.Page) ([]client.Attachment, error) {
	if skipAttachs && !strings.Contains(page.Body.Storage.Value, "<ri:attachment") {
		return nil, nil
	}
	return c.ListAttachments(page.ID)
}

// writePage writes the document, honoring --force and --dry-run. It reports
// whether the file was written or skipped.
func writePage(path, content string) (string, error) {
	if _, err := os.Stat(path); err == nil && !force {
		return attachfile.StatusSkipped, nil
	}
	if dryRun {
		return statusWrote, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return statusWrote, nil
}

const statusWrote = "wrote"

// writeAttachments downloads the attachments to export, reporting the ones left
// behind rather than dropping them silently.
func writeAttachments(
	c *client.ConfluenceClient, atts []client.Attachment, referenced map[string]bool, root string,
) []attachment {
	opts := attachfile.Options{Root: root, Force: force, DryRun: dryRun}
	out := make([]attachment, 0, len(atts))
	for _, a := range atts {
		if !allAttachments && !referenced[a.Title] {
			out = append(out, attachment{name: a.Title, status: statusSkippedUnreferenced})
			continue
		}
		w := attachfile.Write(c, a, opts)
		out = append(out, attachment{
			name: w.Name, destPath: w.DestPath, status: w.Status, err: w.Err, code: w.Code,
		})
	}
	return out
}

// missingReferences reports names the page refers to that are not attached --
// already broken in Confluence. They are warnings, not failures: the page's
// problem is not the export's, and blocking would prevent exporting a page in
// order to fix it.
func missingReferences(referenced map[string]bool, atts []client.Attachment) []string {
	have := make(map[string]bool, len(atts))
	for _, a := range atts {
		have[a.Title] = true
	}
	var missing []string
	for name := range referenced {
		if !have[name] {
			missing = append(missing, fmt.Sprintf("%s is referenced but not attached", name))
		}
	}
	sort.Strings(missing) // map iteration order would make output nondeterministic
	return missing
}

// slugUnsafeRE matches everything a filename slug drops: anything that is not a
// letter, digit, underscore, hyphen, or whitespace. Unicode letters are kept, so
// a non-Latin title still yields a usable name.
var slugUnsafeRE = regexp.MustCompile(`[^\p{L}\p{N}_\s-]+`)

// whitespaceRE collapses each whitespace run into a single hyphen.
var whitespaceRE = regexp.MustCompile(`\s+`)

// slugMax caps the slug so a long title can't produce a filename the filesystem
// rejects; 80 leaves room for the extension well inside every limit.
const slugMax = 80

// pageFilename is the name to write the page under: --file when given, else a
// slug of the title, else the page id.
//
// The slug is filename-specific rather than the converter's heading-anchor
// sluggers: it must drop path separators, cap length, and produce something
// usable when a title slugs to nothing. Reusing an anchor slugger would also
// mean a change to anchor generation silently renaming exported files.
func pageFilename(page *client.Page, override string) string {
	if override != "" {
		return override
	}
	if s := slugify(page.Title); s != "" {
		return s + ".md"
	}
	return page.ID + ".md"
}

func slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugUnsafeRE.ReplaceAllString(s, "")
	s = whitespaceRE.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len([]rune(s)) > slugMax {
		s = strings.Trim(string([]rune(s)[:slugMax]), "-")
	}
	return s
}

// report prints the outcome and returns the command's exit status.
func report(res result) error {
	failed := 0
	for _, a := range res.attachments {
		if a.status == attachfile.StatusFailed {
			failed++
		}
	}

	if ui.IsJSON() {
		env := jsonout.NewEnvelope(command, []any{buildResult(res)}, map[string]int{
			"total": 1, "succeeded": boolToInt(res.err == nil), "failed": boolToInt(res.err != nil),
		})
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if res.err != nil || failed > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	if res.err != nil {
		ui.Error(res.err.Error())
		return ui.SilentExit(1)
	}

	if res.pageStatus == attachfile.StatusSkipped {
		ui.Dim(fmt.Sprintf("%-10s %s  (exists; --force to overwrite)", res.pageStatus, res.destPath))
	} else {
		ui.Success(fmt.Sprintf("%-10s %s", res.pageStatus, res.destPath))
	}

	unreferenced := 0
	for _, a := range res.attachments {
		switch a.status {
		case statusSkippedUnreferenced:
			unreferenced++
		case attachfile.StatusSkipped:
			ui.Dim(fmt.Sprintf("%-10s %s  (exists; --force to overwrite)", a.status, a.destPath))
		case attachfile.StatusFailed:
			ui.Error(fmt.Sprintf("%-10s %s: %s", a.status, a.name, a.err))
		default:
			ui.Success(fmt.Sprintf("%-10s %s", a.status, a.destPath))
		}
	}
	if unreferenced > 0 {
		ui.Dim(fmt.Sprintf("           (skipped %d unreferenced attachment(s); "+
			"--all-attachments to include)", unreferenced))
	}
	for _, w := range res.warnings {
		ui.Warn(w)
	}

	if failed > 0 {
		return ui.SilentExit(1)
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
}
