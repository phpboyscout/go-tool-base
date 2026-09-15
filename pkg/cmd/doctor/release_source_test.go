package doctor

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestCheckReleaseSource pins spec 0195 D10: the release source type must
// have a registered provider whatever the forge features say. The
// forge-adapters check asks only about enabled features, which is why a
// default project that enabled update and no forge passed doctor while its
// updater could not be built.
func TestCheckReleaseSource(t *testing.T) {
	t.Run("no release source is skipped", func(t *testing.T) {
		res := checkReleaseSource(context.Background(), &p.Props{Tool: p.Tool{Name: "t"}})
		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("update disabled is skipped", func(t *testing.T) {
		props := &p.Props{Tool: p.Tool{
			Name:          "t",
			ReleaseSource: p.ReleaseSource{Type: "github", Owner: "o", Repo: "r"},
			Features:      p.SetFeatures(p.Disable(p.UpdateCmd)),
		}}
		res := checkReleaseSource(context.Background(), props)
		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("a type with no registered provider fails and names the import", func(t *testing.T) {
		props := &p.Props{Tool: p.Tool{
			Name:          "t",
			ReleaseSource: p.ReleaseSource{Type: "github", Owner: "o", Repo: "r"},
		}}
		res := checkReleaseSource(context.Background(), props)
		require.Equal(t, CheckFail, res.Status, res.Message)
		assert.Contains(t, res.Message, "github")
		assert.Contains(t, res.Message, "gitlab.com/phpboyscout/go/forge-github")
	})

	t.Run("a registered type passes", func(t *testing.T) {
		require.NoError(t, forgeapi.Register("doctor-release-test", func(context.Context, forgeapi.Endpoint, forgeapi.Config, ...forgeapi.Option) (forgeapi.Provider, error) {
			return nil, nil
		}))

		props := &p.Props{Tool: p.Tool{
			Name:          "t",
			ReleaseSource: p.ReleaseSource{Type: "doctor-release-test", Owner: "o", Repo: "r"},
		}}
		res := checkReleaseSource(context.Background(), props)
		assert.Equal(t, CheckPass, res.Status, res.Message)
	})

	t.Run("it is in the default check set", func(t *testing.T) {
		want := reflect.ValueOf(checkReleaseSource).Pointer()

		found := false
		for _, check := range DefaultChecks(&p.Props{}) {
			if reflect.ValueOf(check).Pointer() == want {
				found = true
			}
		}

		assert.True(t, found, "checkReleaseSource must be a default check")
	})

}
