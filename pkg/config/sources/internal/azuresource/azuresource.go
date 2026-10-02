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

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
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

// Settings are the initialiser questions every Azure kind shares, after its
// own.
func Settings() []setup.SourceSetting {
	return []setup.SourceSetting{
		{Key: "tenant_id", Title: "Azure tenant", Description: "Leave empty to use the tenant the Azure chain resolves"},
	}
}
