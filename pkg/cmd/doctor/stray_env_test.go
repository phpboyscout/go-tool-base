package doctor

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go/features"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0205 D3: a <PREFIX>_* variable whose key the tool does not declare is
// reported by name and key, never by value, and the check never gates.
func TestCheckStrayEnvVariables(t *testing.T) {
	t.Parallel()

	const secret = "never-print-this"

	props := func(t *testing.T, environ ...string) *p.Props {
		t.Helper()

		store, err := config.NewStore(t.Context(), config.WithEnv("TOOLTEST", config.WithEnviron(func() []string { return environ })))
		require.NoError(t, err)

		return &p.Props{
			Tool:   p.Tool{Name: "tooltest", EnvPrefix: "TOOLTEST"},
			Config: store,
			Assets: p.NewAssets(p.AssetMap{"tool": fstest.MapFS{
				"assets/config.yaml": &fstest.MapFile{Data: []byte("myapp:\n  setting: on\n")},
			}}),
		}
	}

	t.Run("variables for declared and framework keys pass", func(t *testing.T) {
		t.Parallel()

		result := checkStrayEnvVariables(context.Background(), props(t, "TOOLTEST_LOG_LEVEL=debug", "TOOLTEST_MYAPP_SETTING=off"))
		assert.Equal(t, "Environment variables", result.Name)
		assert.Equal(t, CheckPass, result.Status)
	})

	t.Run("undeclared keys warn by variable and key, without gating", func(t *testing.T) {
		t.Parallel()

		result := checkStrayEnvVariables(context.Background(), props(t,
			"TOOLTEST_LOG_LEVEL=debug", "TOOLTEST_ANNOUNCE_FLAGS="+secret, "TOOLTEST_VERSION=1.2.3"))
		assert.Equal(t, CheckWarn, result.Status)
		assert.False(t, result.Gating, "a CI job's own variables must not fail doctor")
		assert.Contains(t, result.Details, "TOOLTEST_ANNOUNCE_FLAGS sets announce.flags")
		assert.Contains(t, result.Details, "TOOLTEST_VERSION sets version")
		assert.NotContains(t, result.Details, "TOOLTEST_LOG_LEVEL")
		assert.NotContains(t, result.Message+result.Details, secret)
	})

	t.Run("a variable read by name is not stray, whatever key it maps to", func(t *testing.T) {
		t.Parallel()

		r := features.NewRegistry()
		setup.DeclareEnvVariableOn(r, "TOOLTEST_OWN_KNOB")

		set, err := features.Resolve(r.Snapshot(), nil)
		require.NoError(t, err)

		tp := props(t, "TOOLTEST_OWN_KNOB=1")
		tp.Features = set

		assert.Equal(t, CheckPass, checkStrayEnvVariables(context.Background(), tp).Status)
	})

	t.Run("the framework's GTB_ACCESSIBLE is not stray under the GTB prefix", func(t *testing.T) {
		t.Parallel()

		store, err := config.NewStore(t.Context(), config.WithEnv("GTB", config.WithEnviron(func() []string { return []string{"GTB_ACCESSIBLE=true"} })))
		require.NoError(t, err)

		gp := &p.Props{Tool: p.Tool{Name: "gtb", EnvPrefix: "GTB"}, Config: store}

		assert.Equal(t, CheckPass, checkStrayEnvVariables(context.Background(), gp).Status)
	})

	t.Run("no prefix, no environment layer, nothing to check", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, CheckSkip, checkStrayEnvVariables(context.Background(), &p.Props{}).Status)
	})
}
