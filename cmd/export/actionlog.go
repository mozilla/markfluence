package export

// The action log lines an export records (#149).
//
// export is the other end of the merge base. Arrangement 2 -- Confluence is the
// source of truth, a local copy is an export that gets edited and published
// back -- begins here, so without a line written now the first `update` of an
// exported file has nothing to compare against and overwrites whatever the page
// has become in the meantime.
//
// It takes two passes, and the split is forced rather than chosen: see
// recordWalk and recordShas.

import (
	"path/filepath"

	"github.com/mozilla/markfluence/internal/actionlog"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/linkindex"
	"github.com/mozilla/markfluence/internal/pagemeta"
	"github.com/mozilla/markfluence/internal/project"
	"github.com/mozilla/markfluence/internal/ui"
)

// recorder is the export's action log plus the root its keys are relative to.
// A nil *recorder records nothing, which is what a destination with no project
// file above it resolves to.
type recorder struct {
	log  *actionlog.Log
	root *project.Root
}

// newRecorder resolves the destination's root and its log.
//
// The root is *discovered* rather than assumed to be dest: a single-page export
// into an existing docs tree has its root further up, and a key has to be
// relative to the root whose log it will be looked up in. For a multi-page
// export writeProjectFile has already planted a marker at dest, so the walk
// stops there -- which is why this runs after it.
//
// Nothing here can fail the export. A destination with no root gets no log at
// all, which is #139's rule: a root with no project file refuses rather than
// creating one.
func newRecorder(dest string) *recorder {
	if dryRun {
		return nil
	}
	root, err := project.Discover(dest)
	if err != nil {
		// Never fatal -- the files are what the command is for -- but not
		// silent either. A malformed markfluence.yaml above the destination is
		// fatal to `update` and inert here, so without this line the author
		// gets no hint that the project file is why divergence detection will
		// later find no bases.
		ui.Debug("not recording this export: " + err.Error())
		return nil
	}
	log := actionlog.For(root)
	if log == nil {
		_ = root.FS.Close()
		return nil
	}
	return &recorder{log: log, root: root}
}

// close releases the root's os.Root handle.
func (rec *recorder) close() {
	if rec == nil {
		return
	}
	_ = rec.root.FS.Close()
}

// key is the manifest key for a written file, or "" when it has none.
func (rec *recorder) key(destPath string) string {
	abs, err := filepath.Abs(destPath)
	if err != nil {
		return ""
	}
	key, ok := pagemeta.KeyFor(rec.root, abs)
	if !ok {
		return ""
	}
	return key
}

// append writes one line, reporting a failure as a warning on the page's own
// result rather than failing the export: the file is on disk by the time this
// runs, so failing would say it is not.
//
// On the result and not through ui.Hint, which was the first version of this:
// every ui helper is a no-op under --json, so an unwritable .markfluence --
// a read-only checkout, a full disk, a file shadowing the directory -- made
// `export --json` report every page as a clean success while recording no base
// anywhere. A warning is a schema field, which is what update and create
// already use for the identical failure.
func (rec *recorder) append(res *result, e actionlog.Entry) {
	if err := rec.log.Append(e); err != nil {
		res.warnings = append(res.warnings,
			"could not record this export in "+rec.log.Path()+": "+err.Error())
	}
}

// recordable reports whether a result established anything worth recording, and
// its key.
//
// A page that was **skipped** records nothing. export skips a page whose file
// already exists (S3), and that file is somebody's -- possibly edited. A base
// claiming it was derived from this version is a claim export has no grounds
// for, and it would silence divergence detection for exactly the file most
// likely to need it.
func (rec *recorder) recordable(res *result) (string, bool) {
	if rec == nil || res.err != nil || res.page == nil || res.pageStatus != statusWrote {
		return "", false
	}
	key := rec.key(res.destPath)
	return key, key != ""
}

