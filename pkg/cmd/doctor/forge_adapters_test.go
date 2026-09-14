package doctor

import (
	"testing"

	"github.com/stretchr/testify/assert"

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

	// Every forge adapter is linked into this test binary, so an enabled forge
	// passes and the message names it. The failing branch is exercised in
	// pkg/setup/forge, where the registry can be substituted.
	t.Run("enabled and linked passes", func(t *testing.T) {
		t.Parallel()

		props := &p.Props{Tool: p.Tool{Features: []p.Feature{{ID: forge.GithubFeature, Enabled: true}}}}

		res := checkForgeAdapters(t.Context(), props)

		assert.Equal(t, CheckPass, res.Status)
		assert.Contains(t, res.Message, "GitHub")
	})
}
