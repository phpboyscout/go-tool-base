package azureappconfig

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configazureappconfig "gitlab.com/phpboyscout/go/config-azure-appconfig"
)

type fakeStore struct {
	settings []configazureappconfig.Setting
	label    *string
}

func (f fakeStore) List(_ context.Context, keyFilter, label string) ([]configazureappconfig.Setting, error) {
	*f.label = label

	var out []configazureappconfig.Setting

	for _, s := range f.settings {
		if strings.HasPrefix(s.Key, strings.TrimSuffix(keyFilter, "*")) {
			out = append(out, s)
		}
	}

	return out, nil
}

func (f fakeStore) Get(context.Context, string, string, string) (configazureappconfig.Setting, bool, error) {
	return configazureappconfig.Setting{}, false, nil
}

func (f fakeStore) Set(context.Context, string, string, string, string, string) (bool, error) {
	return true, nil
}

func (f fakeStore) Delete(context.Context, string, string, string) (bool, error) {
	return true, nil
}

// Spec 0204 D18: an azure-appconfig source reads the settings under its
// prefix, under its label.
func TestFactory_ReadsThePrefix(t *testing.T) {
	t.Parallel()

	var label string

	store := fakeStore{label: &label, settings: []configazureappconfig.Setting{{Key: "mytool/log/level", Value: "debug", Label: "prod"}}}
	open := func(_ context.Context, _ config.Reader) (configazureappconfig.Store, error) { return store, nil }

	backend, err := factoryWith(open)(t.Context(), settings(t, "endpoint: https://acme.azconfig.io\nprefix: mytool/\nlabel: prod\n"), withJSON)
	require.NoError(t, err)
	assert.Equal(t, "debug", resolve(t, backend).GetString("log.level"))
	assert.Equal(t, "prod", label)
}

func TestFactory_NeedsAnEndpoint(t *testing.T) {
	t.Parallel()

	_, err := factoryWith(openStore)(t.Context(), settings(t, "prefix: mytool/\n"), withJSON)
	require.ErrorIs(t, err, ErrNoEndpoint)
}

func TestOpenStore_BuildsWithoutTheNetwork(t *testing.T) {
	t.Parallel()

	store, err := openStore(t.Context(), settings(t, "endpoint: https://acme.azconfig.io\n"))
	require.NoError(t, err)
	assert.NotNil(t, store)
}