// recordWalk writes one page's line as that page is written, carrying the page
// version and no sha.
//
// During the walk is too early for a sha: a file's render depends on the link
// index over the whole tree, and its siblings are not on disk yet, so a sha
// taken here is not the one a later `update` will recompute. The version is
// knowable now and is the half that matters most -- it is the merge base
// proper, the thing that answers "has the page moved past my copy". recordShas
// appends a fuller line for the same file once the walk finishes.
//
// Writing now rather than only at the end is #139 D10's rule: a run that dies
// partway must leave every already-written page recorded.
func (rec *recorder) recordWalk(res *result) {
	key, ok := rec.recordable(res)
	if !ok {
		return
	}
	rec.append(res, actionlog.Entry{
		Action:      actionlog.ActionExport,
		Status:      actionlog.StatusOK,
		File:        key,
		PageID:      res.page.ID,
		PageVersion: res.page.Version.Number,
	})
}

// recordShas is the second pass: once every file is on disk, render each one
// the way `update` will and append a line carrying its publish sha.
//
// It is self-consistent in the way that matters. The pass hashes *our own
// render*, not a comparison against the page, so it does not depend on
// round-trip fidelity at all -- L5/L6 being Partial is irrelevant here, because
// the value recorded is precisely the one a later `update` recomputes from the
// same file. What it does have to match is update's own inputs, which is why
// the title comes from the file's frontmatter and the space key from the live
// page's webui link.
//
// The cost is one index build plus one conversion per exported page: local CPU
// against files just written, in a command dominated by page GETs and
// attachment downloads. It is not extra work in total, but the first update's
// work moved earlier.
//
// Best-effort throughout. A file the converter refuses gets no sha and keeps
// the version-only line recordWalk already wrote, which still refuses a moved
// page -- the degrade-per-field rule. Nothing here can fail the export.
func (rec *recorder) recordShas(c *client.ConfluenceClient, results []result) {
	if rec == nil {
		return
	}
	// Nothing to record means no index, which is not a micro-optimization:
	// linkindex.Build walks and parses every .md under the *discovered* root
	// rather than under what this run wrote. Re-running an export over an
	// already-exported tree records nothing (every page is skipped), and a
	// single-page export into a large docs repo would otherwise walk the whole
	// ancestor project for one file.
	if !rec.anyRecordable(results) {
		return
	}
	// Built after the walk, deliberately: an index built before it would not
	// see the files this run wrote, which is the whole reason for two passes.
	index, err := linkindex.Build(rec.root)
	if err != nil {
		return
	}
	for i := range results {
		res := &results[i]
		key, ok := rec.recordable(res)
		if !ok {
			continue
		}
		sha, ok := rec.publishSHA(c, index, *res)
		if !ok {
			continue
		}
		rec.append(res, actionlog.Entry{
			Action:        actionlog.ActionExport,
			Status:        actionlog.StatusOK,
			File:          key,
			PageID:        res.page.ID,
			PageVersion:   res.page.Version.Number,
			PublishSHA256: sha,
		})
	}
}

// anyRecordable reports whether any result would produce a line, so the caller
// can skip building an index nothing will use.
func (rec *recorder) anyRecordable(results []result) bool {
	for i := range results {
		if _, ok := rec.recordable(&results[i]); ok {
			return true
		}
	}
	return false
}

// publishSHA renders one exported file the way update would and returns its
// publish sha, or false when it cannot be computed.
func (rec *recorder) publishSHA(
	c *client.ConfluenceClient, index *linkindex.Index, res result,
) (string, bool) {
	mf, err := frontmatter.ParseFile(res.destPath)
	if err != nil {
		return "", false
	}
	// SiteURL, not BaseURL, exactly as update and create do: rewritten links
	// are published into the page, so they must name the site even when
	// requests go through the gateway.
	spaceKey := client.SpaceKeyFromWebUI(res.page.Links.WebUI)
	page, err := convert.MdToConfluence(mf, rec.root, index, c.SiteURL(), spaceKey)
	if err != nil {
		return "", false
	}
	// The title update will send: the file's own, falling back to the live
	// page's the way update's resolveTitlePageID does.
	title := mf.Title()
	if title == "" {
		title = res.page.Title
	}
	return actionlog.Sum(title, page.HTML), true
}
