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

func shortsNamed(root *setup.Command, name string) []string {
	var shorts []string

	for _, c := range root.Commands() {
		if c.Name() == name {
			shorts = append(shorts, c.Short)
		}
	}

	return shorts
}

func toolCommand(name, short string) *setup.Command {
	return setup.Wrap(p.FeatureID(name), &cobra.Command{Use: name, Short: short, RunE: func(*cobra.Command, []string) error { return nil }})
}

// A tool's own command loses a name already on the root, whichever way cobra
// would have sorted the two (spec 0202, #224).
func TestRootCommands_ToolCommandCollisionIsSkippedAndLogged(t *testing.T) {
	t.Parallel()

	const (
		plugin    = p.FeatureID("root-probe")
		updateOne = "Update to the latest available version"
		versionIt = "Print version, commit, and build date"
	)

	on, off := p.Enable(p.UpdateCmd), p.Disable(p.UpdateCmd)

	for _, tc := range []struct {
		name     string
		update   p.FeatureState
		commands []*setup.Command
		want     map[string]string
		warnings []string
	}{
		{
			name: "a feature's command", update: on,
			commands: []*setup.Command{toolCommand("update", "own")},
			want:     map[string]string{"update": updateOne},
			warnings: []string{`the tool's own "update" command is skipped: feature "update" already provides one; rename yours, or disable that feature`},
		},
		{
			name: "a command with no feature", update: on,
			commands: []*setup.Command{toolCommand("version", "own")},
			want:     map[string]string{"version": versionIt},
			warnings: []string{`the tool's own "version" command is skipped: every tool has one; rename yours`},
		},
		{
			name: "two in one root", update: on,
			commands: []*setup.Command{toolCommand("version", "own"), toolCommand("update", "own")},
			want:     map[string]string{"version": versionIt, "update": updateOne},
			warnings: []string{`own "version" command is skipped`, `own "update" command is skipped`},
		},
		{
			name: "a command a feature contributes", update: on,
			commands: []*setup.Command{toolCommand("probe", "own")},
			want:     map[string]string{"probe": "contributed"},
			warnings: []string{`the tool's own "probe" command is skipped: feature "root-probe" already provides one`},
		},
		{
			name: "two of the tool's own", update: on,
			commands: []*setup.Command{toolCommand("deploy", "first"), toolCommand("deploy", "second")},
			want:     map[string]string{"deploy": "first"},
			warnings: []string{`the tool's own "deploy" command is skipped: the tool has another of that name; rename one of them`},
		},
		{
			name: "a disabled feature leaves the name free", update: off,
			commands: []*setup.Command{toolCommand("update", "own")},
			want:     map[string]string{"update": "own"},
		},
		{
			name: "a name differing in case is another name", update: on,
			commands: []*setup.Command{toolCommand("Update", "own")},
			want:     map[string]string{"update": updateOne, "Update": "own"},
		},
		{
			name: "a missing command", update: on,
			commands: []*setup.Command{nil, {}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reg := rootCommandsRegistry(t, plugin, func(*p.Props) *setup.Command {
				return setup.Wrap("", &cobra.Command{Use: "probe", Short: "contributed", RunE: func(*cobra.Command, []string) error { return nil }})
			})

			log := logger.NewBuffer()
			props := &p.Props{
				Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(tc.update, p.Enable(plugin))},
				Logger: log,
				FS:     afero.NewMemMapFs(),
				Assets: p.NewAssets(),
			}
			root := NewCmdRootWithOptions(props, WithRegistry(reg), WithSubcommands(append(tc.commands, toolCommand("after", "after"))...))

			for name, short := range tc.want {
				assert.Equal(t, []string{short}, shortsNamed(root, name))
			}

			for _, w := range tc.warnings {
				assert.True(t, log.ContainsLevel(logger.WarnLevel, w), w)
			}

			if len(tc.warnings) == 0 {
				assert.False(t, log.ContainsLevel(logger.WarnLevel, "is skipped"))
			}

			assert.Equal(t, []string{"after"}, shortsNamed(root, "after"), "the commands after a skipped one are attached")
		})
	}
}

