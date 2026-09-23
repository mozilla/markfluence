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
	"strings"

	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/labels"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/pagestatus"
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
	Short: "Check Markdown files for problems, with no network access",
	Long: "Check one or more Markdown FILEs for problems before you publish them.\n" +
		"check makes no network request and needs no credentials, so it is fast and\n" +
		"safe to run in CI or from an agent.\n\n" +
		"check reports:\n\n" +
		"  - conversion warnings\n" +
		"  - broken images and broken links\n" +
		"  - frontmatter that does not parse\n" +
		"  - a page_width that is not valid\n" +
		"  - a page_id that is not a number\n" +
		"  - a title or page_status that is present but empty\n" +
		"  - a markfluence.yaml that markfluence cannot load\n\n" +
		"check does each file separately. It exits with a code that is not zero if any\n" +
		"file is broken or failed. A warning alone does not fail.\n\n" +
		"check reads the metadata of a file from its frontmatter and from its pages:\n" +
		"entry in markfluence.yaml. It reports an entry only when that file is one of\n" +
		"the FILEs. Thus one bad entry does not stop the check of the other files. Two\n" +
		"locations that name different pages are an error. A file with its own\n" +
		"frontmatter in a project that uses pages: gets a warning, because both work.\n\n" +
		"check cannot tell whether a page can have the status that page_status names.\n" +
		"Confluence decides that for each page and each account, and check makes no\n" +
		"request. Thus check reports an empty page_status, but not a misspelled one.\n" +
		"update and create do a check of the name.\n\n" +
		"\"link not resolved: TARGET\" means that TARGET is a .md file under the\n" +
		"documentation root that has no page_id yet. That is the usual state of a tree\n" +
		"that nobody published yet, and it is not a defect.\n\n" +
		"\"same-page anchor not resolved: #heading\" is the same for an anchor in the\n" +
		"current file. The heading exists, but markfluence cannot make its URL until\n" +
		"this file has a page_id. The first publish of the file resolves it.",
	Example: "  # Check a set of files\n" +
		"  markfluence check docs/*.md\n\n" +
		"  # Show the storage HTML that a publish would send\n" +
		"  markfluence check --show-html docs/one-page.md\n",
	Args:              cobra.MinimumNArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func init() {
	Cmd.Flags().BoolVar(&showHTML, "show-html", false,
		"Also print the converted storage HTML and the list of attachments, for debugging.")
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
		if !r.ok() {
			failures++
		}
	}
	for _, dir := range roots.Roots() {
		ui.Info("root: " + dir)
	}
	// Under --debug only, and beside the root it belongs to: a project-wide
	// default takes effect for a file that says nothing about it, so it has no
	// answer anywhere in the file a reader would open.
	project.ReportSettings(roots)

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

	// The root first, because a file's metadata may live in the project file's
	// pages: block rather than in the file -- and then everything below
	// validates the *resolved* metadata, so an entry's values are checked
	// exactly as a file's own are. That is #139's per-file scoping made real:
	// an entry is diagnosed when its file is named, and never otherwise.
	abs, err := filepath.Abs(filename)
	if err != nil {
		return r.fail(err, jsonout.CodeIO)
	}
	root, err := roots.Resolve(filepath.Dir(abs))
	if err != nil {
		code := jsonout.CodeIO
		if project.IsConfigError(err) {
			// A markfluence.yaml that cannot be understood is a local defect in
			// a file the author can open and fix, which is check's whole
			// subject -- reporting it as I/O would send the reader looking for
			// a disk fault.
			code = jsonout.CodeValidation
		}
		return r.fail(project.RootError(err), code)
	}
	key, _ := pagemeta.KeyFor(root, abs)
	meta, err := pagemeta.Resolve(key, mf, root)
	if err != nil {
		// The two locations name different pages. Offline-visible, and exactly
		// the kind of defect check exists to catch before a publish does.
		return r.fail(err, jsonout.CodeValidation)
	}

	// A soft disagreement between the two locations, which D6 promises is
	// reported wherever metadata is resolved -- not only by update.
	r.warnings = append(r.warnings, meta.Warnings...)

	if _, err := pagewidth.Declared(meta.Fields); err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	// An invalid label is a guaranteed publish defect that needs no network to
	// see, the same class as an invalid page_width -- and worse in one way: a
	// name Confluence splits on a space publishes successfully, as the wrong
	// labels, and then cannot be removed by any spelling of the file (see
	// docs/confluence/labels.md). Catching it offline is the cheapest place it
	// can be caught.
	labelSet, err := labels.Declared(meta.Lists, meta.Fields)
	if err != nil {
		return r.fail(err, jsonout.CodeValidation)
	}
	if pageID := strings.TrimSpace(meta.Fields["page_id"]); pageID != "" && !pageref.IsDigits(pageID) {
		return r.fail(errors.New(pageref.NotNumericMessage(pageID)), jsonout.CodeValidation)
	}
	index, err := indexes.Get(root)
	if err != nil {
		return r.fail(fmt.Errorf("building the link index: %w", err), jsonout.CodeIO)
	}

	// Collected before the conversion, which can bail out: a defect found
	// without the converter -- in the frontmatter or in the project file -- is
	// independent of anything the converter finds, and reporting it only when
	// the body happens to convert would hide it behind an unrelated failure.
	//
	// A title that is present and empty is a guaranteed publish failure needing
	// no network to see: create and update both reject it. The narrowness
	// elsewhere -- never reporting whether page_id/space/parent are set -- holds
	// because check cannot know which verb is coming, and that reasoning stops
	// applying once both verbs agree. An absent title stays unreported: update
	// accepts it and keeps the live page's title.
	var localBroken []string
	// A project-wide page_width Confluence does not accept, reported only for a
	// file that would actually use it -- one whose own frontmatter declares no
	// width. A file that declares its own wins over the project file (the
	// chain is flag > frontmatter > project file), so reporting the project's
	// bad value there would fail a file that publishes perfectly well, and
	// check's rule is that a false positive is worse than a miss.
	//
	// This is where a project-wide width is validated offline at all:
	// internal/project cannot check its own value, since it would have to
	// import internal/pagewidth, which imports internal/client, which holds a
	// *project.Cache. check is the one verb that can find it without
	// publishing.
	//
	// Broken rather than a warning, matching an invalid frontmatter page_width:
	// for the files it is reported on, the publish really would fail. And
	// reported per file rather than once for the run, which is what keeps every
	// diagnostic scoped to the files actually named -- a file under a different
	// project hears nothing about this one.
	// meta.Fields, not mf.Frontmatter: a width declared in this file's pages:
	// entry wins over the project default exactly as one in its frontmatter
	// does, so reading the file alone reported the project's bad value against
	// a file that publishes perfectly well -- the false positive the paragraph
	// above says check must not produce.
	if root.Config.PageWidth != "" && strings.TrimSpace(meta.Fields["page_width"]) == "" {
		if _, err := pagewidth.Declared(
			map[string]string{"page_width": root.Config.PageWidth}); err != nil {
			localBroken = append(localBroken, fmt.Sprintf("%s: %s", root.File, err))
		}
	}
	if title, present := meta.Fields["title"]; present && strings.TrimSpace(title) == "" {
		localBroken = append(localBroken,
			"the title is present but empty; give it a value or remove it")
	}
	// The shape of page_status and nothing more. A present-but-empty value is
	// the title case exactly -- both verbs reject it, so no verb makes it valid
	// and there is no false positive to have.
	//
	// The *name* is deliberately unchecked, and cannot be checked here: the
	// statuses a space offers are that space's own configuration, read from
	// Confluence per space, and check has no client, no credentials and no
	// network. This is the first frontmatter field whose vocabulary is server
	// state rather than a fixed list, which is why check's Long says so --
	// otherwise a clean run reads as a promise the status will publish.
	if _, _, err := pagestatus.Declared(meta.Fields); err != nil {
		localBroken = append(localBroken, err.Error())
	}

	// "No half-and-half" (#139): a file carrying its own markfluence keys in a
	// project that has chosen the manifest. A *warning*, never an error, and
	// that is the whole point -- agreement between the two locations is legal,
	// so this has to be sayable without becoming a wall somebody hits halfway
	// through a migration. The remedy is manual -- move the keys into the entry
	// by hand -- and deliberately so: nothing writes an existing file's
	// frontmatter for you now that `fix` is gone (#151), and `fix` never
	// actually moved them into the manifest anyway, it refused the file.
	if pagemeta.HasManifest(root) && meta.InFile() {
		r.warnings = append(r.warnings, fmt.Sprintf(
			"this file carries markfluence frontmatter in a project that keeps page "+
				"metadata in %s; both work, but keeping it in one place is clearer",
			project.Filename))
	}

	page, err := convert.MdToConfluence(mf, root, index, checkBaseURL, checkSpaceKey)
	if err != nil {
		// Two assets wanting one attachment name is a defect in the document,
		// not a failure of the converter: the author fixes it by renaming a
		// file, exactly as they would fix a dead link. Reported as Broken so it
		// reads that way and lands in the same list, rather than as a failed
		// file whose error field a reader has to interpret.
		//
		// It is the only entry the file gets, which is the one way this is
		// weaker than the rest of check: a collision aborts the conversion, so
		// any broken link found before it is discarded with the page, and a
		// second collision is never reached. Fix the collision and re-run for
		// the full list. Enumerating both would mean the converter carrying on
		// past a document it has already refused to publish.
		var collision *convert.NameCollisionError
		if errors.As(err, &collision) {
			r.broken = append(localBroken, collision.Error())
			r.status = statusBroken
			return r
		}
		return r.fail(err, jsonout.CodeConvert)
	}
	r.broken = append(localBroken, page.Broken...)
	// Appended rather than assigned: the half-and-half lint above has already
	// put a warning here, and assigning discarded it. Label warnings still
	// lead the converter's -- they are a property of the declared metadata, so
	// they hold whatever the converter went on to find in the body.
	r.warnings = append(r.warnings, labelSet.Warnings...)
	r.warnings = append(r.warnings, page.Warnings...)
	if showHTML {
		r.debugHTML = page.HTML
		r.debugAttachments = page.Attachments
		r.hasDebug = true
	}

	switch {
	case len(r.broken) > 0:
		r.status = statusBroken
	case len(r.warnings) > 0:
		r.status = statusWarnings
	default:
		r.status = statusClean
	}
	return r
}
