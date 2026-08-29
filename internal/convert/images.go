package convert

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/util"
)

// supportedImageExts are the extensions Confluence renders. A local image with
// any other extension is treated as broken.
var supportedImageExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".svg": true, ".webp": true, ".bmp": true,
}

var allowedAlign = map[string]bool{"left": true, "center": true, "right": true}

// renderImage rewrites an image to a Confluence <ac:image>. Remote images become
// an ri:url reference; a local image with a supported extension that exists
// becomes an ri:attachment (collected for upload, deduped by filename); a missing
// file or unsupported extension becomes literal "IMAGE BROKEN: ..." text.
func (r *storageRenderer) renderImage(
	w util.BufWriter, source []byte, node ast.Node, entering bool,
) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.Image)
	src := string(n.Destination)
	if src == "" {
		return ast.WalkSkipChildren, nil
	}
	alt := nodeText(node, source)
	attrs := r.parseImageTitle(string(n.Title), src)

	// Remote and unsupported-extension images are decided on src/fsPath alone
	// and never touch the filesystem -- checked and returned before any of the
	// root/Lstat work below, so a remote URL costs no syscalls just because
	// its "path" happens to parse.
	if isRemoteURL(src) {
		_, _ = w.WriteString(acImage(alt, attrs, "", src))
		return ast.WalkSkipChildren, nil
	}
	// src is a URL; fsPath is the file it names. Everything touching the
	// filesystem -- and the attachment name derived from it -- uses the path
	// relative to root, not fsPath itself. The broken messages stay on src so
	// they echo what the author wrote. Decoding before computing that relative
	// path is what keeps an encoded "..%2F" from slipping past the root check.
	fsPath := decodeDestination(src)
	if !supportedImageExts[strings.ToLower(filepath.Ext(fsPath))] {
		msg := fmt.Sprintf("IMAGE BROKEN: %s (unsupported type)", src)
		r.broken = append(r.broken, msg)
		_, _ = w.WriteString(html.EscapeString(msg))
		return ast.WalkSkipChildren, nil
	}

	rootRel, insideRoot := rootRelative(r.root.Dir, r.baseDir, fsPath)

	var info os.FileInfo
	var lstatErr error
	if insideRoot {
		info, lstatErr = r.root.FS.Lstat(rootRel)
	}
	// An escape only os.Root can see -- a symlinked intermediate directory --
	// is folded into the same "outside" case as a lexically escaping path,
	// wrapped the way internal/attachfile already wraps it: os.Root's bare
	// error names neither the image nor the reason. Every other boolean below
	// is guarded by insideRoot too, not just lstatErr == nil -- Lstat is never
	// called at all when insideRoot is false, so lstatErr and info both sit at
	// their zero values (nil), and "lstatErr == nil" alone would read as
	// "Lstat succeeded" when it really means "Lstat was never asked."
	escapesRoot := !insideRoot || (lstatErr != nil && strings.Contains(lstatErr.Error(), "escapes from parent"))
	notFound := insideRoot && lstatErr != nil && !escapesRoot
	isSymlink := insideRoot && lstatErr == nil && info.Mode()&os.ModeSymlink != 0
	notRegular := insideRoot && lstatErr == nil && !isSymlink && !info.Mode().IsRegular()

	switch {
	case escapesRoot:
		msg := fmt.Sprintf("IMAGE BROKEN: %s (outside the documentation root)", src)
		r.broken = append(r.broken, msg)
		_, _ = w.WriteString(html.EscapeString(msg))

	case notFound, notRegular:
		// Not found, or something that was never a file to begin with (a
		// directory, say) -- both read the same as "not found" always has.
		msg := fmt.Sprintf("IMAGE BROKEN: %s (not found)", src)
		r.broken = append(r.broken, msg)
		_, _ = w.WriteString(html.EscapeString(msg))

	case isSymlink:
		msg := fmt.Sprintf("IMAGE BROKEN: %s (symlink, not a regular file)", src)
		r.broken = append(r.broken, msg)
		_, _ = w.WriteString(html.EscapeString(msg))

	default:
		filename := AttachmentFilename(rootRel)
		if !r.seen[filename] {
			if r.seen == nil {
				r.seen = map[string]bool{}
			}
			r.seen[filename] = true
			r.attachments = append(r.attachments, Attachment{
				Filename: filename, Path: filepath.Join(r.root.Dir, rootRel), Source: rootRel,
			})
		}
		_, _ = w.WriteString(acImage(alt, attrs, filename, ""))
	}
	return ast.WalkSkipChildren, nil
}

