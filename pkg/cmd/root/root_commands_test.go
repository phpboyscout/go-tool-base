package root

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// rootCommandsRegistry is the default registry's descriptors plus one plugin
// feature contributing the given root-command providers, so the root under
// test is the real tree with one contributor of the test's own (spec 0199 D5).
func rootCommandsRegistry(t *testing.T, plugin p.FeatureID, providers ...setup.RootCommandProvider) features.Registry {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(p.FeatureDescriptor{ID: plugin, ConstName: "Probe", ConstPackage: "example.com/probe", Kind: "plugin"}))
	setup.RegisterRootCommandsOn(reg, plugin, providers...)

	return reg
}

// TestRootCommands_ContributionReachesTheRoot: an enabled feature's provider
// puts a command on the root; the root stamps an unstamped one with the
// contributing feature and leaves a provider's own stamp alone; a disabled
// feature's provider is never called (spec 0202 D2).
func TestRootCommands_ContributionReachesTheRoot(t *testing.T) {
	t.Parallel()

	const plugin = p.FeatureID("root-probe")

	calls := 0
	reg := rootCommandsRegistry(t, plugin,
		func(*p.Props) *setup.Command {
			calls++

			return setup.Wrap("", &cobra.Command{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }})
		},
		func(*p.Props) *setup.Command {
			return setup.Wrap("other", &cobra.Command{Use: "stamped", RunE: func(*cobra.Command, []string) error { return nil }})
		},
		func(*p.Props) *setup.Command { return nil },
	)

	props := &p.Props{
		Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable(plugin))},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}
	root := NewCmdRootWithOptions(props, WithRegistry(reg))

	probe, _, err := root.Find([]string{"probe"})
	require.NoError(t, err)
	assert.Equal(t, "probe", probe.Name())
	assert.Equal(t, plugin, setup.FeatureOf(probe), "an unstamped contribution is stamped with its contributor")

	stamped, _, err := root.Find([]string{"stamped"})
	require.NoError(t, err)
	assert.Equal(t, p.FeatureID("other"), setup.FeatureOf(stamped), "a provider's own stamp is left alone")

	assert.Equal(t, 1, calls, "each provider is called once per root")

	off := &p.Props{
		Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(p.Disable(plugin))},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}
	offRoot := NewCmdRootWithOptions(off, WithRegistry(reg))
	_, _, err = offRoot.Find([]string{"probe"})
	require.Error(t, err, "a disabled feature contributes nothing")
	assert.Equal(t, 1, calls, "a disabled feature's provider is not called")
}

// TestRootCommands_CollisionIsSkippedAndLogged: a contribution named like a
// command already on the root loses, at warn, naming both features, rather
// than shadowing a built-in or taking the tool down at construction.
func TestRootCommands_CollisionIsSkippedAndLogged(t *testing.T) {
	t.Parallel()

	const plugin = p.FeatureID("root-collider")

	reg := rootCommandsRegistry(t, plugin, func(*p.Props) *setup.Command {
		return setup.Wrap("", &cobra.Command{Use: "version", RunE: func(*cobra.Command, []string) error { return nil }})
	})

	log := logger.NewBuffer()
	props := &p.Props{
		Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable(plugin))},
		Logger: log,
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}
	root := NewCmdRootWithOptions(props, WithRegistry(reg))

	var versions int
	for _, c := range root.Commands() {
		if c.Name() == "version" {
			versions++
		}
	}

	assert.Equal(t, 1, versions, "the built-in keeps the name; the collision is not added")
	assert.True(t, log.ContainsLevel(logger.WarnLevel, "root-collider"), "the warn names the contributing feature")
	assert.True(t, log.ContainsLevel(logger.WarnLevel, "version"), "the warn names the command")
}
