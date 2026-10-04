package gcpgcs

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
)

func TestFactory_AnOpenerFailureStops(t *testing.T) {
	t.Parallel()

	failing := func(context.Context, config.Reader) (config.FS, io.Closer, error) {
		return nil, nil, errors.New("no credentials")
	}

	_, err := factoryWith(failing)(t.Context(), settings(t, "bucket: b\nobject: c.yaml\n"), bootstrap{})
	require.ErrorContains(t, err, "no credentials")
}