// parseImageTitle turns a markdown image title into extra <ac:image> attributes.
// A JSON object supplies title/width/height/align; anything else becomes a plain
// tooltip (title). Invalid width/height/align values are dropped with a warning.
func (r *storageRenderer) parseImageTitle(titleRaw, src string) map[string]string {
	if titleRaw == "" {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(titleRaw), &data); err != nil {
		return map[string]string{"title": titleRaw}
	}

	attrs := map[string]string{}
	if s := toStr(data["title"]); s != "" {
		attrs["title"] = s
	}
	for _, dim := range []string{"width", "height"} {
		v, ok := data[dim]
		if !ok {
			continue
		}
		s := toStr(v)
		if s == "" {
			continue
		}
		if isDigits(s) {
			attrs[dim] = s
		} else {
			r.warnings = append(r.warnings,
				fmt.Sprintf("%s: ignoring %s=%s (must be a number)", src, dim, pyRepr(v)))
		}
	}
	if v, ok := data["align"]; ok {
		if s := toStr(v); s != "" {
			if allowedAlign[s] {
				attrs["align"] = s
			} else {
				r.warnings = append(r.warnings,
					fmt.Sprintf("%s: ignoring align=%s (must be left, center, or right)", src, pyRepr(v)))
			}
		}
	}
	return attrs
}

// acImage builds an <ac:image> referencing an attachment (riFilename) or a URL
// (riURL). Attribute values are XML-escaped.
func acImage(alt string, attrs map[string]string, riFilename, riURL string) string {
	var parts []string
	if alt != "" {
		parts = append(parts, fmt.Sprintf(`ac:alt="%s"`, html.EscapeString(alt)))
	}
	for _, key := range []string{"title", "width", "height", "align"} {
		if v := attrs[key]; v != "" {
			parts = append(parts, fmt.Sprintf(`ac:%s="%s"`, key, html.EscapeString(v)))
		}
	}
	leading := ""
	if len(parts) > 0 {
		leading = " " + strings.Join(parts, " ")
	}

	resource := fmt.Sprintf(`<ri:url ri:value="%s" />`, riURL)
	if riFilename != "" {
		resource = fmt.Sprintf(`<ri:attachment ri:filename="%s" />`, riFilename)
	}
	return fmt.Sprintf("<ac:image%s>%s</ac:image>", leading, resource)
}

// nodeText returns the concatenated plain text of a node's descendants.
func nodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			b.Write(t.Segment.Value(source))
		case *ast.String:
			b.Write(t.Value)
		}
		return ast.WalkContinue, nil
	})
	return b.String()
}

func isRemoteURL(src string) bool {
	return strings.HasPrefix(src, "http://") ||
		strings.HasPrefix(src, "https://") ||
		strings.HasPrefix(src, "//")
}

// rootRelative resolves an image destination (fsPath, already decoded and
// still relative to baseDir -- the referencing file's own directory) to a path
// relative to root, in slash form for recording as an attachment Source and
// for passing to root.FS. ok is false when the result climbs above root, which
// the caller reports as broken rather than ever asking root.FS about it.
//
// A path at or below root is fine, including one reached via ".." from a page
// in a subdirectory: "../assets/logo.png" from docs/guide/foo.md is the
// ordinary shared-assets layout, and resolves to "assets/logo.png" once
// docs/guide's ".." cancels out -- not to something climbing past root itself.
func rootRelative(root, baseDir, fsPath string) (rel string, ok bool) {
	abs, err := filepath.Abs(filepath.Join(baseDir, fsPath))
	if err != nil {
		return "", false
	}
	r, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	r = filepath.ToSlash(r)
	if r == ".." || strings.HasPrefix(r, "../") {
		return "", false
	}
	return r, true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// toStr renders a JSON value the way the reference implementation stringifies it:
// strings as-is, integral numbers without a decimal point.
func toStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// pyRepr renders a JSON value for a warning message: strings single-quoted,
// everything else via its default formatting.
func pyRepr(v any) string {
	if s, ok := v.(string); ok {
		return "'" + s + "'"
	}
	return toStr(v)
}
