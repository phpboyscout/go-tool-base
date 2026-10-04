package azurekeyvault

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configazurekeyvault "gitlab.com/phpboyscout/go/config-azure-keyvault"
	"gitlab.com/phpboyscout/go/errors"
)

// Every secret in the vault, narrowed by a name prefix and decoded through a
// value format, at the slot's poll cadence.
func TestFactory_TheVaultWithItsOptions(t *testing.T) {
	t.Parallel()

	var url string

	backend, err := factoryWith(opening(fakeVault{"app-db": `{"host":"db.internal"}`, "other": `{"host":"x"}`}, &url))(t.Context(),
		settings(t, "vault_url: https://acme.vault.azure.net\nname_prefix: app-\nvalue_format: json\npoll_interval: 90s\n"), withJSON)
	require.NoError(t, err)
	assert.False(t, resolve(t, backend).IsSet("other"), "the name prefix narrows the vault")
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	t.Parallel()

	var url string

	_, err := factoryWith(opening(fakeVault{}, &url))(t.Context(),
		settings(t, "vault_url: https://acme.vault.azure.net\npoll_interval: soon\n"), withJSON)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")

	failing := func(context.Context, config.Reader) (configazurekeyvault.SecretsAPI, error) {
		return nil, errors.New("no credentials")
	}

	_, err = factoryWith(failing)(t.Context(), settings(t, "vault_url: https://acme.vault.azure.net\nname: app\n"), withJSON)
	require.ErrorContains(t, err, "no credentials")
}
