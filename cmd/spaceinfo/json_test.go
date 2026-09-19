package spaceinfo

import (
	"bytes"
	"strings"
	"testing"

	"github.com/mozilla/markfluence/internal/jsonout"
	"github.com/mozilla/markfluence/internal/schematest"
	"github.com/mozilla/markfluence/internal/ui"
)

// The document is built with the command's own builder rather than a hand-copied
// literal, so a renamed key or a changed summary has to reach the schema
// through this test rather than validating a copy while the real output drifts.
func TestSchemaConformance(t *testing.T) {
	cases := map[string]report{
		"everything read": stub{
			operations:  []string{"read:space", "create:page", "administer:space"},
			spaceStates: []string{"Rough draft", "Verified"},
			pages:       [][]string{{row("1", "current", "", 2, 1), row("2", "archived", "1", 40, 40)}},
		}.mustBuild(t),
		"homepage fallback": stub{
			homepageStates: []string{"Rough draft"},
			pages:          [][]string{{row("1", "current", "", 40, 40)}},
		}.mustBuild(t),
		// The three independent degradations, together: no operations, no
		// statuses from either source, and a walk that died partway.
		"everything degraded": stub{
			omitOps:   true,
			pages:     [][]string{{row("1", "current", "", 40, 40)}, {row("2", "current", "", 40, 40)}},
			walkFails: true,
		}.mustBuild(t),
	}
	for name, rep := range cases {
		t.Run(name, func(t *testing.T) {
			env := jsonout.NewEnvelope("space-info", []any{rep.jsonResult()},
				map[string]int{"total": 1, "succeeded": 1, "failed": 0})
			var buf bytes.Buffer
			if err := jsonout.Emit(&buf, env); err != nil {
				t.Fatalf("Emit: %v", err)
			}
			schematest.ValidateEnvelope(t, buf.Bytes())
		})
	}
}

// Every failure this command has goes to stderr as an error object, since there
// is no page id to name in a results[0] entry -- find/search/children --space's
// rule. All of stderr is validated as one document, which is also what forbids
// a stray human line under --json.
func TestFatalFailValidates(t *testing.T) {
	for name, code := range map[string]jsonout.Code{
		"not found":  jsonout.CodeNotFound,
		"validation": jsonout.CodeValidation,
		"config":     jsonout.CodeConfig,
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := jsonout.EmitError(&buf, "space-info", "something went wrong", code); err != nil {
				t.Fatalf("EmitError: %v", err)
			}
			schematest.ValidateError(t, buf.Bytes())
		})
	}
}

// --since is validated before anything else, including credentials: a negative
// day count is a local defect and should not cost a request or a token.
func TestNegativeSinceIsRefused(t *testing.T) {
	sinceDays = -1
	t.Cleanup(func() { sinceDays = 7 })
	err := run(Cmd, []string{"ENG"})
	if !ui.IsSilent(err) || ui.ExitCode(err) != 2 {
		t.Fatalf("run = %v, want a silent exit-2 error", err)
	}
}

// The schema's command enum has to hold this command. That its if/then branch
// actually constrains results *and* summary is checked by
// internal/schematest's own document tests, and that the command is registered
// is checked from the other side by cmd's TestCommandEnumMatchesRegisteredCommands
// -- this is the near half of that loop.
func TestCommandIsInTheSchemaEnum(t *testing.T) {
	if !strings.Contains(strings.Join(schematest.Commands(t), " "), "space-info") {
		t.Error("space-info is not in the schema's command enum")
	}
}
