package cmd

import "testing"

// TestRootCommandWiring is the step-1 smoke test: it confirms the root command
// is registered with the expected name and persistent flags. It grows real
// coverage as subcommands land.
func TestRootCommandWiring(t *testing.T) {
	if rootCmd.Use != "markfluence" {
		t.Errorf("rootCmd.Use = %q, want %q", rootCmd.Use, "markfluence")
	}
	for _, flag := range []string{"url", "debug", "no-color", "json"} {
		if rootCmd.PersistentFlags().Lookup(flag) == nil {
			t.Errorf("persistent flag --%s not registered", flag)
		}
	}
}

func TestSubcommandsRegistered(t *testing.T) {
	want := map[string]bool{"update": false, "create": false, "fix": false, "info": false, "read": false}
	for _, c := range rootCmd.Commands() {
		delete(want, c.Name())
	}
	for name := range want {
		t.Errorf("subcommand %q not registered", name)
	}
}
