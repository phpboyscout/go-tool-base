package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func builtinNamesProject(t *testing.T, l logger.Logger, m Manifest, cfg Config) *Generator {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/group", 0o755))
	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))

	raw, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", raw, 0o644))

	cfg.Path = "/work"

	return New(&props.Props{FS: fs, Logger: l, Config: emptyTestStore(t)}, &cfg)
}

func TestPrepareAndVerify_RefusesABuiltinsName(t *testing.T) {
	t.Parallel()

	off := func(names ...string) []ManifestFeature {
		var out []ManifestFeature
		for _, n := range names {
			out = append(out, ManifestFeature{Name: n, Enabled: false})
		}

		return out
	}

	tests := []struct {
		name     string
		command  string
		aliases  []string
		parent   string
		features []ManifestFeature
		existing []ManifestCommand
		force    bool
		dryRun   bool
		wantHint string
	}{
		{name: "a default-on feature", command: "update", wantHint: "gtb disable update"},
		{name: "the mcp link", command: "mcp", wantHint: "gtb disable mcp"},
		{name: "an opt-in feature that is on", command: "config", features: []ManifestFeature{{Name: "config", Enabled: true}}, wantHint: "gtb disable config"},
		{name: "an opt-in feature that is off", command: "config"},
		{name: "a disabled feature", command: "update", features: off("update")},
		{name: "version, which has no feature", command: "version", features: off("update"), wantHint: "every tool has"},
		{name: "help, which has no feature", command: "help", wantHint: "every tool has"},
		{name: "completion, which cobra gives up", command: "completion"},
		{name: "a feature with no command", command: "keychain", features: []ManifestFeature{{Name: "keychain", Enabled: true}}},
		{name: "under a parent", command: "update", parent: "group"},
		{name: "a command the project already has", command: "update", existing: []ManifestCommand{{Name: "update"}}},
		{name: "a nested command of that name is not the one being made", command: "update", existing: []ManifestCommand{{Name: "server", Commands: []ManifestCommand{{Name: "update"}}}}, wantHint: "gtb disable update"},
		{name: "an alias", command: "apply", aliases: []string{"ap", "update"}, wantHint: "gtb disable update"},
		{name: "an alias of version", command: "apply", aliases: []string{"version"}, wantHint: "every tool has"},
		{name: "an alias of completion", command: "apply", aliases: []string{"completion"}},
		{name: "an alias of a disabled feature", command: "apply", aliases: []string{"update"}, features: off("update")},
		{name: "an alias under a parent", command: "apply", aliases: []string{"update"}, parent: "group"},
		{name: "an alias the command already has", command: "apply", aliases: []string{"update"}, existing: []ManifestCommand{{Name: "apply", Aliases: []string{"update"}}}},
		{name: "an alias added to an existing command", command: "apply", aliases: []string{"update"}, existing: []ManifestCommand{{Name: "apply", Aliases: []string{"ap"}}}, wantHint: "gtb disable update"},
		{name: "an alias another command already has", command: "apply", aliases: []string{"update"}, existing: []ManifestCommand{{Name: "ship", Aliases: []string{"update"}}}, wantHint: "gtb disable update"},
		{name: "with --force", command: "update", force: true, wantHint: "gtb disable update"},
		{name: "in a dry run", command: "update", dryRun: true, wantHint: "gtb disable update"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			g := builtinNamesProject(t, logger.NewNoop(), Manifest{
				Properties: ManifestProperties{Features: tc.features},
				Commands:   append([]ManifestCommand{{Name: "group"}}, tc.existing...),
			}, Config{Name: tc.command, Aliases: tc.aliases, Parent: tc.parent, Force: tc.force, DryRun: tc.dryRun})

			_, err := g.prepareAndVerify()
			if tc.wantHint == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrBuiltinCommandName)
			assert.Contains(t, errors.FlattenHints(err), tc.wantHint)
		})
	}
}

