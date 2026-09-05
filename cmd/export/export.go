// Package export implements the `markfluence export` command: write a
// Confluence page and the attachments it uses to a directory.
package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/attachfile"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagetree"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "export"

// statusSkippedUnreferenced marks an attachment left behind because the page
// does not reference it. It is not attachfile's concern -- that package writes
// what it is given -- so it only ever appears in this command's output.
const statusSkippedUnreferenced = "skipped_unreferenced"

// depthAll is the --depth value meaning "however deep it goes".
const depthAll = "all"

var (
	dest           string
	fileFlag       string
	depthOpt       string
	spaceOpt       string
	allAttachments bool
	skipAttachs    bool
	force          bool
	dryRun         bool
)

// Cmd is the export command.
var Cmd = &cobra.Command{
	Use:   command + " [PAGE]",
	Short: "Write a Confluence page and its attachments to a directory",
	Long: "Write a Confluence page and the attachments it uses to a directory.\n\n" +
		"PAGE is a numeric page id, a Confluence page URL, or a markdown file\n" +
		"whose frontmatter has a page_id.\n\n" +
		"Pass --space KEY instead of a PAGE to export a whole space, whose root\n" +
		"pages become the top level. It needs an explicit --depth, since a space\n" +
		"walk is one pair of requests per page and folder in it and should be\n" +
		"asked for rather than typed by accident.\n\n" +
		"The page is written as markdown with title/space/parent/page_id/\n" +
		"page_width frontmatter -- the same output `read` prints -- so an\n" +
		"exported file can be edited and published back with update.\n\n" +
		"--depth exports the page's descendants too, mirroring the Confluence\n" +
		"hierarchy: a page becomes <slug>.md with a <slug>/ beside it for its\n" +
		"children, a folder becomes a directory, and each child's parent:\n" +
		"points at its parent's file so the tree can be published into fresh\n" +
		"pages. It costs a pair of requests per page and folder walked, plus\n" +
		"the page's own.\n\n" +
		"Attachments markfluence published are written to the paths their\n" +
		"images came from; one that originated in Confluence is written under\n" +
		"the page's own directory, since attachment names are unique per page\n" +
		"and not per space. Only attachments the page references are exported;\n" +
		"--all-attachments takes everything on the page.\n\n" +
		"This is the one-command form of `read` plus `attachment-download`.",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().StringVar(&dest, "dest", ".", "Directory to write the export into.")
	Cmd.Flags().StringVar(&depthOpt, "depth", "0",
		`How deep to export: 0 for the page alone, a positive number, or "all".`)
	Cmd.Flags().StringVar(&spaceOpt, "space", "",
		"Export a whole space, by key, instead of a PAGE.")
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
	completion.RegisterFlag(Cmd, "depth", completion.Values("0", "1", "2", depthAll))
	// A space key lives on the server, and completion runs on every keystroke,
	// so it completes to nothing rather than stalling the shell.
	completion.RegisterFlag(Cmd, "space", cobra.NoFileCompletions)
}

func run(cmd *cobra.Command, args []string) error {
	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	// Before the credential check: none of these needs a server to be
	// recognized as a usage error, and reporting a missing token for a command
	// that was mistyped anyway helps nobody.
	if err := checkTarget(args, spaceOpt, cmd.Flags().Changed("depth")); err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}
	depth, err := parseDepth(depthOpt)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}
	if err := checkSpaceDepth(spaceOpt, depth); err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}
	if depth != depthNone && fileFlag != "" {
		// --file names one file, while a page's directory is named from its
		// slug regardless -- so honouring it in a tree would let a page's file
		// and its own subdirectory disagree.
		return fatalFail("--file applies to a single page; it cannot be combined with --depth",
			jsonout.CodeValidation)
	}

	root, err := filepath.Abs(dest)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}

	if spaceOpt != "" {
		return exportSpace(c, spaceOpt, root, depth)
	}

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
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

	marker, err := writeProjectFile(root, depth != depthNone)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}
	results, warnings, err := exportTree(c, page, root, depth)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}
	return report(results, marker, root, warnings)
}

// depthNone is --depth 0: the named page and nothing under it.
const depthNone = 0

