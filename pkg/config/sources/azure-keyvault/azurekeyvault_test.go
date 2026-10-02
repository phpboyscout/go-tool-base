package azurekeyvault

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configazurekeyvault "gitlab.com/phpboyscout/go/config-azure-keyvault"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// enabled is what a live, usable secret carries; the adapter skips the rest.
var enabled = configazurekeyvault.Fitness{Enabled: true}

type fakeVault map[string]string

func (f fakeVault) Get(_ context.Context, name string) (*configazurekeyvault.Secret, error) {
	return &configazurekeyvault.Secret{Name: name, Value: f[name], Version: "1", Fitness: enabled}, nil
}

func (f fakeVault) List(context.Context) ([]configazurekeyvault.Properties, error) {
	var out []configazurekeyvault.Properties
	for name := range f {
		out = append(out, configazurekeyvault.Properties{Name: name, Version: "1", Fitness: enabled})
	}

	return out, nil
}

func opening(api configazurekeyvault.SecretsAPI, url *string) opener {
	return func(_ context.Context, settings config.Reader) (configazurekeyvault.SecretsAPI, error) {
		*url = settings.GetString("vault_url")

		return api, nil
	}
}

// Spec 0204 D18: an azure-keyvault source reads one secret whose value is a
// document (JSON by default), from the vault its slot names; it is sensitive.
func TestFactory_ReadsOneSecret(t *testing.T) {
	t.Parallel()

	var url string

	backend, err := factoryWith(opening(fakeVault{"app": `{"db":{"host":"db.internal"}}`}, &url))(
		t.Context(), settings(t, "vault_url: https://acme.vault.azure.net\nname: app\n"), withJSON)
	require.NoError(t, err)
	assert.Equal(t, "https://acme.vault.azure.net", url)
	assert.Equal(t, "db.internal", resolve(t, backend).GetString("db.host"))
	assert.True(t, backend.Capabilities().Sensitive)
}

// Without a name, every secret in the vault is a key, scoped by an optional
// name prefix.
func TestFactory_ReadsTheVault(t *testing.T) {
	t.Parallel()

	var url string

	backend, err := factoryWith(opening(fakeVault{"token": "s3cret"}, &url))(
		t.Context(), settings(t, "vault_url: https://acme.vault.azure.net\n"), withJSON)
	require.NoError(t, err)
	assert.Equal(t, "s3cret", resolve(t, backend).GetString("token"))
}

func TestFactory_Refuses(t *testing.T) {
	t.Parallel()

	var url string

	open := opening(fakeVault{}, &url)

	_, err := factoryWith(open)(t.Context(), settings(t, "name: app\n"), withJSON)
	require.ErrorIs(t, err, ErrNoVault)

	_, err = factoryWith(open)(t.Context(), settings(t, "vault_url: https://acme.vault.azure.net\nname: app\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, "a JSON secret needs the json format linked")
}

// The production opener builds a client without contacting Azure.
func TestOpenVault_BuildsWithoutTheNetwork(t *testing.T) {
	t.Parallel()

	api, err := openVault(t.Context(), settings(t, "vault_url: https://acme.vault.azure.net\n"))
	require.NoError(t, err)
	assert.NotNil(t, api)
}
