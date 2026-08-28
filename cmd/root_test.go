package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/schematest"
)

// TestRootCommandWiring is the step-1 smoke test: it confirms the root command
// is registered with the expected name and persistent flags. It grows real
// coverage as subcommands land.
func TestRootCommandWiring(t *testing.T) {
	if rootCmd.Use != "markfluence" {
		t.Errorf("rootCmd.Use = %q, want %q", rootCmd.Use, "markfluence")
	}
	for _, flag := range []string{"url", "debug", "no-color", "json", "env-file", "root"} {
		if rootCmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("persistent flag --%s not registered", flag)
		}
	}
}

func TestSubcommandsRegistered(t *testing.T) {
	want := map[string]bool{
		"update": false, "create": false, "fix": false, "info": false, "read": false, "schema": false,
	}
	for _, c := range rootCmd.Commands() {
		delete(want, c.Name())
	}
	for name := range want {
		t.Errorf("subcommand %q not registered", name)
	}
}

// completionOut collects the generated completion scripts; see
// TestCompletionScripts for why it outlives the test.
var completionOut bytes.Buffer

// TestCompletionScripts pins cobra's generated `completion` command as part of
// the CLI: the release runs it to produce the scripts packaged in the archives
// and installed by the Homebrew cask, so turning it off would ship empty files.
func TestCompletionScripts(t *testing.T) {
	// completionOut is where every shell's script lands. Cobra binds the
	// completion command's writer once, when it builds that command on the
	// first Execute, so a buffer scoped to the shell -- or even to the test
	// function, which `go test -count=2` runs twice -- would be written to only
	// the first time and read as empty after that.
	out := &completionOut
	rootCmd.SetOut(out)
	rootCmd.SetErr(out)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		t.Run(shell, func(t *testing.T) {
			out.Reset()
			rootCmd.SetArgs([]string{"completion", shell})
			if err := rootCmd.Execute(); err != nil {
				t.Fatalf("completion %s: %v", shell, err)
			}
			// Every generated script asks the binary itself for candidates
			// through the hidden __complete command; a script naming that and
			// this CLI is a real one rather than a stub.
			script := out.String()
			if !strings.Contains(script, "__complete") || !strings.Contains(script, "markfluence") {
				t.Errorf("%s completion script looks empty or generic:\n%s", shell, script)
			}
		})
	}
}

// noJSONEnvelope lists the subcommands that emit no --json envelope, and so
// have no business in the schema's command enum. Keeping it explicit means
// adding a command that reports nothing machine-readable is a deliberate entry
// here rather than a silent omission from the contract.
var noJSONEnvelope = map[string]string{
	"help":       "cobra's own; prints help text",
	"completion": "cobra's own; prints a shell script",
	"schema":     "prints the schema document itself, not an envelope",
}

// TestCommandEnumMatchesRegisteredCommands ties the CLI's command list to the
// --json contract in both directions. Without it, a new command's only nudge
// toward the schema is its own conformance test -- which a developer can satisfy
// by adding the name to the command enum and stopping, leaving its results
// unvalidated (see internal/schematest/document.go).
func TestCommandEnumMatchesRegisteredCommands(t *testing.T) {
	rootCmd.InitDefaultCompletionCmd()

	inEnum := make(map[string]bool)
	for _, name := range schematest.Commands(t) {
		inEnum[name] = true
	}

	registered := make(map[string]bool)
	for _, c := range rootCmd.Commands() {
		name := c.Name()
		registered[name] = true
		if _, exempt := noJSONEnvelope[name]; exempt {
			if inEnum[name] {
				t.Errorf("command %q is listed as emitting no JSON envelope but is in the "+
					"schema's command enum", name)
			}
			continue
		}
		if !inEnum[name] {
			t.Errorf("command %q is not in the schema's command enum: add it (with an "+
				"if/then branch for its results and summary), or list it in noJSONEnvelope",
				name)
		}
	}

	for name := range inEnum {
		if !registered[name] {
			t.Errorf("schema's command enum lists %q, which is not a registered command", name)
		}
	}
}

// TestSubcommandsCompleteArgs requires every subcommand to say what its
// arguments are, so a newly added one doesn't quietly fall back to completing
// every file in the directory.
func TestSubcommandsCompleteArgs(t *testing.T) {
	rootCmd.InitDefaultCompletionCmd()
	for _, c := range rootCmd.Commands() {
		// Cobra's own commands complete their own arguments.
		if c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		if c.ValidArgsFunction == nil && len(c.ValidArgs) == 0 {
			t.Errorf("subcommand %q registers no argument completion", c.Name())
		}
	}
}
