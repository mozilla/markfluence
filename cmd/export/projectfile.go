package export

// The markfluence.yaml an exported tree needs to be republishable.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mozilla/markfluence/internal/project"
)

// projectFileBody is what markfluence.yaml holds. Its existence is its whole
// meaning -- nothing in it is parsed -- so it carries a comment saying so, for
// whoever finds it and wonders (_plans/025).
const projectFileBody = `# Marks the root of a markfluence project. Image and link paths are recorded
# relative to this directory. https://github.com/mozilla/markfluence
`

// Outcomes for the marker file, reported like any other write.
const (
	markerWrote   = statusWrote
	markerExists  = "exists"
	markerSkipped = "" // not a multi-page export, so none is needed
)

// writeProjectFile plants markfluence.yaml at the destination of a multi-page
// export, and reports what it did.
//
// It is load-bearing rather than tidy. Without a project file the documentation
// root falls back to a markdown file's own directory, so dest/home/child.md
// would take dest/home/ as its root -- and a shared asset reconstructed at
// dest/assets/brand.png then sits above that root and republishes as
// IMAGE BROKEN. A single-page export needs none: its file is at dest, so the
// fallback root is already dest.
//
// Written before the first page, not after the last. A run that dies partway is
// the case the resume behaviour exists for, and a partial tree with no marker
// is a tree whose every shared asset republishes broken.
//
// Never overwritten (S3): an existing file is somebody's project, and this one
// has nothing in it worth replacing anyway.
func writeProjectFile(root string, multiPage bool) (string, error) {
	if !multiPage {
		return markerSkipped, nil
	}
	path := filepath.Join(root, project.Filename)
	if _, err := os.Stat(path); err == nil {
		return markerExists, nil
	}
	if dryRun {
		return markerWrote, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(projectFileBody), 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return markerWrote, nil
}
