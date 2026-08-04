package props_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// The keychain is a host-binary decision, not a framework one: a build wires it
// by blank-importing go/credentials/keychain, and a regulated downstream that
// omits the import gets go-keyring — and its D-Bus and wincred transitives —
// dropped by the linker.
//
// [TestDefaultConfigLayers_ExcludesKeychain] guards the decision. This guards
// the CONSEQUENCE, which is the part that can rot without anyone noticing: the
// moment any framework package acquires a non-blank path to go-keyring, every
// downstream links it whatever they declared, and the only symptom is a larger
// binary and a new dependency in an audit.
//
// It shells out to `go list`, so it is integration-gated for the same reason
// cmd/gtb-no-aws-smoke keeps its link-time arm out of the unit path: a unit test
// should not depend on the build toolchain being present and warm.
func TestKeychainIsNotLinkedByTheFrameworkCore(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "deps")

	t.Parallel()

	const (
		keyring = "github.com/zalando/go-keyring"
		module  = "gitlab.com/phpboyscout/go-tool-base"
	)

	// The framework core: everything a downstream gets by importing GTB,
	// before its own main makes any decision.
	assert.NotContains(t, deps(t, module+"/pkg/..."), keyring,
		"a framework package now reaches go-keyring, so every downstream links it "+
			"regardless of whether they blank-imported go/credentials/keychain")

	// The other arm, and the reason the first is not vacuous: a binary that DOES
	// blank-import it must link it. Without this, deleting the import
	// everywhere would leave the assertion above passing.
	assert.Contains(t, deps(t, module+"/cmd/gtb"), keyring,
		"cmd/gtb blank-imports go/credentials/keychain, so it must link go-keyring")
}

// deps returns the full transitive import set of a package pattern.
func deps(t *testing.T, pattern string) []string {
	t.Helper()

	// #nosec G204 -- pattern is a compile-time constant from this test
	out, err := exec.CommandContext(t.Context(), "go", "list", "-deps", pattern).Output()
	require.NoErrorf(t, err, "go list -deps %s", pattern)

	return strings.Fields(string(out))
}
