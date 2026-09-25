// Package diff implements the `markfluence diff` command: show what differs
// between a Confluence page and the local Markdown file that publishes to it.
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
	Short: "Show what is different between a page and its local Markdown file",
	Long: "Show what is different between a Confluence page and the local Markdown file\n" +
		"that publishes to it. diff writes nothing, to disk or to Confluence.\n\n" +
		"FILE is one Markdown file. Its page_id comes from its frontmatter or from its\n" +
		"pages: entry in markfluence.yaml. diff takes one file, and not a glob, because\n" +
		"a diff of a whole tree is too long to read.\n\n" +
		"OUTPUT\n\n" +
		"diff writes two things, to two streams.\n\n" +
		"stdout holds the diff of the body in unified format, and nothing else. Thus it\n" +
		"is a patch that other tools can use:\n\n" +
		"  markfluence diff FILE > my.diff && patch -R -p1 < my.diff\n\n" +
		"With --json, stdout holds one JSON document with both parts, and not a patch.\n\n" +
		"The patch applies to the real file, because diff does not compare the\n" +
		"frontmatter block. Both sides share it. Thus the line numbers in each hunk are\n" +
		"the line numbers in your editor, and no patch can change a page_id.\n\n" +
		"The patch names the file relative to the documentation root, so run patch from\n" +
		"the root. If there is no markfluence.yaml, the patch names the file as you\n" +
		"typed it.\n\n" +
		"stderr holds a report on the frontmatter, one field at a time. For each field,\n" +
		"it tells you whether the local value came from the frontmatter or from\n" +
		"markfluence.yaml. To discard the report, add 2>/dev/null. To see only the\n" +
		"report, add >/dev/null.\n\n" +
		"EXIT CODES\n\n" +
		"diff uses the exit codes of diff(1), and not the exit codes of markfluence:\n\n" +
		"  0  the same\n" +
		"  1  different, in the body or in the frontmatter\n" +
		"  2  trouble: a bad flag, a file that names no page, a page that is gone,\n" +
		"     a refused credential, or a failed request\n\n" +
		"Thus `if markfluence diff FILE >/dev/null; then ...` means \"in sync\". Every\n" +
		"other command reports an operational failure as 1. diff reports it as 2,\n" +
		"because 1 has a different meaning here.\n\n" +
		"WHICH FIELDS DIFF COMPARES\n\n" +
		"diff compares only the fields that the file declares, because a publish asserts\n" +
		"only those. An absent labels or page_width leaves the value on the page alone,\n" +
		"and an absent title keeps the live title. Thus for a file with only page_id and\n" +
		"title, diff reports the title and the body, and nothing else.\n\n" +
		"diff compares parent as a page id, so a parent: that names a .md file agrees\n" +
		"with the page when that file's page_id is the page's parent. parent: null means\n" +
		"the top of the space, and update moves the page to agree with parent.\n\n" +
		"diff also compares a space: default in markfluence.yaml when the file declares\n" +
		"no space. update does not move a page to a different space. It refuses the\n" +
		"file, so a difference in space is one that you must correct by hand.\n\n" +
		"DIFFERENCES THAT YOU DID NOT MAKE\n\n" +
		"The Confluence side is the page, rendered back to Markdown. That round trip\n" +
		"loses some details, in documented ways. Expect these differences. None of them\n" +
		"is a defect:\n\n" +
		"  - the alignment :--- of a table column publishes as plain --- and comes\n" +
		"    back as ---, and a column gets its most frequent alignment\n" +
		"  - a bold span that holds a link comes back with a different spelling, after\n" +
		"    the Confluence editor saves the page\n" +
		"  - a soft line break in a paragraph becomes a space\n" +
		"  - a cell color that is not one of the 21 named swatches comes back as a\n" +
		"    literal hex value\n" +
		"  - a macro that markfluence does not map comes back as raw storage tags\n\n" +
		"Thus read the patch before you apply it.",
	Example: "  # What would a publish of this file change on the page?\n" +
		"  markfluence diff docs/runbook.md\n\n" +
		"  # Write only the patch of the body to a file\n" +
		"  markfluence diff docs/runbook.md 2>/dev/null > my.diff\n\n" +
		"  # Put the edits from the page into the file, conflicts included\n" +
		"  markfluence diff --reverse docs/runbook.md | patch -p1\n\n" +
		"  # Is this file in sync?\n" +
		"  if markfluence diff docs/runbook.md >/dev/null 2>&1; then echo yes; fi\n\n" +
		"  # Compare the two in an external tool\n" +
		"  markfluence read docs/runbook.md > /tmp/page.md && meld /tmp/page.md docs/runbook.md",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&reverseFlag, "reverse", false,
		"Swap the two sides. Then the patch applies the changes on the page to the file")
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

	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(envFile)
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
