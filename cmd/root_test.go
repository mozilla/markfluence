package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/client"
	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
	"github.com/mozilla/markfluence/internal/ui"
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

// TestSchemaCommandRegistered checks the one registration
// TestCommandEnumMatchesRegisteredCommands can't: every other subcommand's
// registration is implied by its presence in the schema's command enum (that
// test's second loop), but "schema" is deliberately exempt from the enum, so
// nothing else would catch its command being dropped entirely.
func TestSchemaCommandRegistered(t *testing.T) {
	for _, c := range rootCmd.Commands() {
		if c.Name() == "schema" {
			return
		}
	}
	t.Error(`subcommand "schema" not registered`)
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

// TestSecurityWarnerIsWired pins the one line that makes the .env permission
// warning exist at runtime. Everything else about it is tested in
// internal/client (the predicate) and internal/ui (the output), each against
// its own double -- so deleting the SetSecurityWarner call in
// PersistentPreRunE would leave every one of those tests passing and the
// feature silently gone. A retry log going quiet is a debugging annoyance; a
// security warning going quiet is the feature not existing.
func TestSecurityWarnerIsWired(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	body := "CONFLUENCE_URL=https://wiki\nCONFLUENCE_USERNAME=bot\nCONFLUENCE_TOKEN=secret\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.SetSecurityWarner(nil) })

	if err := rootCmd.PersistentPreRunE(rootCmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	_, resolveErr := client.Resolve(client.ResolveOptions{EnvFile: path})
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if resolveErr != nil {
		t.Fatalf("Resolve: %v", resolveErr)
	}
	if !strings.Contains(string(out), "holds your API token") {
		t.Errorf("stderr = %q, want the .env permission warning: is SetSecurityWarner still wired?", out)
	}
}

// TestSecurityWarningUnderJSONStaysOffStderr is the regression this design
// exists for. Under --json, stderr is itself a schema-validated document
// (#/$defs/errorObject, asserted in cmd/children's own tests), so a
// human-readable warning line printed ahead of it would break any consumer
// that parses stderr -- and the schema invites exactly that. The warning has
// to travel inside the documents instead.
func TestSecurityWarningUnderJSONStaysOffStderr(t *testing.T) {
	jsonout.ResetWarnings()
	t.Cleanup(jsonout.ResetWarnings)
	ui.SetJSON(true)
	t.Cleanup(func() { ui.SetJSON(false) })

	const msg = "/tmp/.env is readable by others (mode 0644) and holds your API token; run: chmod 600 /tmp/.env"

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	reportSecurityWarning(msg)
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(printed) != 0 {
		t.Errorf("stderr = %q under --json, want nothing: it would precede the error object", printed)
	}

	// Both documents carry it, and both still validate.
	var buf bytes.Buffer
	if err := jsonout.EmitError(&buf, "read", "missing Confluence username", jsonout.CodeConfig); err != nil {
		t.Fatalf("EmitError: %v", err)
	}
	schematest.ValidateError(t, buf.Bytes())
	if !strings.Contains(buf.String(), "holds your API token") {
		t.Errorf("error object = %s, want the warning carried in it", buf.String())
	}

	buf.Reset()
	env := jsonout.NewEnvelope("read", nil, map[string]int{"total": 0})
	if err := jsonout.Emit(&buf, env); err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if !strings.Contains(buf.String(), "holds your API token") {
		t.Errorf("envelope = %s, want the warning carried in it", buf.String())
	}
}

// TestSecurityWarningInHumanModeGoesToStderr: the other half. Nothing structured
// is emitted in human mode, so the line itself is the whole delivery.
func TestSecurityWarningInHumanModeGoesToStderr(t *testing.T) {
	jsonout.ResetWarnings()
	t.Cleanup(jsonout.ResetWarnings)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	reportSecurityWarning("mind the mode")
	os.Stderr = old
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	printed, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(printed), "mind the mode") {
		t.Errorf("stderr = %q, want the warning", printed)
	}
}
