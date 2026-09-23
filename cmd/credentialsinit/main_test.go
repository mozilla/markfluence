package credentialsinit

import (
	"os"
	"testing"

	"github.com/mozilla/markfluence/internal/testenv"
)

// TestMain hides the developer's own credentials from this package's tests;
// see testenv.RunIsolated.
func TestMain(m *testing.M) { os.Exit(testenv.RunIsolated(m)) }
