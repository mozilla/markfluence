package read

import (
	"os"
	"testing"

	"github.com/mozilla/markfluence/internal/clienttest"
)

// TestMain hides the developer's own credentials from this package's tests;
// see clienttest.RunIsolated.
func TestMain(m *testing.M) { os.Exit(clienttest.RunIsolated(m)) }