// checkTarget requires exactly one of PAGE and --space, and an explicit --depth
// alongside --space.
//
// A space walk with the default depth would export nothing at all -- the space
// itself is not a page and has no file -- and defaulting it to "all" instead
// would make a bare typo fire thousands of requests at a shared instance. So it
// is asked for.
func checkTarget(args []string, space string, depthGiven bool) error {
	switch {
	case len(args) == 0 && space == "":
		return errors.New("no page given: pass a PAGE, or --space KEY to export a whole space")
	case len(args) > 0 && space != "":
		return errors.New("PAGE and --space cannot be combined: --space exports a whole space")
	case space != "" && !depthGiven:
		return errors.New(`--space needs an explicit --depth (--depth all for the whole space)`)
	}
	return nil
}

// checkSpaceDepth refuses a space export that would walk nothing.
//
// checkTarget asks only whether --depth was given, and "0" is given. But a
// space has no file of its own, so depth 0 walks nothing, exports nothing, and
// would still plant a project-marker file at the destination and report
// success.
func checkSpaceDepth(space string, depth int) error {
	if space != "" && depth == depthNone {
		return errors.New("--space with --depth 0 would export nothing: " +
			"a space has no file of its own (--depth all for the whole space)")
	}
	return nil
}

// exportSpace exports a space's pages, its root pages forming the top level.
//
// The key is resolved before the walk even though the route the walk uses takes
// a key: an unknown key is a typo and deserves to be named as one, and the v1
// route reports it as a 404 -- which is also what a rejected credential looks
// like.
func exportSpace(c *client.ConfluenceClient, key, root string, depth int) error {
	spaceID, err := c.ResolveSpaceID(key)
	if err != nil {
		return spaceFail(err, jsonout.CodeFor(err))
	}
	if spaceID == "" {
		return fatalFail(fmt.Sprintf("space %q not found", key), jsonout.CodeValidation)
	}

	if dryRun && !ui.IsJSON() {
		ui.Warn("DRY RUN — no files will be written.")
	}
	marker, err := writeProjectFile(root, true)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}
	nodes, err := pagetree.WalkSpace(c, key, depth)
	if err != nil {
		return spaceFail(err, jsonout.CodeFor(err))
	}
	results, warnings := exportNodes(c, nil, root, nodes)
	return report(results, marker, root, warnings)
}

// parseDepth reads the --depth vocabulary: a non-negative number, or "all".
//
// 0 is legal here and is the default, unlike `children --depth`, which refuses
// it: there it would be a request for no rows at all, while here it is the
// named page by itself -- a real answer, and the one this command gave before
// it could walk.
func parseDepth(v string) (int, error) {
	if v == depthAll {
		return pagetree.AllDepths, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid --depth %q: want a non-negative number or %q", v, depthAll)
	}
	return n, nil
}

// exportTree walks the subtree (when asked) and exports the page and every page
// under it.
//
// A walk failure fails the command rather than one page: pagetree aborts on the
// first listing error, before any page has been exported, so there is no
// partial result to report against.
func exportTree(
	c *client.ConfluenceClient, page *client.Page, root string, depth int,
) ([]result, []string, error) {
	var nodes []pagetree.Node
	if depth != depthNone {
		var err error
		if nodes, err = pagetree.Walk(c, page.ID, depth); err != nil {
			return nil, nil, err
		}
	}
	results, warnings := exportNodes(c, page, root, nodes)
	return results, warnings, nil
}

