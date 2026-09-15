// Package diff implements the `markfluence diff` command: show what differs
// between a Confluence page and the local markdown file that publishes to it.
//
// Nothing is written to disk and nothing is written to Confluence.
//
// The output is two things on two streams, which is the shape that makes it
// usable rather than merely readable:
//
//   - **stdout** carries the body as a real unified diff, and nothing else, so
//     `markfluence diff FILE > my.diff` produces a patch that applies to the
//     actual file -- `patch -R -p1`, since the file on disk is the +++ side, or
//     a plain `patch -p1` from `diff --reverse`.
//   - **stderr** carries the frontmatter half as a per-field report. A value
//     difference is a one-line fact rather than a hunk, and only a report can
//     say *where the local value came from* -- its own frontmatter or
//     markfluence.yaml -- which is the difference between an actionable report
//     and "add title: X" with no hint about which file to edit.
//
// Keeping stdout to one clean document is the same rule `children --space`
// follows for its hint, for the same reason, and it is why the frontmatter
// report is not simply printed above the diff.
package diff

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagedoc"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var reverseFlag bool

// Cmd is the diff command.
var Cmd = &cobra.Command{
	Use:   "diff FILE",
	Short: "Show what differs between a page and its local markdown file",
	Long: "Show what differs between a Confluence page and the local markdown\n" +
		"file that publishes to it. Nothing is written, to disk or to Confluence.\n\n" +
		"FILE is one markdown file, and its page comes from its own page_id or\n" +
		"from its entry in markfluence.yaml's pages: block. One file, not a glob:\n" +
		"a diff over a whole tree is output nobody reads.\n\n" +
		"THE OUTPUT IS TWO THINGS, ON TWO STREAMS\n\n" +
		"stdout carries the body as a unified diff and nothing else, so it is a\n" +
		"patch other tools can use:\n\n" +
		"  markfluence diff FILE > my.diff && patch -R -p1 < my.diff\n\n" +
		"It applies to the real file because the frontmatter block is shared\n" +
		"between the two sides rather than diffed, which also keeps hunk line\n" +
		"numbers the ones you would count to in an editor and means no patch can\n" +
		"rewrite a page_id.\n\n" +
		"The labels name the file relative to the documentation root, however the\n" +
		"command was invoked, so run patch from the root -- not from the\n" +
		"directory the file happens to be in.\n\n" +
		"stderr carries the frontmatter half as a per-field report, naming for\n" +
		"each field whether the local value came from the file's frontmatter or\n" +
		"from markfluence.yaml. Redirect it away with 2>/dev/null, or keep only\n" +
		"it with >/dev/null.\n\n" +
		"EXIT CODES ARE diff(1)'s, NOT markfluence's\n\n" +
		"  0  identical\n" +
		"  1  differs (either half)\n" +
		"  2  trouble -- a bad flag, a file naming no page, a page that is gone,\n" +
		"     a rejected credential, a failed fetch\n\n" +
		"So `if markfluence diff FILE >/dev/null; then ...` means \"in sync\".\n" +
		"Every other command reports an operational failure as 1; this one is 2,\n" +
		"because 1 is spoken for.\n\n" +
		"WHICH FIELDS ARE COMPARED\n\n" +
		"Only the ones the file declares, because those are the ones publishing\n" +
		"would assert: an absent labels or page_width leaves the page's alone, an\n" +
		"absent title keeps the live title. So a file carrying only page_id and\n" +
		"title reports on its title and its body, and nothing else.\n\n" +
		"Two fields are compared but not reconciled by any verb today: update\n" +
		"moves a page neither between spaces nor to a new parent, so a space or\n" +
		"parent difference is a disagreement to fix by hand.\n\n" +
		"DIFFERENCES YOU DID NOT MAKE\n\n" +
		"The Confluence side is the page rendered back to markdown, and that\n" +
		"round trip is lossy in documented ways (guarantees L5 and L6 are both\n" +
		"Partial). Expect these, none of which are defects:\n\n" +
		"  - a table's :--- alignment publishes bare and reads back as ---, and\n" +
		"    a column takes its most common declared alignment\n" +
		"  - a bold span containing a link comes back respelled, once the\n" +
		"    Confluence editor has re-serialized the page\n" +
		"  - a soft line break inside a paragraph becomes a space\n" +
		"  - a table cell colour outside the 21 named swatches comes back as a\n" +
		"    literal hex\n" +
		"  - a macro markfluence does not map comes back as raw storage tags\n\n" +
		"This is why the patch is worth reading before it is worth applying.",
	Example: "  # What would publishing this file change on the page?\n" +
		"  markfluence diff docs/runbook.md\n\n" +
		"  # Just the body patch, as a file\n" +
		"  markfluence diff docs/runbook.md 2>/dev/null > my.diff\n\n" +
		"  # Pull the page's edits into the file, conflicts and all\n" +
		"  markfluence diff --reverse docs/runbook.md | patch -p1\n\n" +
		"  # Is this file in sync?\n" +
		"  if markfluence diff docs/runbook.md >/dev/null 2>&1; then echo yes; fi\n\n" +
		"  # Side by side in an external tool\n" +
		"  markfluence read docs/runbook.md > /tmp/page.md && meld /tmp/page.md docs/runbook.md",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&reverseFlag, "reverse", false,
		"Swap the sides, so the patch applies the page's changes to the file")
}

