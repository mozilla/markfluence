// Package attachref defines the local-attachment reference shared by
// internal/convert (which discovers these while converting a page's markdown)
// and internal/client (which uploads them). internal/convert is deliberately
// client-free, so this shape -- otherwise identical on both sides -- lives here
// instead of being owned by either.
package attachref

// LocalAttachment is a local file to be uploaded as one page's attachment.
// Path is absolute. Filename is the attachment name, which is Source's base
// name (convert.AttachmentFilename). Source is the normalized root-relative
// path the image was written as, recorded on the attachment so a later read
// recovers it exactly rather than inferring it.
//
// The mapping from Source to Filename is lossy on purpose, and the loss is
// checked rather than encoded around: two images whose base names agree cannot
// both be attached to one page, and whoever builds these refuses that rather
// than letting one overwrite the other. Filename used to be a bijective
// encoding of Source, which made a name move whenever a path moved -- and a
// moved name is a new attachment with the old one orphaned (_plans/029).
type LocalAttachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Source   string `json:"source"`
}