// exportNodes exports a root page (when there is one) and every walked page
// beneath it, in walk order.
//
// page is nil when the thing named has no file of its own -- a whole space --
// so the walk's top level is the top of the export.
func exportNodes(
	c *client.ConfluenceClient, page *client.Page, root string, nodes []pagetree.Node,
) ([]result, []string) {
	// Which page wrote each destination, so a second page wanting the same file
	// with different content is reported rather than skipped.
	claims := newClaims()
	rootTitle, rootID := "", ""
	if page != nil {
		rootTitle, rootID = page.Title, page.ID
	}
	places, warnings := layout(rootTitle, rootID, nodes)
	// Every page's file is spoken for before a single attachment is written.
	// An attachment's recorded path can name any file under dest, and a
	// parent's attachments are written before its children are exported, so
	// otherwise an attachment lands on a child's file and the child is skipped
	// as "already there" -- silently, and counted as a success.
	for id, p := range places {
		if p.file != "" {
			claims.reservePage(filepath.Join(root, filepath.FromSlash(p.file)), id)
		}
	}

	results := make([]result, 0, len(nodes)+1)
	// failed carries a page's failure down to its descendants: a page whose
	// parent was not written must not be written with a parent: pointing at a
	// file that does not exist. create's precedent for the same shape.
	failed := map[string]bool{}

	if page != nil {
		place := places[page.ID]
		if fileFlag != "" {
			place.file = fileFlag
		}
		r := exportOne(c, page, root, pagedoc.Placement{AttachmentDir: place.childDir}, place, claims)
		if r.err != nil {
			failed[page.ID] = true
		}
		results = append(results, r)
	}

	for _, n := range nodes {
		if n.Type == pagetree.TypeFolder {
			// A folder shapes paths and nothing else: no file, no result row,
			// and its directory appears only because something lands in it.
			continue
		}
		place := places[n.ID]
		switch {
		case failed[n.ParentID]:
			failed[n.ID] = true
			results = append(results, failedResult(n, place,
				errors.New("parent page was not exported; skipping"), jsonout.CodeValidation))
			continue
		}
		child, err := c.GetPageBodyOrNil(n.ID)
		switch {
		case err != nil:
			failed[n.ID] = true
			results = append(results, failedResult(n, place, err, jsonout.CodeFor(err)))
		case child == nil || child.Body.Storage.Value == "":
			failed[n.ID] = true
			results = append(results, failedResult(n, place,
				fmt.Errorf("page %s has no readable body", n.ID), jsonout.CodeValidation))
		default:
			r := exportOne(c, child, root, pagedoc.Placement{
				Dir: place.dir, AttachmentDir: place.childDir, Parent: place.parentFile,
			}, place, claims)
			if r.err != nil {
				failed[n.ID] = true
			}
			results = append(results, r)
		}
	}

	// Collision warnings belong to the run rather than to any one page, and a
	// page's own warnings are not printed when that page failed -- so they are
	// returned rather than folded into a result.
	return results, warnings
}

// failedResult is a page that could not be exported, carrying enough of the
// walk's row to be reported without a fetched page.
func failedResult(n pagetree.Node, place placement, err error, code jsonout.Code) result {
	return result{
		node: &n, place: place, err: err, code: code,
	}
}

