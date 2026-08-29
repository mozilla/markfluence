// Package attachref defines the local-attachment reference shared by
// internal/convert (which discovers these while converting a page's markdown)
// and internal/client (which uploads them). internal/convert is deliberately
// client-free, so this shape -- otherwise identical on both sides -- lives here
// instead of being owned by either.
package attachref

// LocalAttachment is a local file to be uploaded as one page's attachment.
// Path is absolute. Filename is the attachment name, a bijective encoding of
// Source, so distinct images can never collide on one name. Source is the
// normalized page-relative path the image was written as, recorded on the
// attachment so a later read recovers it exactly rather than inferring it.
type LocalAttachment struct {
	Filename string `json:"filename"`
	Path     string `json:"path"`
	Source   string `json:"source"`
}
