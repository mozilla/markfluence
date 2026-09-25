// Package attachref defines the local-attachment reference shared by
// internal/convert (which discovers these while converting a page's Markdown)
// and internal/client (which uploads them). internal/convert is deliberately
// client-free, so this shape -- otherwise identical on both sides -- lives here
// instead of being owned by either.
package attachref

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

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

	// Root, when set, is the documentation root Source is relative to, and
	// Open reads through it (#186, S2). The converter sets it; a file named on
	// attachment-upload's command line has none, since no Markdown reference
	// chose it and --name lets it live anywhere.
	Root *os.Root `json:"-"`
}

// Open opens the file to upload. With a Root it opens Source through the
// root, the path the converter checked, so a directory on the way that became
// a symbolic link since the check cannot lead the read outside the root: the
// check and the read would otherwise be two separate lookups. Without one it
// opens Path.
//
// os.Root refuses an escape but follows a symbolic link that stays inside the
// root, and markfluence follows none (docs/design-principles.md, Symlinks): a
// link swapped in at the file's own name since the check would publish some
// other file in the project -- a .env, say -- under this attachment's name. So
// after opening, the name is looked at again through the root, and it must not
// be a link and must be the very file the handle holds.
//
// Whatever is opened must be a regular file, checked on the handle rather than
// the path, so the check describes the thing actually read. The open is
// non-blocking, so a FIFO put in the file's place is refused instead of
// hanging the upload until something writes to it.
func (a LocalAttachment) Open() (*os.File, error) {
	const flags = os.O_RDONLY | syscall.O_NONBLOCK
	var f *os.File
	var err error
	if a.Root != nil {
		f, err = a.Root.OpenFile(filepath.FromSlash(a.Source), flags, 0)
	} else {
		f, err = os.OpenFile(a.Path, flags, 0)
	}
	if err != nil {
		return nil, a.openError(err)
	}
	if err := a.check(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// check refuses a handle that is not a regular file, or, through a Root, a
// name that is a symbolic link or no longer names the file the handle holds.
func (a LocalAttachment) check(f *os.File) error {
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", a.Path)
	}
	if a.Root == nil {
		return nil
	}
	named, err := a.Root.Lstat(filepath.FromSlash(a.Source))
	switch {
	case err != nil:
		return a.openError(err)
	case named.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%s is a symbolic link, not a regular file", a.Path)
	case !os.SameFile(fi, named):
		return fmt.Errorf("%s changed while it was being opened", a.Path)
	}
	return nil
}

// openError reports an escape the way the converter does, since os.Root's own
// error names neither the file nor the reason and reads as a disk failure.
func (a LocalAttachment) openError(err error) error {
	if strings.Contains(err.Error(), "escapes from parent") {
		return fmt.Errorf("%s is outside the documentation root", a.Path)
	}
	return fmt.Errorf("opening %s: %w", a.Path, err)
}
