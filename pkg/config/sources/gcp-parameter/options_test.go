package gcpparameter

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configgcpparameter "gitlab.com/phpboyscout/go/config-gcp-parameter"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type unlinked struct{ bootstrap }

func (unlinked) CodecFor(path string) (config.Codec, error) {
	return nil, errors.Wrapf(setup.ErrUnlinkedConfigFormat, "%s", path)
}

func openFake(pm configgcpparameter.PM) opener {
	return func(context.Context, config.Reader) (configgcpparameter.PM, io.Closer, error) { return pm, nil, nil }
}

// listingPM lists one parameter under any prefix.
type listingPM struct{ fakePM }

func (listingPM) List(context.Context, string) ([]configgcpparameter.Parameter, error) {
	return []configgcpparameter.Parameter{{ID: "mytool-region", VersionName: "mytool-region/versions/1", Payload: []byte("eu-west-2")}}, nil
}

// The prefix shape, at a poll cadence of the slot's choosing: each parameter
// under the prefix is a key, with the prefix stripped.
func TestFactory_ReadsEveryParameterUnderAPrefix(t *testing.T) {
	t.Parallel()

	backend, err := factoryWith(openFake(listingPM{}))(t.Context(), settings(t, "project: acme\nprefix: mytool-\npoll_interval: 90s\n"), bootstrap{})
	require.NoError(t, err)
	assert.Equal(t, "eu-west-2", resolve(t, backend).GetString("region"))
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	t.Parallel()

	_, err := factoryWith(openFake(fakePM{}))(t.Context(), settings(t, "project: acme\nparameter: mytool\npoll_interval: soon\n"), bootstrap{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")

	_, err = factoryWith(openFake(fakePM{}))(t.Context(), settings(t, "project: acme\nparameter: mytool\n"), unlinked{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat, "one parameter is a YAML document by default, through the linked codec")

	failing := func(context.Context, config.Reader) (configgcpparameter.PM, io.Closer, error) {
		return nil, nil, errors.New("no credentials")
	}

	_, err = factoryWith(failing)(t.Context(), settings(t, "project: acme\nparameter: mytool\n"), bootstrap{})
	require.ErrorContains(t, err, "no credentials")
}
