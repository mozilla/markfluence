// Package cmd implements the markfluence command-line interface.
package cmd

import (
	"fmt"
	"os"

	"github.com/mozilla/markfluence/internal/ui"
	"github.com/spf13/cobra"
)

var (
	urlFlag     string
	debugFlag   bool
	noColorFlag bool

	// Version is set at build time via -ldflags.
	Version = "dev"
)

var rootCmd = &cobra.Command{
	Use:   "markfluence",
	Short: "Publish markdown to Confluence",
	Long: "markfluence publishes and manipulates Confluence pages from markdown files.\n\n" +
		"Set the base URL with --url. Credentials come from the CONFLUENCE_AUTH\n" +
		"environment variable, formatted as username:token (split on the first ':').",
	Version: Version,
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if noColorFlag {
			if err := os.Setenv("NO_COLOR", "1"); err != nil {
				return fmt.Errorf("setting NO_COLOR: %w", err)
			}
		}
		ui.SetDebug(debugFlag)
		return nil
	},
	// Bare `markfluence` prints help; subcommands (added later) carry the work.
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
	SilenceUsage: true,
}

// Execute runs the root command, exiting non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&urlFlag, "url", "",
		"Confluence base URL (e.g. https://example.atlassian.net)")
	rootCmd.PersistentFlags().BoolVarP(&debugFlag, "debug", "d", false,
		"Enable verbose debug output")
	rootCmd.PersistentFlags().BoolVar(&noColorFlag, "no-color", false,
		"Disable colored output")
	rootCmd.PersistentFlags().SortFlags = false
}