// A name on the root beats an alias: the alias goes, the command stays (spec
// 0202, #224).
func TestRootCommands_AnAliasThatIsANameIsDropped(t *testing.T) {
	t.Parallel()

	const plugin = p.FeatureID("root-probe")

	aliased := func(name string, aliases ...string) *setup.Command {
		c := toolCommand(name, "own")
		c.Aliases = aliases

		return c
	}

	for _, tc := range []struct {
		name        string
		update      p.FeatureState
		contributed []string
		commands    []*setup.Command
		runs        map[string]string
		warnings    []string
	}{
		{
			name: "a feature's command", update: p.Enable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "ap", "update")},
			runs:     map[string]string{"update": "update", "apply": "apply", "ap": "apply"},
			warnings: []string{`the alias "update" of the "apply" command is dropped: feature "update" provides a command of that name`},
		},
		{
			name: "a command with no feature, and two in one root", update: p.Enable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "version", "update")},
			runs:     map[string]string{"update": "update", "version": "version", "apply": "apply"},
			warnings: []string{
				`the alias "version" of the "apply" command is dropped: every tool has a command of that name`,
				`the alias "update" of the "apply" command is dropped`,
			},
		},
		{
			name: "a feature's contributed command", update: p.Enable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "probe")},
			runs:     map[string]string{"probe": "probe"},
			warnings: []string{`the alias "probe" of the "apply" command is dropped: feature "root-probe" provides a command of that name`},
		},
		{
			name: "a contribution's own alias", update: p.Enable(p.UpdateCmd), contributed: []string{"pr", "update"},
			runs:     map[string]string{"update": "update", "pr": "probe"},
			warnings: []string{`the alias "update" of the "probe" command is dropped`},
		},
		{
			name: "the tool's own, whichever came first", update: p.Enable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "ship"), aliased("ship", "deploy"), aliased("deploy")},
			runs:     map[string]string{"ship": "ship", "deploy": "deploy"},
			warnings: []string{
				`the alias "ship" of the "apply" command is dropped: the tool has a command of that name`,
				`the alias "deploy" of the "ship" command is dropped: the tool has a command of that name`,
			},
		},
		{
			name: "a disabled feature leaves the name free", update: p.Disable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "update")},
			runs:     map[string]string{"update": "apply"},
		},
		{
			name: "its own name", update: p.Enable(p.UpdateCmd),
			commands: []*setup.Command{aliased("apply", "apply")},
			runs:     map[string]string{"apply": "apply"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reg := rootCommandsRegistry(t, plugin, func(*p.Props) *setup.Command {
				return setup.Wrap("", &cobra.Command{Use: "probe", Aliases: tc.contributed, RunE: func(*cobra.Command, []string) error { return nil }})
			})

			log := logger.NewBuffer()
			props := &p.Props{
				Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(tc.update, p.Enable(plugin))},
				Logger: log,
				FS:     afero.NewMemMapFs(),
				Assets: p.NewAssets(),
			}
			root := NewCmdRootWithOptions(props, WithRegistry(reg), WithSubcommands(tc.commands...))

			for word, want := range tc.runs {
				got, _, err := root.Find([]string{word})
				require.NoError(t, err, word)
				assert.Equal(t, want, got.Name(), word)
			}

			for _, w := range tc.warnings {
				assert.True(t, log.ContainsLevel(logger.WarnLevel, w), w)
			}

			if len(tc.warnings) == 0 {
				assert.False(t, log.ContainsLevel(logger.WarnLevel, "is dropped"))
			}
		})
	}
}

func TestRootCommands_DroppingAnAliasLeavesTheCallersSliceAlone(t *testing.T) {
	t.Parallel()

	aliases := []string{"update", "ap"}
	cmd := toolCommand("apply", "own")
	cmd.Aliases = aliases

	NewCmdRoot(&p.Props{
		Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable(p.UpdateCmd))},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}, cmd)

	assert.Equal(t, []string{"ap"}, cmd.Aliases)
	assert.Equal(t, []string{"update", "ap"}, aliases)
}
