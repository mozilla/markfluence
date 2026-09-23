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
	"github.com/mozilla/markfluence/cmd/check"
	"github.com/mozilla/markfluence/cmd/children"
	"github.com/mozilla/markfluence/cmd/create"
	"github.com/mozilla/markfluence/cmd/diff"
	"github.com/mozilla/markfluence/cmd/export"
	"github.com/mozilla/markfluence/cmd/find"
	"github.com/mozilla/markfluence/cmd/pageinfo"
	"github.com/mozilla/markfluence/cmd/read"
	"github.com/mozilla/markfluence/cmd/schema"
	"github.com/mozilla/markfluence/cmd/search"
	"github.com/mozilla/markfluence/cmd/spaceinfo"
	"github.com/mozilla/markfluence/cmd/update"
	"github.com/mozilla/markfluence/cmd/userfind"
	"github.com/mozilla/markfluence/cmd/userinfo"
	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/completion"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	envFileFlag string
	rootFlag    string
	debugFlag   bool
	noColorFlag bool
	jsonFlag    bool
)

var rootCmd = &cobra.Command{
	Use:   "markfluence",
	Short: "Publish Markdown to Confluence",
	Long: "markfluence publishes and manipulates Confluence pages from Markdown files.\n\n" +
		"It needs a site URL, a username, and an API token, and for a scoped token a\n" +
		"cloud ID:\n\n" +
		"  CONFLUENCE_URL        the site, such as https://YOUR-SITE.atlassian.net\n" +
		"  CONFLUENCE_USERNAME   your email address\n" +
		"  CONFLUENCE_TOKEN      your API token\n" +
		"  CONFLUENCE_CLOUD_ID   optional; only for a scoped API token\n\n" +
		"markfluence reads each one from these places, and uses the first it finds:\n\n" +
		"  1. the file that --env-file names\n" +
		"  2. the environment variable\n" +
		"  3. your credentials file, ~/.config/markfluence/credentials\n" +
		"     ($XDG_CONFIG_HOME/markfluence/credentials if you set XDG_CONFIG_HOME)\n\n" +
		"The two files hold KEY=value lines. Put the URL and the token in the same\n" +
		"place: markfluence refuses to send a token to a URL from a different place.\n" +
		"It reads the cloud ID only from the place that gives the URL.\n\n" +
		"There is no flag for any of these, so the token cannot get into your shell\n" +
		"history. To use a different site for one command, name a file with\n" +
		"--env-file.\n\n" +
		"Set the cloud ID only for a scoped API token, such as the token of a service\n" +
		"account. Confluence refuses a scoped token at your site URL, so markfluence\n" +
		"sends it through the api.atlassian.com gateway, which needs the cloud ID. To\n" +
		"find your cloud ID, open https://YOUR-SITE.atlassian.net/_edge/tenant_info .\n" +
		"The cloud ID is not a secret.",
	// --version prints the build stamp ("markfluence VERSION (SHA, DATE)"). The
	// only use of it: nothing published carries a build stamp, and the converter
	// takes no build state at all.
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
		client.SetSecurityWarner(reportSecurityWarning)
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

// reportSecurityWarning delivers a credential-hygiene warning to both output
// modes: a human sees it immediately on stderr, and --json carries it in the
// documents rather than printing it, because stderr under --json is itself a
// schema-validated document (#/$defs/errorObject) -- a stray human line ahead
// of it would break a consumer that parses stderr, which the schema invites.
//
// Both, not either: the warning is raised during credential resolution, before
// anything knows whether this run will emit an envelope, an error object, or
// (on a --dry-run of nothing) neither.
func reportSecurityWarning(msg string) {
	jsonout.AddWarning(msg)
	ui.Warn(msg)
}

// Execute runs the root command, exiting non-zero on error. A failure a command
// already reported (a silent error) is not printed again and exits with its
// carried code (1 operational, 2 config/usage). Any other error is
// cobra-generated (bad args/flags): a usage error, printed as a human line or a
// JSON error object under --json, exiting 2.
// Root returns the root command, for tooling that needs to walk the command
// tree rather than run it -- currently only the docs generator, which renders
// every command's --help into docs/commands/.
//
// Exported for that one caller rather than left unexported with the generator
// living inside this package: a main() in here would be built into the binary,
// and a _test.go that writes files into the repo is not a test.
func Root() *cobra.Command { return rootCmd }

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
	rootCmd.PersistentFlags().StringVar(&envFileFlag, "env-file", "",
		"File to read credentials from, before the environment and your credentials "+
			"file")
	rootCmd.PersistentFlags().StringVar(&rootFlag, "root", "",
		"Documentation root for every file. The default is the nearest directory "+
			"above each file that has a markfluence.yaml, or the directory of the file "+
			"if there is none")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false,
		"Print debug details, such as each retry decision")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false,
		"Print output with no color")
	rootCmd.PersistentFlags().BoolVar(&jsonFlag, "json", false,
		"Write JSON, and no human output. A result goes to stdout. A fatal error goes "+
			"to stderr as a JSON error object")
	rootCmd.PersistentFlags().SortFlags = false
	completion.RegisterFlag(rootCmd, "root", completion.Directories)

	// The stamp already carries its own "markfluence v" prefix; print it verbatim
	// rather than cobra's default "markfluence version <...>" wrapper.
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	// Append a docs footer to every command's --help output. Subcommands inherit
	// the root's help template, so setting it once covers them all.
	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() +
		"\nMore documentation at: https://github.com/mozilla/markfluence\n")

	rootCmd.AddCommand(update.Cmd)
	rootCmd.AddCommand(create.Cmd)
	rootCmd.AddCommand(check.Cmd)
	rootCmd.AddCommand(diff.Cmd)
	rootCmd.AddCommand(pageinfo.Cmd)
	rootCmd.AddCommand(spaceinfo.Cmd)
	rootCmd.AddCommand(read.Cmd)
	rootCmd.AddCommand(children.Cmd)
	rootCmd.AddCommand(find.Cmd)
	rootCmd.AddCommand(search.Cmd)
	rootCmd.AddCommand(userfind.Cmd)
	rootCmd.AddCommand(userinfo.Cmd)
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