// result is everything one page's export produced.
type result struct {
	page *client.Page
	// node is the walk row for a page that was never fetched -- one whose body
	// failed, or that was skipped because an ancestor did. Exactly one of page
	// and node is set.
	node        *pagetree.Node
	place       placement
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

// exportOne writes one page's file and its attachments. pl positions the body
// and its frontmatter; place says where the file goes.
func exportOne(
	c *client.ConfluenceClient, page *client.Page, root string,
	pl pagedoc.Placement, place placement, claims *destClaims,
) result {
	res := result{page: page, place: place}
	res.destPath = filepath.Join(root, filepath.FromSlash(place.file))

	atts, err := attachmentsFor(c, page)
	if err != nil {
		// The listing is needed both to resolve image paths and to know what to
		// download, so unlike read -- which tolerates a failure and falls back to
		// decoding names -- an export cannot quietly produce a partial tree.
		res.err, res.code = err, jsonout.CodeFor(err)
		return res
	}

	// A file that is already there is not written again (S3), and rendering it
	// only to throw the result away is what makes a retry cost as much as the
	// first run. Skipping the render skips the page-width read and every
	// <ac:link> title lookup with it.
	//
	// The attachment pass below still runs, which is what lets a retry finish a
	// run that died partway through *attachments* rather than partway through
	// pages.
	if exists(res.destPath) && !force {
		res.pageStatus = attachfile.StatusSkipped
	} else {
		pl.Attachments = atts
		doc, err := pagedoc.Render(c, page, pl)
		if err != nil {
			res.err, res.code = err, jsonout.CodeConvert
			return res
		}
		if res.pageStatus, err = writePage(res.destPath, doc.String()); err != nil {
			res.err, res.code = err, jsonout.CodeIO
			return res
		}
	}

	referenced := convert.ReferencedAttachmentNames(page.Body.Storage.Value)
	res.warnings = missingReferences(referenced, atts)
	if skipAttachs {
		return res
	}
	res.attachments = writeAttachments(c, page, atts, referenced, root, pl, claims)
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

// exists reports whether stat succeeded. Any stat failure -- including a
// permission error on a parent directory -- reads as "not there", so the write
// goes ahead and fails with a message naming the real problem rather than this
// reporting a skip it cannot justify.
func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// writePage writes the document, honoring --force and --dry-run. It reports
// whether the file was written or skipped.
func writePage(path, content string) (string, error) {
	if exists(path) && !force {
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
	c *client.ConfluenceClient, page *client.Page, atts []client.Attachment,
	referenced map[string]bool, root string, pl pagedoc.Placement, claims *destClaims,
) []attachment {
	// Dir must be the AttachmentDir the body was rendered with, or an attachment
	// with no recorded path is written somewhere its own image does not point.
	// Taken from pagedoc rather than recomputed here, so the two cannot drift.
	opts := attachfile.Options{
		Root: root, Dir: pagedoc.AttachmentDirFor(page, pl), Force: force, DryRun: dryRun,
	}
	out := make([]attachment, 0, len(atts))
	for _, a := range atts {
		if !allAttachments && !referenced[a.Title] {
			out = append(out, attachment{name: a.Title, status: statusSkippedUnreferenced})
			continue
		}
		if dest, err := attachfile.Resolve(a, opts); err == nil {
			if conflict := claims.claim(dest, page, a); conflict != nil {
				out = append(out, attachment{
					name: a.Title, destPath: dest, status: attachfile.StatusFailed,
					err: conflict, code: jsonout.CodeValidation,
				})
				continue
			}
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

// report prints the outcomes and returns the command's exit status.
func report(results []result, marker, destRoot string, runWarnings []string) error {
	failed, succeeded, skipped := 0, 0, 0
	for _, r := range results {
		if r.err != nil || anyAttachmentFailed(r) {
			failed++
			continue
		}
		succeeded++
		if r.pageStatus == attachfile.StatusSkipped {
			skipped++
		}
	}

	if ui.IsJSON() {
		env := envelope(results, marker, destRoot, succeeded, failed, skipped)
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if failed > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	for _, w := range runWarnings {
		ui.Warn(w)
	}
	if marker == markerWrote {
		ui.Success(fmt.Sprintf("%-10s %s", statusWrote, filepath.Join(destRoot, project.Filename)))
	}
	for _, r := range results {
		reportOne(r)
	}
	if len(results) > 1 {
		ui.Info(fmt.Sprintf("%d pages (%d exported, %d skipped, %d failed)",
			len(results), succeeded-skipped, skipped, failed))
	}
	if failed > 0 {
		return ui.SilentExit(1)
	}
	return nil
}

// envelope is the document --json emits, split out so the schema conformance
// test validates what the command really writes rather than a hand-copied
// duplicate of it.
func envelope(results []result, marker, destRoot string, succeeded, failed, skipped int) jsonout.Envelope {
	out := make([]any, 0, len(results))
	for _, r := range results {
		out = append(out, buildResult(r))
	}
	env := jsonout.NewEnvelope(command, out, jsonExportSummary{
		Total: len(results), Succeeded: succeeded, Failed: failed, Skipped: skipped,
		ProjectFile: nullable(marker),
	})
	// dest is the root every path in this export is relative to, and the marker
	// is what makes it one.
	if marker != markerSkipped {
		env.Roots = []string{destRoot}
	}
	return env
}

// reportOne prints one page's lines: the page file, then its attachments.
func reportOne(r result) {
	if r.err != nil {
		ui.Error(fmt.Sprintf("%-10s %s: %s", attachfile.StatusFailed, r.title(), r.err))
		return
	}

	if r.pageStatus == attachfile.StatusSkipped {
		ui.Dim(fmt.Sprintf("%-10s %s  (exists; --force to overwrite)", r.pageStatus, r.destPath))
	} else {
		ui.Success(fmt.Sprintf("%-10s %s", r.pageStatus, r.destPath))
	}

	unreferenced := 0
	for _, a := range r.attachments {
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
	for _, w := range r.warnings {
		ui.Warn(w)
	}
}

// title identifies a page in human output, whether or not it was ever fetched.
func (r result) title() string {
	switch {
	case r.place.file != "":
		return r.place.file
	case r.page != nil:
		return r.page.Title
	case r.node != nil:
		return r.node.Title
	}
	return ""
}

// anyAttachmentFailed reports whether a page that itself exported cleanly left
// an attachment behind, which still fails the run.
func anyAttachmentFailed(r result) bool {
	for _, a := range r.attachments {
		if a.status == attachfile.StatusFailed {
			return true
		}
	}
	return false
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

// spaceFail reports a failure of a whole-space walk: there is no page id to
// name, so it takes the stderr errorObject shape children --space established
// for exactly this, which find and search share.
func spaceFail(err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, err.Error(), code)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
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
		jsonExportSummary{Total: 1, Failed: 1})
}