func TestPrepareAndVerify_WarnsOfAnExistingCommandWithABuiltinsName(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		existing ManifestCommand
		aliases  []string
		want     string
	}{
		"its name":            {existing: ManifestCommand{Name: "update"}, want: `the top-level command "update" shares`},
		"its alias":           {existing: ManifestCommand{Name: "apply", Aliases: []string{"update"}}, aliases: []string{"update"}, want: `the alias "update" of the top-level command "apply" shares`},
		"an alias it keeps":   {existing: ManifestCommand{Name: "apply", Aliases: []string{"update"}}, want: `the alias "update" of the top-level command "apply" shares`},
		"an alias it gave up": {existing: ManifestCommand{Name: "apply", Aliases: []string{"update"}}, aliases: []string{"ap"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			log := logger.NewBuffer()
			g := builtinNamesProject(t, log, Manifest{Commands: []ManifestCommand{tc.existing}}, Config{Name: tc.existing.Name, Aliases: tc.aliases})

			_, err := g.prepareAndVerify()
			require.NoError(t, err)

			if tc.want == "" {
				assert.False(t, log.ContainsLevel(logger.WarnLevel, "shares"))

				return
			}

			assert.True(t, log.ContainsLevel(logger.WarnLevel, tc.want))
			assert.True(t, log.ContainsLevel(logger.WarnLevel, "gtb disable update"))
		})
	}
}

