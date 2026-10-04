package awssecrets

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config/sources/internal/awssource/awssourcetest"
)

// Every secret under a prefix, at a version stage and poll cadence of the
// slot's choosing, decoded through its value format.
func TestFactory_ReadsAPrefixWithItsOptions(t *testing.T) {
	awssourcetest.Isolate(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")

		switch r.Header.Get("X-Amz-Target") {
		case "secretsmanager.ListSecrets":
			_ = json.NewEncoder(w).Encode(map[string]any{"SecretList": []map[string]any{
				{"ARN": "arn:aws:secretsmanager:eu-west-2:1:secret:team/db", "Name": "team/db"},
			}})
		default:
			_ = json.NewEncoder(w).Encode(map[string]any{ //nolint:gosec // a fake secret in an httptest double
				"ARN": "arn:aws:secretsmanager:eu-west-2:1:secret:team/db", "Name": "team/db",
				"SecretString": `{"host":"db.internal"}`, "VersionId": "v1", "VersionStages": []string{"AWSPREVIOUS"},
			})
		}
	}))
	t.Cleanup(srv.Close)

	backend, err := factory(t.Context(),
		settings(t, "prefix: team/\nendpoint: "+srv.URL+"\nversion_stage: AWSPREVIOUS\nvalue_format: json\npoll_interval: 90s\n"), withJSON)
	require.NoError(t, err)

	store, err := config.NewStore(t.Context(), config.WithBackend(backend))
	require.NoError(t, err)
	assert.Equal(t, "db.internal", store.View().GetString("db.host"), "the prefix is stripped and the value decoded")
}

func TestFactory_RefusesItsSettings(t *testing.T) {
	awssourcetest.Isolate(t)

	_, err := factory(t.Context(), settings(t, "name: app\npoll_interval: soon\n"), withJSON)
	require.ErrorContains(t, err, "poll_interval")

	_, err = factory(t.Context(), settings(t, "name: app\nprofile: no-such-profile\n"), withJSON)
	require.ErrorContains(t, err, "no-such-profile")
}
