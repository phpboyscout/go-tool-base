package gcpsecret

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configgcpsecret "gitlab.com/phpboyscout/go/config-gcp-secret"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func openFake(api configgcpsecret.API) opener {
	return func(context.Context, config.Reader) (configgcpsecret.API, io.Closer, error) { return api, nil, nil }
}

// A project's secrets, narrowed by a name prefix, decoded through a value
// format, at a pinned version and poll cadence.
func TestFactory_ReadsAProjectsSecretsWithItsOptions(t *testing.T) {
	t.Parallel()

	api := fakeSecrets{"app-db": `{"host":"db.internal"}`, "other": `{"host":"elsewhere"}`}

	backend, err := factoryWith(openFake(api))(t.Context(),
		settings(t, "project: acme\nname_prefix: app-\nversion: latest\nvalue_format: json\npoll_interval: 90s\n"), withJSON)
	require.NoError(t, err)

	view := resolve(t, backend)
	assert.False(t, view.IsSet("other"), "the name prefix narrows the project's secrets")
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	t.Parallel()

	_, err := factoryWith(openFake(fakeSecrets{}))(t.Context(), settings(t, "project: acme\npoll_interval: soon\n"), withJSON)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")

	_, err = factoryWith(openFake(fakeSecrets{}))(t.Context(), settings(t, "project: acme\nvalue_format: json\n"), bootstrap{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)

	failing := func(context.Context, config.Reader) (configgcpsecret.API, io.Closer, error) {
		return nil, nil, errors.New("no credentials")
	}

	_, err = factoryWith(failing)(t.Context(), settings(t, "project: acme\nsecret: app\n"), withJSON)
	require.ErrorContains(t, err, "no credentials")
}