func run(cmd *cobra.Command, args []string) error {
	filename := args[0]

	rootOverride, _ := cmd.Flags().GetString("root")
	roots := project.NewCache(rootOverride)
	defer roots.Close()

	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		// Two failures wear one signature here: the file could not be read, or
		// its frontmatter could not be parsed. Only this command has one file
		// to be specific about, and jsonout.CodeOr's own contract asks for the
		// distinction -- IO when the file could not be read, VALIDATION when it
		// is wrong.
		code := jsonout.CodeValidation
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) {
			code = jsonout.CodeIO
		}
		return fatalFail(err.Error(), code)
	}
	abs, err := filepath.Abs(filename)
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeIO)
	}
	// The root first: a file's metadata may live in markfluence.yaml rather
	// than in the file, so nothing local can be decided until it is known, and
	// it also decides the placement the page's body is rendered for.
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return fatalFail(project.RootError(err).Error(), rootErrorCode(err))
	}
	key, _ := pagemeta.KeyFor(root, abs)
	meta, err := pagemeta.Resolve(key, mf, root)
	if err != nil {
		// The two locations name different pages, so there is no single local
		// answer to compare the page against.
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	pageID := strings.TrimSpace(meta.Fields["page_id"])
	switch {
	case !meta.Managed(), pageID == "":
		return fatalFail(fmt.Sprintf(
			"%s names no page: give it a page_id, in its frontmatter or in %s's "+
				"pages: block, or publish it with markfluence create",
			filename, project.Filename), jsonout.CodeValidation)
	case !pageref.IsDigits(pageID):
		return fatalFail(pageref.NotNumericMessage(pageID), jsonout.CodeValidation)
	}

	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.ResolveOptions{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile, Roots: roots,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	page, err := c.GetPageBodyOrNil(pageID)
	if err != nil {
		return operationalFail(roots, pageID, err, jsonout.CodeFor(err))
	}
	if page == nil {
		return operationalFail(roots, pageID, errors.New(
			pageref.NotFoundMessage(pageID,
				"re-export the page, or correct the page_id")), jsonout.CodeNotFound)
	}
	// An empty body is compared as an empty body, not refused. read refuses one
	// because it has nothing to print; here it is an answer -- the whole file is
	// an addition. A folder cannot reach this point (every v2 page route answers
	// a folder id with 404, handled above), so the reachable case is a
	// genuinely empty page, which is exactly what `create` leaves behind for a
	// body-less file and what a parent page often is.
	rendered := ""
	if page.Body.Storage.Value != "" {
		if rendered, err = convert.StorageToMarkdown(page.Body.Storage.Value,
			pagedoc.Options(c, page, placementFor(key), pagedoc.NewUserCache())); err != nil {
			return operationalFail(roots, pageID, err, jsonout.CodeConvert)
		}
	}

	confluence, local := documents(mf.Content, mf.Body, rendered)
	body, err := compare(confluence, local, reportPath(root, abs, filename), reverseFlag)
	if err != nil {
		return operationalFail(roots, pageID, err, jsonout.CodeConvert)
	}

	fields, warnings := compareMetadata(c, page, meta, root, filepath.Dir(abs))
	warnings = append(meta.Warnings, warnings...)

	res := result{
		file:     filename,
		page:     page,
		url:      c.PageURL(page, pageID),
		meta:     meta,
		fields:   fields,
		body:     body,
		warnings: warnings,
	}

	if ui.IsJSON() {
		env := jsonout.NewEnvelope("diff", []any{jsonResult(res)},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		env.Roots = roots.Roots()
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		return exitFor(res)
	}

	report(res)
	return exitFor(res)
}

// placementFor renders the page as though it were exported to exactly where the
// file already is, which is what makes an attachment path comparable rather
// than guaranteed-different.
//
// Dir is the file's own directory relative to the documentation root, because
// that is what an attachment's recorded path= is relative to: a page at
// docs/child.md referencing a recorded assets/brand.png has to say
// ../assets/brand.png, exactly as a tree export would write it.
//
// AttachmentDir is left to derive from the page's title, matching where
// attachment-download puts an attachment with no recorded path -- so one added
// in the editor shows up as an added image reference, which is a real
// difference.
//
// Parent is unused: nothing here renders frontmatter, so there is nothing for
// it to override. The parent is compared as a resolved id instead.
func placementFor(key string) pagedoc.Placement {
	dir := path.Dir(key)
	if dir == "." || key == "" {
		dir = ""
	}
	return pagedoc.Placement{Dir: dir}
}

// reportPath is the path the diff labels name, relative to the documentation
// root when there is a real one.
//
// Root-relative rather than as-typed so that `patch -p1` run from the root
// lands on the file however the command was invoked -- `markfluence diff
// ../docs/runbook.md` must not produce a patch naming ../docs/runbook.md.
//
// "A real one" is the part that matters. With no markfluence.yaml anywhere,
// project.Discover falls back to the *starting* directory, which for this
// command is the file's own -- so a root-relative path would be the bare base
// name, silently dropping the docs/ a reader typed and making the patch apply
// only from that subdirectory. A project whose files carry their own page_id
// needs no marker, so that is an ordinary configuration rather than an edge.
// There the path as typed is the honest answer: it is what the reader sees, and
// `patch -p1` lands from wherever they ran markfluence.
func reportPath(root *project.Root, abs, typed string) string {
	if root == nil || root.Dir == "" || root.File == "" {
		return filepath.ToSlash(filepath.Clean(typed))
	}
	rel, err := filepath.Rel(root.Dir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, "../") {
		return filepath.ToSlash(typed)
	}
	return filepath.ToSlash(rel)
}

// result is everything the two output paths need, so neither computes anything
// the other does not.
type result struct {
	file     string
	page     *client.Page
	url      string
	meta     pagemeta.Resolved
	fields   []difference
	body     bodyDiff
	warnings []string
}

// differs reports whether anything actually disagrees. An uncomparable field is
// reported but does not count: nobody asked the page, so nobody may claim it
// disagrees.
func (r result) differs() bool {
	if r.body.Differs() {
		return true
	}
	for _, d := range r.fields {
		if d.Comparable {
			return true
		}
	}
	return false
}

// exitFor is the diff(1) contract: 1 when the two sides differ, 0 when they do
// not. Trouble exits 2 through fatalFail/operationalFail.
func exitFor(r result) error {
	if r.differs() {
		return ui.SilentExit(1)
	}
	return nil
}

// rootErrorCode classifies a root-resolution failure. A malformed project file
// is a local defect in a file the author can open and fix, so reporting it as
// I/O would send the reader looking for a disk fault.
func rootErrorCode(err error) jsonout.Code {
	if project.IsConfigError(err) {
		return jsonout.CodeValidation
	}
	return jsonout.CodeIO
}

// fatalFail reports trouble before the page was identified: a JSON error object
// on stderr under --json, else a human error line. Exits 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "diff", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports trouble that names the page: under --json a
// results[0] entry {ok:false,error,code}, else a human error line.
//
// Exits 2 rather than read's 1, because this command spends 1 on "differs" --
// see Cmd.Long. That is the whole cost of the diff(1) contract, and it is
// cheaper than making a shell script parse JSON to ask one question.
func operationalFail(
	roots *project.Cache, pageID string, err error, code jsonout.Code,
) error {
	if ui.IsJSON() {
		_ = jsonout.Emit(os.Stdout, failEnvelope(roots, pageID, err, code))
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(2)
}

// failEnvelope is the document operationalFail writes, split out so the schema
// conformance test can validate the envelope this command really emits instead
// of a hand-copied duplicate of it.
func failEnvelope(
	roots *project.Cache, pageID string, err error, code jsonout.Code,
) jsonout.Envelope {
	env := jsonout.NewEnvelope("diff", []any{jsonout.NewSingleOpFailure(pageID, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
	if roots != nil {
		env.Roots = roots.Roots()
	}
	return env
}