func TestApplyFeatures_RefusesAFeatureWhoseNameACommandHas(t *testing.T) {
	t.Parallel()

	g := builtinNamesProject(t, logger.NewNoop(), Manifest{
		Commands: []ManifestCommand{{Name: "config"}, {Name: "group", Commands: []ManifestCommand{{Name: "man"}}}},
	}, Config{})

	before, err := afero.ReadFile(g.props.FS, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)

	_, err = g.ApplyFeatures(context.Background(), map[string]bool{"config": true, "telemetry": true})
	require.ErrorIs(t, err, ErrBuiltinCommandName)
	assert.Contains(t, errors.FlattenHints(err), "config")

	after, err := afero.ReadFile(g.props.FS, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a refusal writes nothing")
}

// The wizard writes a project's features without ApplyFeatures.
func TestApplyAuthorSettings_RefusesAFeatureWhoseNameACommandHas(t *testing.T) {
	t.Parallel()

	g := builtinNamesProject(t, logger.NewNoop(), Manifest{Commands: []ManifestCommand{{Name: "config"}}}, Config{})

	before, err := g.LoadManifest()
	require.NoError(t, err)

	cfg := SkeletonConfigFromManifest(*before)
	cfg.Features = []ManifestFeature{{Name: "config", Enabled: true}}

	require.ErrorIs(t, g.ApplyAuthorSettings(context.Background(), cfg), ErrBuiltinCommandName)

	after, err := g.LoadManifest()
	require.NoError(t, err)
	assert.Equal(t, before, after, "a refusal writes nothing")

	require.ErrorIs(t, ApplyAuthorSettingsTo(after, cfg), ErrBuiltinCommandName, "a dry run refuses it too")
}

func TestCheckEnabledFeatureNames(t *testing.T) {
	t.Parallel()

	on := func(n string) []ManifestFeature { return []ManifestFeature{{Name: n, Enabled: true}} }
	cmds := []ManifestCommand{{Name: "config"}, {Name: "update"}, {Name: "group", Commands: []ManifestCommand{{Name: "man", Aliases: []string{"changelog"}}}}, {Name: "apply", Aliases: []string{"ap", "docs"}}}

	err := checkEnabledFeatureNames([]ManifestFeature{{Name: "docs"}}, on("docs"), cmds)
	require.ErrorIs(t, err, ErrBuiltinCommandName, "an alias holds its name on the root")
	assert.Contains(t, errors.FlattenHints(err), "apply command has the alias docs")
	require.NoError(t, checkEnabledFeatureNames([]ManifestFeature{{Name: "changelog"}}, on("changelog"), cmds), "a nested command's alias does not")

	require.ErrorIs(t, checkEnabledFeatureNames(nil, on("config"), cmds), ErrBuiltinCommandName)
	require.NoError(t, checkEnabledFeatureNames(nil, on("man"), cmds), "a nested command holds no name on the root")
	require.NoError(t, checkEnabledFeatureNames(nil, on("telemetry"), cmds))
	require.NoError(t, checkEnabledFeatureNames(nil, on("ai"), []ManifestCommand{{Name: "ai"}}), "a feature with no command holds no name")
	require.NoError(t, checkEnabledFeatureNames(on("config"), on("config"), cmds), "a feature already on is not turned on")
	require.NoError(t, checkEnabledFeatureNames(on("config"), nil, cmds), "turning it off frees the name")
}

func TestWarnBuiltinCommandNames(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	g := builtinNamesProject(t, log, Manifest{}, Config{})

	g.warnBuiltinCommandNames(&Manifest{Commands: []ManifestCommand{{Name: "update"}, {Name: "version"}, {Name: "config"}, {Name: "deploy"}, {Name: "apply", Aliases: []string{"docs", "man"}}}})

	assert.True(t, log.ContainsLevel(logger.WarnLevel, `"update"`))
	assert.True(t, log.ContainsLevel(logger.WarnLevel, "gtb disable update"))
	assert.True(t, log.ContainsLevel(logger.WarnLevel, `"version"`))
	assert.False(t, log.ContainsLevel(logger.WarnLevel, `"config"`), "config is off by default")
	assert.False(t, log.ContainsLevel(logger.WarnLevel, `"deploy"`))
	assert.True(t, log.ContainsLevel(logger.WarnLevel, `the alias "docs" of the top-level command "apply"`))
	assert.False(t, log.ContainsLevel(logger.WarnLevel, `"man"`), "man is off by default")
}

func TestWarnBuiltinCommandNames_GoesByTheProjectsFeatures(t *testing.T) {
	t.Parallel()

	log := logger.NewBuffer()
	g := builtinNamesProject(t, log, Manifest{}, Config{})

	g.warnBuiltinCommandNames(&Manifest{
		Properties: ManifestProperties{Features: []ManifestFeature{{Name: "update"}, {Name: "config", Enabled: true}}},
		Commands:   []ManifestCommand{{Name: "update"}, {Name: "config"}},
	})

	assert.False(t, log.ContainsLevel(logger.WarnLevel, `"update"`))
	assert.True(t, log.ContainsLevel(logger.WarnLevel, `"config"`))
}

func TestRegenerateProject_WarnsOfABuiltinsName(t *testing.T) {
	t.Parallel()

	g, _, _ := newSettingsSkeletonProject(t)
	log := logger.NewBuffer()
	g.props.Logger = log

	m, err := g.LoadManifest()
	require.NoError(t, err)

	m.Commands = append(m.Commands, ManifestCommand{Name: "update", Description: "my own"})
	require.NoError(t, g.writeManifest(m))
	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.True(t, log.ContainsLevel(logger.WarnLevel, "gtb disable update"))
}

// The names are stated rather than derived, so this holds them to the commands
// the framework's root attaches with every scaffoldable feature on.
func TestBuiltinCommandNames_MatchTheRoot(t *testing.T) {
	t.Parallel()

	var states []props.FeatureState
	for _, d := range templates.Catalogue() {
		states = append(states, props.Enable(d.ID))
	}

	p, err := props.New(props.Tool{Name: "w", Features: props.SetFeatures(states...)}, logger.NewNoop(), afero.NewMemMapFs())
	require.NoError(t, err)

	got := map[string]bool{}
	for _, c := range root.NewCmdRoot(p).Commands() {
		got[c.Name()] = true
	}

	want := map[string]bool{"version": true}
	for name := range featureCommandNames {
		want[name] = true
	}

	assert.Equal(t, want, got)
}
