// Package read implements the `markfluence read` command: fetch a page and
// print its body to stdout.
package read

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/convert"
	"github.com/mozilla/markfluence/internal/frontmatter"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pagewidth"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// Output formats. markdown (the default) is the best-effort inverse of
// MdToConfluence; storage is the raw stored XHTML.
const (
	formatMarkdown = "markdown"
	formatStorage  = "storage"
)

var formatFlag string

// Cmd is the read command.
var Cmd = &cobra.Command{
	Use:   "read ARG",
	Short: "Fetch a Confluence page and print its body",
	Long: "Fetch a Confluence page and print its body to stdout.\n\n" +
		"ARG is a numeric page id or a Confluence page URL (the modern\n" +
		"/wiki/.../pages/<id>/... form or a legacy ?pageId=<id> URL).\n\n" +
		"The default markdown output carries title/page_id/space/page_width\n" +
		"frontmatter and is a best-effort inverse of what create/update publish.",
	Args: cobra.ExactArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&formatFlag, "format", formatMarkdown,
		"Output format: markdown (default) or storage")
}

func run(cmd *cobra.Command, args []string) error {
	if formatFlag != formatMarkdown && formatFlag != formatStorage {
		return fatalFail(fmt.Sprintf("unsupported --format %q (supported: %s, %s)",
			formatFlag, formatMarkdown, formatStorage), jsonout.CodeValidation)
	}

	pageID, err := parsePageID(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	cloudID, _ := cmd.Flags().GetString("cloud-id")
	envFile, _ := cmd.Flags().GetString("env-file")
	c, err := client.Resolve(client.Options{
		URL: url, Username: username, CloudID: cloudID, EnvFile: envFile,
	})
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeConfig)
	}

	page, err := c.GetPageBodyOrNil(pageID)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}
	if page == nil {
		return operationalFail(pageID, fmt.Errorf("page %s not found", pageID), jsonout.CodeNotFound)
	}
	if page.Body.Storage.Value == "" {
		return operationalFail(pageID, fmt.Errorf(
			"page %s has no readable body (it may be a folder or an unsupported content type)",
			pageID), jsonout.CodeValidation)
	}

	body := page.Body.Storage.Value
	if formatFlag == formatMarkdown {
		body, err = convert.StorageToMarkdown(page.Body.Storage.Value)
		if err != nil {
			return operationalFail(pageID, err, jsonout.CodeConvert)
		}
	}

	if ui.IsJSON() {
		env := jsonout.NewEnvelope("read", []any{buildResult(c, page, formatFlag, body)},
			map[string]int{"total": 1, "succeeded": 1, "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}

	if formatFlag == formatStorage {
		fmt.Println(body)
		return nil
	}
	fmt.Print(frontmatterBlock(c, page) + "\n" + body)
	return nil
}

// fatalFail reports a config/usage/pre-flight failure: a JSON error object on
// stderr under --json, else a human error line, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, "read", msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports an operational failure for the single target: under
// --json a results[0] entry {ok:false,error,code}, else a human error line,
// exiting 1.
func operationalFail(pageID string, err error, code jsonout.Code) error {
	if ui.IsJSON() {
		res := map[string]any{"ok": false, "page_id": pageID, "error": err.Error(), "code": code}
		env := jsonout.NewEnvelope("read", []any{res},
			map[string]int{"total": 1, "succeeded": 0, "failed": 1})
		_ = jsonout.Emit(os.Stdout, env)
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// frontmatterBlock builds the YAML frontmatter prefix for markdown output:
// title, space, parent, page_id, and (best-effort) page_width. parent is "null"
// for a top-level page, else the parent's page id (both free from the fetched
// page). A failed page_width read is tolerated -- the field is simply omitted
// rather than failing the read.
func frontmatterBlock(c *client.ConfluenceClient, page *client.Page) string {
	parent := page.ParentID
	if parent == "" {
		parent = "null"
	}
	width := ""
	if w, _, err := pagewidth.Read(c, page.ID); err == nil {
		width = string(w)
	}
	return renderFrontmatter(page.Title, client.SpaceKeyFromWebUI(page.Links.WebUI), parent, page.ID, width)
}

// renderFrontmatter assembles the frontmatter block from resolved field values,
// omitting space/parent/page_width when empty. UpdateField emits them in the
// canonical order and auto-quotes values as needed.
func renderFrontmatter(title, space, parent, pageID, width string) string {
	fm := ""
	fm = frontmatter.UpdateField(fm, "title", title, "")
	if space != "" {
		fm = frontmatter.UpdateField(fm, "space", space, "")
	}
	if parent != "" {
		fm = frontmatter.UpdateField(fm, "parent", parent, "")
	}
	fm = frontmatter.UpdateField(fm, "page_id", pageID, "")
	if width != "" {
		fm = frontmatter.UpdateField(fm, "page_width", width, "")
	}
	return fm
}

// pagePathRE matches the numeric id in a modern Confluence page URL path,
// e.g. /wiki/spaces/ENG/pages/123456/Some+Title (the trailing slug is optional).
var pagePathRE = regexp.MustCompile(`/pages/(\d+)(?:/|$)`)

// parsePageID resolves the CLI argument to a numeric page id: a bare numeric id,
// or a Confluence URL carrying the id in its path or a pageId query parameter.
func parsePageID(arg string) (string, error) {
	if isDigits(arg) {
		return arg, nil
	}
	if u, err := url.Parse(arg); err == nil && u.Host != "" {
		if id := u.Query().Get("pageId"); isDigits(id) {
			return id, nil
		}
		if m := pagePathRE.FindStringSubmatch(u.Path); m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("%q is not a numeric page id or a Confluence page URL", arg)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) == -1
}
