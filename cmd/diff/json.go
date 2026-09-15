package diff

// The --json document. Both streams' worth of content goes in the payload:
// stderr under --json is a schema-validated document with no room for a stray
// human line.

import (
	"github.com/mozilla/markfluence/internal/pagemeta"
)

// diffResult is the --json result for one file.
//
// Every field is on this struct and nothing uses omitempty, so every one always
// marshals and the schema's additionalProperties:false / required catch an
// added, renamed or removed field whatever a fixture sets.
type diffResult struct {
	OK   bool   `json:"ok"`
	File string `json:"file"`
	// PageID, Title and URL describe the page the file was compared against.
	PageID string `json:"page_id"`
	Title  string `json:"title"`
	URL    string `json:"url"`
	// MetadataSource is where the file's metadata came from, as everywhere
	// else: frontmatter, manifest, or null.
	MetadataSource *string `json:"metadata_source"`
	// Differs is the answer the exit code carries, so a consumer need not read
	// one. False when only an uncomparable field was reported.
	Differs bool `json:"differs"`
	// BodyDiffers says whether there is a patch, which is not the same
	// question: a file whose title alone changed differs with an empty diff.
	BodyDiffers bool `json:"body_differs"`
	// Added and Removed count changed lines in Diff.
	Added   int `json:"added"`
	Removed int `json:"removed"`
	// Diff is the unified diff, always uncoloured -- the bytes the human path
	// would print with NO_COLOR set -- and "" when the bodies agree.
	Diff string `json:"diff"`
	// Frontmatter is the per-field report, only the fields that differ, in
	// frontmatter's canonical order. Initialized rather than left nil so it
	// marshals as [] and never as null.
	Frontmatter []frontmatterDifference `json:"frontmatter"`
	// Warnings are this file's: a soft metadata disagreement, or a field that
	// could not be compared. The envelope's own warnings are about the
	// invocation instead.
	Warnings []string `json:"warnings"`
}

// frontmatterDifference is one field the file and the page disagree about.
type frontmatterDifference struct {
	Field string `json:"field"`
	// Confluence is null when the page's side could not be read, which is the
	// one case Comparable is false.
	Confluence *string `json:"confluence"`
	Local      string  `json:"local"`
	// Source names the location that supplied the local value, so a consumer
	// reporting the difference can name the file to edit.
	Source string `json:"source"`
	// Comparable is false for a field whose page-side value could not be
	// fetched. Such a field is reported and does not set Differs: nobody asked
	// the page, so nobody may claim it disagrees.
	Comparable bool `json:"comparable"`
	// Note explains a comparison that is not a plain string match, or "".
	Note string `json:"note"`
}

// jsonResult builds the result document.
func jsonResult(r result) diffResult {
	out := diffResult{
		OK:             true,
		File:           r.file,
		PageID:         r.page.ID,
		Title:          r.page.Title,
		URL:            r.url,
		MetadataSource: metadataSource(r.meta),
		Differs:        r.differs(),
		BodyDiffers:    r.body.Differs(),
		Added:          r.body.Added,
		Removed:        r.body.Removed,
		Diff:           r.body.Text,
		Frontmatter:    []frontmatterDifference{},
		Warnings:       []string{},
	}
	for _, d := range r.fields {
		entry := frontmatterDifference{
			Field: d.Field, Local: d.Local, Source: d.Source,
			Comparable: d.Comparable, Note: d.Note,
		}
		if d.Comparable {
			v := d.Confluence
			entry.Confluence = &v
		}
		out.Frontmatter = append(out.Frontmatter, entry)
	}
	out.Warnings = append(out.Warnings, r.warnings...)
	return out
}

// metadataSource is the nullable metadata_source every file-oriented command
// reports. Unmanaged is impossible here -- a file naming no page never reaches
// a comparison -- but the field stays nullable so the shape matches update's.
func metadataSource(meta pagemeta.Resolved) *string {
	s := string(meta.MetadataSource())
	if s == "" {
		return nil
	}
	return &s
}
