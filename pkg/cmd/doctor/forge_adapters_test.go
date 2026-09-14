package doctor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go/errors"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

func TestCheckForgeAdapters(t *testing.T) {
	t.Parallel()

	t.Run("no forge feature enabled is skipped", func(t *testing.T) {
		t.Parallel()

		res := checkForgeAdapters(t.Context(), &p.Props{})

		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("nil props is skipped", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, CheckSkip, checkForgeAdapters(t.Context(), nil).Status)
	})

	// The framework links no forge adapter, so an enabled forge fails and the
	// message names the module to import.
	t.Run("enabled but not linked fails with the import", func(t *testing.T) {
		t.Parallel()

		props := &p.Props{Tool: p.Tool{Features: []p.Feature{{ID: forge.GithubFeature, Enabled: true}}}}

		res := checkForgeAdapters(t.Context(), props)

		assert.Equal(t, CheckFail, res.Status)
		assert.Contains(t, res.Message, "GitHub")
		assert.Contains(t, res.Message, "gitlab.com/phpboyscout/go/forge-github")
	})

	t.Run("enabled and linked passes", func(t *testing.T) {
		t.Parallel()

		err := forgeapi.Register("gitlab", func(context.Context, forgeapi.Endpoint, forgeapi.Config, ...forgeapi.Option) (forgeapi.Provider, error) {
			return nil, nil //nolint:nilnil // never called: the check only asks whether the type is registered
		})
		if err != nil && !errors.Is(err, forgeapi.ErrAlreadyRegistered) {
			t.Fatal(err)
		}

		props := &p.Props{Tool: p.Tool{Features: []p.Feature{{ID: forge.GitlabFeature, Enabled: true}}}}

		res := checkForgeAdapters(t.Context(), props)

		assert.Equal(t, CheckPass, res.Status)
		assert.Contains(t, res.Message, "GitLab")
	})
}
