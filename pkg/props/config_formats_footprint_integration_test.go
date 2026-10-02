package props_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
)

// A config format or source kind is a link (spec 0204 D8): a tool that does
// not blank-import pkg/config/formats/<format> or pkg/config/sources/<kind>
// must carry neither the adapter module nor what it brings. This fails the
// moment any other framework package reaches one.
func TestConfigFormatsAreNotLinkedByTheFrameworkCore(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "deps")

	t.Parallel()

	const (
		module = "gitlab.com/phpboyscout/go-tool-base"
		links  = module + "/pkg/config/"
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
		"gitlab.com/phpboyscout/go/config-keychain",
		"gitlab.com/phpboyscout/go/config-vault",
		"gitlab.com/phpboyscout/go/config-consul",
		"gitlab.com/phpboyscout/go/vaultclient",
		"github.com/hashicorp/vault/api",
		"github.com/hashicorp/consul/api",
		"gitlab.com/phpboyscout/go/awsclient",
		"gitlab.com/phpboyscout/go/config-aws-s3",
		"gitlab.com/phpboyscout/go/config-aws-ssm",
		"gitlab.com/phpboyscout/go/config-aws-secrets",
		"github.com/aws/aws-sdk-go-v2",
		"gitlab.com/phpboyscout/go/azureclient",
		"gitlab.com/phpboyscout/go/config-azure-blob",
		"gitlab.com/phpboyscout/go/config-azure-keyvault",
		"gitlab.com/phpboyscout/go/config-azure-appconfig",
		"github.com/Azure/azure-sdk-for-go",
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

	assert.True(t, reaches(deps(t, links+"formats/hcl"), "github.com/hashicorp/hcl/v2"), "the hcl link must reach its module")
	assert.True(t, reaches(deps(t, links+"sources/keychain"), "gitlab.com/phpboyscout/go/config-keychain"), "the keychain link must reach its module")
}
