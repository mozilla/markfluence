// Package cmd implements the markfluence command-line interface.
package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/mozilla/markfluence/cmd/create"
	"github.com/mozilla/markfluence/cmd/fix"
	"github.com/mozilla/markfluence/cmd/info"
	"github.com/mozilla/markfluence/cmd/read"
	"github.com/mozilla/markfluence/cmd/update"
	"github.com/mozilla/markfluence/internal/buildinfo"
	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	urlFlag      string
	usernameFlag string
	envFileFlag  string
	debugFlag    bool
	noColorFlag  bool
)

var rootCmd = &cobra.Command{
	Use:   "markfluence",
	Short: "Publish markdown to Confluence",
	Long: "markfluence publishes and manipulates Confluence pages from markdown files.\n\n" +
		"Configuration resolves with the precedence flag > environment variable >\n" +
		".env file. The base URL (--url / CONFLUENCE_URL) and username (--username /\n" +
		"CONFLUENCE_USERNAME) may be set any of those ways; the API token\n" +
		"(CONFLUENCE_TOKEN) comes only from the environment or .env, never a flag.",
	Version: buildinfo.Version,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if noColorFlag {
			if err := os.Setenv("NO_COLOR", "1"); err != nil {
				return fmt.Errorf("setting NO_COLOR: %w", err)
			}
		}
		ui.SetDebug(debugFlag)
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

// Execute runs the root command, exiting non-zero on error. Errors a command
// already reported (ui.ErrSilent) are not printed again; any other error
// reaching here is cobra-generated (e.g. bad args/flags) and is printed.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		if !errors.Is(err, ui.ErrSilent) {
			ui.Error(err.Error())
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&urlFlag, "url", "",
		"Confluence base URL (falls back to $CONFLUENCE_URL, then .env)")
	rootCmd.PersistentFlags().StringVar(&usernameFlag, "username", "",
		"Confluence username/email (falls back to $CONFLUENCE_USERNAME, then .env)")
	rootCmd.PersistentFlags().StringVar(&envFileFlag, "env-file", "",
		"Path to an env file to read (default: ./.env in the working directory)")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false,
		"Enable verbose debug output")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false,
		"Disable colored output")
	rootCmd.PersistentFlags().SortFlags = false

	// Append a docs footer to every command's --help output. Subcommands inherit
	// the root's help template, so setting it once covers them all.
	rootCmd.SetHelpTemplate(rootCmd.HelpTemplate() +
		"\nMore documentation at: https://github.com/mozilla/markfluence\n")

	rootCmd.AddCommand(update.Cmd)
	rootCmd.AddCommand(create.Cmd)
	rootCmd.AddCommand(fix.Cmd)
	rootCmd.AddCommand(info.Cmd)
	rootCmd.AddCommand(read.Cmd)
}
