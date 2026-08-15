// Package cmd implements the markfluence command-line interface.
package cmd

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mozilla/markfluence/cmd/attachmentdownload"
	"github.com/mozilla/markfluence/cmd/attachmentlist"
	"github.com/mozilla/markfluence/cmd/attachmentupload"
	"github.com/mozilla/markfluence/cmd/children"
	"github.com/mozilla/markfluence/cmd/create"
	"github.com/mozilla/markfluence/cmd/export"
	"github.com/mozilla/markfluence/cmd/find"
	"github.com/mozilla/markfluence/cmd/fix"
	"github.com/mozilla/markfluence/cmd/info"
	"github.com/mozilla/markfluence/cmd/read"
	"github.com/mozilla/markfluence/cmd/schema"
	"github.com/mozilla/markfluence/cmd/search"
	"github.com/mozilla/markfluence/cmd/update"
	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	urlFlag      string
	usernameFlag string
	cloudIDFlag  string
	envFileFlag  string
	debugFlag    bool
	noColorFlag  bool
	jsonFlag     bool
)

var rootCmd = &cobra.Command{
	Use:   "markfluence",
	Short: "Publish markdown to Confluence",
	Long: "markfluence publishes and manipulates Confluence pages from markdown files.\n\n" +
		"Configuration resolves with the precedence flag > environment variable >\n" +
		".env file. The site URL (--url / CONFLUENCE_URL), username (--username /\n" +
		"CONFLUENCE_USERNAME), and cloud ID (--cloud-id / CONFLUENCE_CLOUD_ID) may be\n" +
		"set any of those ways; the API token (CONFLUENCE_TOKEN) comes only from the\n" +
		"environment or .env, never a flag.\n\n" +
		"Set the cloud ID to authenticate with a scoped API token, such as one issued\n" +
		"to a service account: those tokens are rejected against the site domain and\n" +
		"must go through Atlassian's api.atlassian.com gateway. Leave it unset for an\n" +
		"unscoped personal token. Find yours at\n" +
		"https://YOUR-SITE.atlassian.net/_edge/tenant_info -- it isn't a secret.",
	// --version prints the build stamp ("markfluence VERSION (SHA, DATE)"), the
	// same string the converter substitutes for the <!-- markfluence-version -->
	// token.
	Version: buildinfo.Stamp(),
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if noColorFlag {
			if err := os.Setenv("NO_COLOR", "1"); err != nil {
				return fmt.Errorf("setting NO_COLOR: %w", err)
			}
		}
		ui.SetDebug(debugFlag)
		ui.SetJSON(jsonFlag)
		client.SetRetryLogger(logRetry)
		return nil
	},
	// Bare `markfluence` prints help; subcommands carry the work.
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
	// Commands print their own diagnostics via internal/ui; silence cobra's
	// usage and error echoing so failures aren't printed twice.
	SilenceUsage:  true,
	SilenceErrors: true,
}

// Execute runs the root command, exiting non-zero on error. A failure a command
// already reported (a silent error) is not printed again and exits with its
// carried code (1 operational, 2 config/usage). Any other error is
// cobra-generated (bad args/flags): a usage error, printed as a human line or a
// JSON error object under --json, exiting 2.
func Execute() {
	// Detect --json before parsing so that even a flag-parse failure (which
	// short-circuits PersistentPreRunE, where SetJSON normally runs) is reported
	// as a JSON error object rather than a stray human line on stderr.
	if jsonRequested(os.Args[1:]) {
		ui.SetJSON(true)
	}
	if err := rootCmd.Execute(); err != nil {
		if ui.IsSilent(err) {
			os.Exit(ui.ExitCode(err))
		}
		if ui.IsJSON() {
			_ = jsonout.EmitError(os.Stderr, "", err.Error(), jsonout.CodeConfig)
		} else {
			ui.Error(err.Error())
		}
		os.Exit(2)
	}
}

