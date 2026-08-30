// Package check implements the `markfluence check` command: validate one or
// more markdown files against the converter and frontmatter rules with no
// network access and no credentials. It writes nothing -- not to Confluence,
// not to disk.
package check

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// checkBaseURL and checkSpaceKey are the regression suite's own defaults,
// used unconditionally rather than exposed as flags. Both are used only to
// build the *text* of a rewritten doc-link href (internal/convert/links.go);
// nothing in ConfluencePage.Broken/Warnings reads either, since resolution
// runs off the link index, not these strings. Hardcoding them makes check
// byte-identical across machines, which is what a CI gate wants.
const (
	checkBaseURL  = "https://wiki.example.net"
	checkSpaceKey = "ENG"
)

var showHTML bool

// Cmd is the check command.
var Cmd = &cobra.Command{
	Use:   "check FILE...",
	Short: "Validate markdown files against the converter and frontmatter rules, offline",
	Long: "Validate one or more markdown FILEs against the converter and frontmatter\n" +
		"rules, with no network access and no credentials -- fast, safe, and\n" +
		"CI/agent-friendly. Reports conversion warnings and broken image/link\n" +
		"references, and frontmatter sanity (parseable, page_width valid, page_id\n" +
		"numeric when present). Each file is processed independently; the command\n" +
		"exits non-zero if any file is broken or failed outright. Warnings alone do\n" +
		"not fail: an unpublished sibling link (no page_id yet) is the normal state\n" +
		"of a tree that hasn't been published, not a defect.",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&showHTML, "show-html", false,
		"Also print the converted storage HTML and attachment list, for debugging.")
}

func run(cmd *cobra.Command, args []string) error {
	rootOverride, _ := cmd.Flags().GetString("root")
	roots := project.NewCache(rootOverride)
	defer roots.Close()
	indexes := linkindex.NewCache()

	failures := 0
	results := make([]*checkResult, 0, len(args))
	for _, filename := range args {
		r := processFile(filename, roots, indexes)
		results = append(results, r)
		if !ui.IsJSON() {
			r.renderHuman()
		}
		if !r.ok {
			failures++
		}
	}
	for _, dir := range roots.Roots() {
		ui.Info("root: " + dir)
	}

	if ui.IsJSON() {
		items := make([]any, len(results))
		for i, r := range results {
			items[i] = r.jsonResult()
		}
		env := jsonout.NewEnvelope("check", items, summarize(results))
		env.Roots = roots.Roots()
		if err := jsonout.Emit(os.Stdout, env); err != nil {
			return err
		}
		if failures > 0 {
			return ui.SilentExit(1)
		}
		return nil
	}

	if failures > 0 {
		ui.Error(fmt.Sprintf("%d of %d file(s) failed.", failures, len(args)))
		return ui.ErrSilent
	}
	return nil
}

// processFile validates one file and returns a result describing the
// outcome. It performs no output itself; the caller renders the result. It
// never writes to Confluence or to disk, and never constructs a
// client.ConfluenceClient.
func processFile(filename string, roots *project.Cache, indexes *linkindex.Cache) *checkResult {
	r := &checkResult{file: filename}
	mf, err := frontmatter.ParseFile(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}

	if _, err := pagewidth.Declared(mf.Frontmatter); err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	if pageID := mf.PageID(); pageID != "" && !pageref.IsDigits(pageID) {
		return r.fail(errors.New(pageref.NotNumericMessage(pageID)), jsonout.CodeValidation)
	}

	abs, err := filepath.Abs(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		return r.fail(fmt.Errorf("resolving the documentation root: %w", err), jsonout.CodeIO)
	}
	index, err := indexes.Get(root)
	if err != nil {
		return r.fail(fmt.Errorf("building the link index: %w", err), jsonout.CodeIO)
	}

	page, err := convert.MdToConfluence(mf, root, index, checkBaseURL, checkSpaceKey, buildinfo.Stamp())
	if err != nil {
		return r.fail(err, jsonout.CodeConvert)
	}
	r.broken = page.Broken
	r.warnings = page.Warnings
	if showHTML {
		r.debugHTML = page.HTML
		r.debugAttachments = page.Attachments
		r.hasDebug = true
	}

	switch {
	case len(r.broken) > 0:
		r.ok = false
		r.status = statusBroken
	case len(r.warnings) > 0:
		r.ok = true
		r.status = statusWarnings
	default:
		r.ok = true
		r.status = statusClean
	}
	return r
}
