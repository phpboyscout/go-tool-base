package props_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// A config format is a link (spec 0204 D8): a tool that does not blank-import
// pkg/config/formats/<format> must carry neither the format's module nor what
// it brings. This fails the moment any other framework package reaches one.
func TestConfigFormatsAreNotLinkedByTheFrameworkCore(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "deps")

	t.Parallel()

	const (
		module = "gitlab.com/phpboyscout/go-tool-base"
		links  = module + "/pkg/config/formats/"
	)

	forbidden := []string{
		"gitlab.com/phpboyscout/go/config-toml",
		"gitlab.com/phpboyscout/go/config-json",
		"gitlab.com/phpboyscout/go/config-hcl",
		"gitlab.com/phpboyscout/go/config-ini",
		"gitlab.com/phpboyscout/go/config-xml",
		"gitlab.com/phpboyscout/go/config-dotenv",
		"gitlab.com/phpboyscout/go/config-properties",
		"github.com/hashicorp/hcl/v2",
		"github.com/zclconf/go-cty",
		"github.com/tidwall/gjson",
	}

	var core []string
	for _, pkg := range list(t, module+"/pkg/...") {
		if !strings.HasPrefix(pkg, links) {
			core = append(core, pkg)
		}
	}

	coreDeps := deps(t, core...)
	for _, mod := range forbidden {
		assert.Falsef(t, reaches(coreDeps, mod),
			"a framework package now reaches %s, so every downstream links it whether or not it linked the format", mod)
	}

	assert.True(t, reaches(deps(t, links+"hcl"), "github.com/hashicorp/hcl/v2"), "the hcl link must reach its module")
}
