package azureappconfig

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configazureappconfig "gitlab.com/phpboyscout/go/config-azure-appconfig"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// A value format decodes a setting holding a document into a subtree, and a
// sentinel key is accepted alongside it.
func TestFactory_AValueFormatAndASentinel(t *testing.T) {
	t.Parallel()

	var label string

	store := fakeStore{label: &label, settings: []configazureappconfig.Setting{{Key: "mytool/db", Value: `{"host":"db.internal"}`}}}
	open := func(context.Context, config.Reader) (configazureappconfig.Store, error) { return store, nil }

	backend, err := factoryWith(open)(t.Context(),
		settings(t, "endpoint: https://acme.azconfig.io\nprefix: mytool/\nvalue_format: json\nsentinel_key: mytool/sentinel\n"), withJSON)
	require.NoError(t, err)
	assert.Equal(t, "db.internal", resolve(t, backend).GetString("db.host"))
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	t.Parallel()

	open := func(context.Context, config.Reader) (configazureappconfig.Store, error) {
		return nil, errors.New("no credentials")
	}

	_, err := factoryWith(open)(t.Context(), settings(t, "endpoint: https://acme.azconfig.io\nvalue_format: json\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)

	_, err = factoryWith(open)(t.Context(), settings(t, "endpoint: https://acme.azconfig.io\n"), withJSON)
	require.ErrorContains(t, err, "no credentials")
}
