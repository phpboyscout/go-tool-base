package props_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// The mcp feature is a host-binary decision, like the keychain: a build wires
// it by blank-importing pkg/mcp, and a regulated downstream that omits the
// import ships without go/mcp and the MCP SDK. This guards the consequence:
// the moment any framework package other than the link reaches the module,
// every downstream links it whatever they declared (spec 0202 D8).
func TestMCPIsNotLinkedByTheFrameworkCore(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "deps")

	t.Parallel()

	const (
		module   = "gitlab.com/phpboyscout/go-tool-base"
		link     = module + "/pkg/mcp"
		mcpMod   = "gitlab.com/phpboyscout/go/mcp"
		sdkMod   = "github.com/modelcontextprotocol/go-sdk"
		noReach  = "a framework package now reaches %s, so every downstream links it regardless of whether they blank-imported pkg/mcp"
		mustLink = "%s is the link (or imports it); it must reach %s"
	)

	var core []string
	for _, pkg := range list(t, module+"/pkg/...") {
		if pkg != link {
			core = append(core, pkg)
		}
	}

	coreDeps := deps(t, core...)
	for _, forbidden := range []string{mcpMod, sdkMod} {
		assert.Falsef(t, reaches(coreDeps, forbidden), noReach, forbidden)
	}

	for _, importer := range []string{link, module + "/cli/cmd/gtb"} {
		importerDeps := deps(t, importer)
		for _, wanted := range []string{mcpMod, sdkMod} {
			assert.Truef(t, reaches(importerDeps, wanted), mustLink, importer, wanted)
		}
	}
}

// reaches reports whether any package in deps lives under the module path.
func reaches(deps []string, modulePath string) bool {
	for _, d := range deps {
		if d == modulePath || strings.HasPrefix(d, modulePath+"/") {
			return true
		}
	}

	return false
}
