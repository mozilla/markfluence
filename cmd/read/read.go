// Package read implements the `markfluence read` command: fetch a page and
// print its body to stdout.
package read

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// formatStorage is the only output format supported today. The flag exists for
// forward-compat (markdown/view/text are planned); see _plans/read-subcommand.md.
const formatStorage = "storage"

var formatFlag string

// Cmd is the read command.
var Cmd = &cobra.Command{
	Use:   "read ARG",
	Short: "Fetch a Confluence page and print its body",
	Long: "Fetch a Confluence page and print its body to stdout.\n\n" +
		"ARG is a numeric page id or a Confluence page URL (the modern\n" +
		"/wiki/.../pages/<id>/... form or a legacy ?pageId=<id> URL).",
	Args: cobra.ExactArgs(1),
	RunE: run,
}

func init() {
	Cmd.Flags().StringVar(&formatFlag, "format", formatStorage,
		"Output format (supported: storage)")
}

func run(cmd *cobra.Command, args []string) error {
	if formatFlag != formatStorage {
		ui.Error(fmt.Sprintf("unsupported --format %q (supported: %s)", formatFlag, formatStorage))
		return ui.ErrSilent
	}

	pageID, err := parsePageID(args[0])
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}

	url, _ := cmd.Flags().GetString("url")
	username, _ := cmd.Flags().GetString("username")
	c, err := client.Resolve(url, username)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}

	page, err := c.GetPageBodyOrNil(pageID)
	if err != nil {
		ui.Error(err.Error())
		return ui.ErrSilent
	}
	if page == nil {
		ui.Error(fmt.Sprintf("page %s not found", pageID))
		return ui.ErrSilent
	}
	if page.Body.Storage.Value == "" {
		ui.Error(fmt.Sprintf(
			"page %s has no readable body (it may be a folder or an unsupported content type)",
			pageID))
		return ui.ErrSilent
	}

	fmt.Println(page.Body.Storage.Value)
	return nil
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
