// Package azuresource resolves the Azure credential a source kind's client
// is made from: azureclient's ambient chain (environment, workload identity,
// managed identity, the Azure CLI's login), with the slot's tenant applied
// (spec 0204 D18).
package azuresource

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"

	"gitlab.com/phpboyscout/go/azureclient"
	"gitlab.com/phpboyscout/go/config"
)

// Credential resolves the credential for a slot. Nothing is requested from
// Azure until a client first uses it.
func Credential(ctx context.Context, settings config.Reader) (azcore.TokenCredential, error) {
	var opts []azureclient.Option

	if tenant := settings.GetString("tenant_id"); tenant != "" {
		opts = append(opts, azureclient.WithTenantID(tenant))
	}

	return azureclient.Ambient(opts...).AzureCredential(ctx)
}

// ChainName names where the slot's Azure credential comes from, for
// doctor (spec 0204 D10).
const ChainName = "the Azure credential chain"
