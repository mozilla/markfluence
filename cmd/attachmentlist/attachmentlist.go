// Package attachmentlist implements the `markfluence attachment-list` command:
// list a page's attachments.
package attachmentlist

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/pageref"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

// command is the name used in help and as the --json command discriminator.
const command = "attachment-list"

// Cmd is the attachment-list command.
var Cmd = &cobra.Command{
	Use:   command + " PAGE",
	Short: "List a Confluence page's attachments",
	Long: "List a Confluence page's attachments.\n\n" +
		"PAGE is a numeric page id, a Confluence page URL, or a markdown file\n" +
		"whose frontmatter has a page_id.\n\n" +
		"The NAME column is the name Confluence stores, which is what\n" +
		"attachment-download takes. For an image markfluence published that is\n" +
		"the encoded source path, and the SOURCE column shows the markdown\n" +
		"image path it came from.\n\n" +
		"SOURCE is a dash when no source path is recorded: the attachment was\n" +
		"uploaded by hand, or it was published before markfluence recorded one.\n" +
		"Use --json, whose managed field tells those two apart.",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completion.MarkdownFiles,
	RunE:              run,
}

func run(cmd *cobra.Command, args []string) error {
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

	pageID, err := pageref.Resolve(args[0])
	if err != nil {
		return fatalFail(err.Error(), jsonout.CodeValidation)
	}

	attachments, err := c.ListAttachments(pageID)
	if err != nil {
		return operationalFail(pageID, err, jsonout.CodeFor(err))
	}

	if ui.IsJSON() {
		results := make([]any, 0, len(attachments))
		for _, a := range attachments {
			results = append(results, buildResult(a))
		}
		env := jsonout.NewEnvelope(command, results,
			map[string]int{"total": len(attachments), "succeeded": len(attachments), "failed": 0})
		return jsonout.Emit(os.Stdout, env)
	}

	if len(attachments) == 0 {
		ui.Info("No attachments.")
		return nil
	}
	fmt.Println(table(attachments))
	return nil
}

// fatalFail reports a config/usage/pre-flight failure: a JSON error object on
// stderr under --json, else a human error line, exiting 2.
func fatalFail(msg string, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.EmitError(os.Stderr, command, msg, code)
	} else {
		ui.Error(msg)
	}
	return ui.SilentExit(2)
}

// operationalFail reports an operational failure for the page: under --json a
// results[0] entry {ok:false,error,code}, else a human error line, exiting 1.
func operationalFail(pageID string, err error, code jsonout.Code) error {
	if ui.IsJSON() {
		_ = jsonout.Emit(os.Stdout, failEnvelope(pageID, err, code))
	} else {
		ui.Error(err.Error())
	}
	return ui.SilentExit(1)
}

// failEnvelope is the document operationalFail writes, split out so the schema
// conformance test can validate the envelope this command really emits instead
// of a hand-copied duplicate of it.
func failEnvelope(pageID string, err error, code jsonout.Code) jsonout.Envelope {
	return jsonout.NewEnvelope(command, []any{jsonout.NewSingleOpFailure(pageID, err, code)},
		map[string]int{"total": 1, "succeeded": 0, "failed": 1})
}

// table renders the attachments as aligned columns. Widths are measured over the
// rows rather than fixed, so a page of short names doesn't print a sparse table.
func table(attachments []client.Attachment) string {
	rows := make([][5]string, 0, len(attachments)+1)
	rows = append(rows, [5]string{"NAME", "SIZE", "VER", "TYPE", "SOURCE"})
	for _, a := range attachments {
		source := "-"
		if m := a.Meta(); m.Source != "" {
			source = m.Source
		}
		rows = append(rows, [5]string{
			a.Title,
			humanSize(a.Extensions.FileSize),
			strconv.Itoa(a.Version.Number),
			a.Extensions.MediaType,
			source,
		})
	}

	var widths [5]int
	for _, r := range rows {
		for i, cell := range r {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, cell := range r {
			if j > 0 {
				b.WriteString("  ")
			}
			// The last column needs no padding, which also avoids trailing spaces.
			if j == len(r)-1 {
				b.WriteString(cell)
				continue
			}
			pad := widths[j] - len([]rune(cell))
			// SIZE and VER are numeric, so right-align them.
			if j == 1 || j == 2 {
				b.WriteString(strings.Repeat(" ", pad) + cell)
			} else {
				b.WriteString(cell + strings.Repeat(" ", pad))
			}
		}
	}
	return b.String()
}

// humanSize renders a byte count in the largest unit that keeps it under 1024,
// with one decimal place above bytes.
func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit && exp < 3; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}
