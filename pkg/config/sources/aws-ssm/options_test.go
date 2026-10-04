package awsssm

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type unlinked struct{ bootstrap }

func (unlinked) CodecFor(path string) (config.Codec, error) {
	return nil, errors.Wrapf(setup.ErrUnlinkedConfigFormat, "%s", path)
}

// A value format decodes a parameter holding a document into a subtree.
func TestFactory_AValueFormat(t *testing.T) {
	awssourcetest.Isolate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_ = json.NewEncoder(w).Encode(map[string]any{"Parameters": []map[string]any{
			{"Name": "/team/mytool/db", "Type": "String", "Value": `{"host":"db.internal"}`, "Version": 1},
		}})
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(), settings(t, "prefix: /team/mytool\nendpoint: "+srv.URL+"\nvalue_format: json\npoll_interval: 90s\n"), bootstrap{})
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", store.View().GetString("db.host"))
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := factory(t.Context(), settings(t, "prefix: /team\nvalue_format: hcl\n"), unlinked{})
	require.ErrorIs(t, err, setup.ErrUnlinkedConfigFormat)

	_, err = factory(t.Context(), settings(t, "prefix: /team\npoll_interval: soon\n"), bootstrap{})
	require.ErrorContains(t, err, "poll_interval")

	_, err = factory(t.Context(), settings(t, "prefix: /team\nprofile: no-such-profile\n"), bootstrap{})
	require.ErrorContains(t, err, "no-such-profile")
}