// jsonRequested reports whether the raw args request --json (bare, or
// --json=true), independent of cobra parsing. --json=false is honored as off.
func jsonRequested(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--json":
			return true
		case strings.HasPrefix(a, "--json="):
			return strings.TrimPrefix(a, "--json=") != "false"
		}
	}
	return false
}

func init() {
	rootCmd.PersistentFlags().StringVar(&urlFlag, "url", "",
		"Confluence base URL (falls back to $CONFLUENCE_URL, then .env)")
	rootCmd.PersistentFlags().StringVar(&usernameFlag, "username", "",
		"Confluence username/email (falls back to $CONFLUENCE_USERNAME, then .env)")
	rootCmd.PersistentFlags().StringVar(&cloudIDFlag, "cloud-id", "",
		"Atlassian cloud ID; set to use a scoped API token via the api.atlassian.com "+
			"gateway (falls back to $CONFLUENCE_CLOUD_ID, then .env)")
	rootCmd.PersistentFlags().StringVar(&envFileFlag, "env-file", "",
		"Path to an env file to read (default: ./.env in the working directory)")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false,
		"Enable verbose debug output")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false,
		"Disable colored output")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false,
		"Emit machine-readable JSON to stdout instead of human output")
	rootCmd.PersistentFlags().SortFlags = false

	// The stamp already carries its own "markfluence v" prefix; print it verbatim
	// rather than cobra's default "markfluence version <...>" wrapper.
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	// Append a docs footer to every command's --help output. Subcommands inherit
	// the root's help template, so setting it once covers them all.
	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() +
		"\nMore documentation at: https://github.com/mozilla/markfluence\n")

	rootCmd.AddCommand(update.Cmd)
	rootCmd.AddCommand(create.Cmd)
	rootCmd.AddCommand(fix.Cmd)
	rootCmd.AddCommand(info.Cmd)
	rootCmd.AddCommand(read.Cmd)
	rootCmd.AddCommand(children.Cmd)
	rootCmd.AddCommand(find.Cmd)
	rootCmd.AddCommand(search.Cmd)
	rootCmd.AddCommand(attachmentlist.Cmd)
	rootCmd.AddCommand(attachmentupload.Cmd)
	rootCmd.AddCommand(attachmentdownload.Cmd)
	rootCmd.AddCommand(export.Cmd)
	rootCmd.AddCommand(schema.Cmd)
}

// logRetry renders a retry decision as a --debug line.
//
// internal/client produces no output of its own, so this is where a retry
// becomes visible. It matters because the alternative is silence: five attempts
// at the 120-second upload timeout plus backoff is roughly twelve minutes with
// nothing on screen, and a decision *not* to retry is exactly as worth seeing as
// a decision to.
func logRetry(ev client.RetryEvent) {
	if !ui.IsDebug() {
		return
	}
	var b strings.Builder
	if ev.Note != "" {
		fmt.Fprintf(&b, "%s %s: %s (after: %v)", ev.Method, ev.URL, ev.Note, ev.Err)
		ui.Debug(b.String())
		return
	}
	fmt.Fprintf(&b, "%s %s: attempt %d ", ev.Method, ev.URL, ev.Attempt+1)
	switch {
	case ev.Err != nil:
		fmt.Fprintf(&b, "failed: %v", ev.Err)
	default:
		fmt.Fprintf(&b, "got HTTP %d", ev.Status)
	}
	if ev.Retrying {
		fmt.Fprintf(&b, "; retrying in %s", ev.Delay.Round(time.Millisecond))
	} else {
		b.WriteString("; not retrying")
	}
	if r := ev.RateLimit; !r.Empty() {
		fmt.Fprintf(&b, " [rate limit: limit=%s remaining=%s reset=%s near=%s reason=%s]",
			orDash(r.Limit), orDash(r.Remaining), orDash(r.Reset), orDash(r.NearLimit), orDash(r.Reason))
	}
	ui.Debug(b.String())
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
