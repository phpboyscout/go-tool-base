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
// by blank-importing pkg/setup/keychain (which links go/credentials/keychain
// and declares the feature), and a regulated downstream that omits the import
// gets go-keyring, and its D-Bus and wincred transitives, dropped by the
// linker.
//
// [TestDefaultConfigLayers_ExcludesKeychain] guards the decision. This guards
// the CONSEQUENCE, which is the part that can rot without anyone noticing: the
// moment any framework package other than the link itself acquires a path to
// go-keyring, every downstream links it whatever they declared, and the only
// symptom is a larger binary and a new dependency in an audit.
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
		link    = module + "/pkg/setup/keychain"
	)

	// The framework core: every pkg/ package except the link, which exists to
	// reach go-keyring and is imported only by a main that wants it.
	var core []string
	for _, pkg := range list(t, module+"/pkg/...") {
		if pkg != link {
			core = append(core, pkg)
		}
	}

	assert.NotContains(t, deps(t, core...), keyring,
		"a framework package now reaches go-keyring, so every downstream links it "+
			"regardless of whether they blank-imported pkg/setup/keychain")

	// The other arm, and the reason the first is not vacuous: the link itself
	// must reach go-keyring, and so must the gtb binary that imports it.
	assert.Contains(t, deps(t, link), keyring, "pkg/setup/keychain is the link; it must reach go-keyring")
	assert.Contains(t, deps(t, module+"/cli/cmd/gtb"), keyring,
		"cli/cmd/gtb blank-imports pkg/setup/keychain, so it must link go-keyring")
}

// list returns the packages a pattern matches.
func list(t *testing.T, pattern string) []string {
	t.Helper()

	// #nosec G204 -- pattern is a compile-time constant from this test
	out, err := exec.CommandContext(t.Context(), "go", "list", pattern).Output()
	require.NoErrorf(t, err, "go list %s", pattern)

	return strings.Fields(string(out))
}

// deps returns the full transitive import set of the given packages.
func deps(t *testing.T, patterns ...string) []string {
	t.Helper()

	args := append([]string{"list", "-deps"}, patterns...)

	// #nosec G204 -- the arguments are package paths from go list or constants in this test
	out, err := exec.CommandContext(t.Context(), "go", args...).Output()
	require.NoErrorf(t, err, "go list -deps %v", patterns)

	return strings.Fields(string(out))
}
