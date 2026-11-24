package generator

import (
	"context"
	"fmt"
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

	with := func(before []ManifestFeature, commands []ManifestCommand) *Manifest {
		return &Manifest{Properties: ManifestProperties{Features: before}, Commands: commands}
	}

	err := checkEnabledFeatureNames(with([]ManifestFeature{{Name: "docs"}}, cmds), on("docs"))
	require.ErrorIs(t, err, ErrBuiltinCommandName, "an alias holds its name on the root")
	assert.Contains(t, errors.FlattenHints(err), "apply command has the alias docs")
	require.NoError(t, checkEnabledFeatureNames(with([]ManifestFeature{{Name: "changelog"}}, cmds), on("changelog")), "a nested command's alias does not")

	require.ErrorIs(t, checkEnabledFeatureNames(with(nil, cmds), on("config")), ErrBuiltinCommandName)
	require.NoError(t, checkEnabledFeatureNames(with(nil, cmds), on("man")), "a nested command holds no name on the root")
	require.NoError(t, checkEnabledFeatureNames(with(nil, cmds), on("telemetry")))
	require.NoError(t, checkEnabledFeatureNames(with(nil, []ManifestCommand{{Name: "ai"}}), on("ai")), "a feature with no command holds no name")
	require.NoError(t, checkEnabledFeatureNames(with(on("config"), cmds), on("config")), "a feature already on is not turned on")
	require.NoError(t, checkEnabledFeatureNames(with(on("config"), cmds), nil), "turning it off frees the name")
}

// An attached command's declared name is not taken later (#249). The holder sits
// in the middle of the modules and of its module's constructors, so every entry
// has to be read.
func TestAnAttachedNameIsNotTakenLater(t *testing.T) {
	t.Parallel()

	project := func(t *testing.T, cfg Config) *Generator {
		t.Helper()

		return builtinNamesProject(t, logger.NewNoop(), Manifest{
			Properties: ManifestProperties{
				Features: []ManifestFeature{{Name: "update"}},
				ExternalCommands: []ManifestExternalCommand{{
					Module: "example.com/notes", Version: "v2.0.0",
					Attach: []ManifestExternalAttach{{Constructor: "NewCmdNotes", Name: "notes"}},
				}, {
					Module: "example.com/release-tools", Version: "v1.0.0",
					Attach: []ManifestExternalAttach{{Constructor: "NewCmdOther"}, {Constructor: "NewCmdSelf", Name: "update"}, {Constructor: "NewCmdAfter", Name: "after"}},
				}, {
					Module: "example.com/tail", Version: "v1.0.0",
					Attach: []ManifestExternalAttach{{Constructor: "NewCmdTail", Name: "tail"}},
				}},
			},
			Commands: []ManifestCommand{{Name: "group"}, {Name: "both", Aliases: []string{"update"}}},
		}, cfg)
	}

	for name, tc := range map[string]struct {
		cfg     Config
		refused bool
	}{
		"as a name":                {cfg: Config{Name: "update"}, refused: true},
		"as an alias":              {cfg: Config{Name: "apply", Aliases: []string{"update"}}, refused: true},
		"in a dry run":             {cfg: Config{Name: "update", DryRun: true}, refused: true},
		"under a parent":           {cfg: Config{Name: "update", Parent: "group"}},
		"an alias the command has": {cfg: Config{Name: "both"}},
		"another name":             {cfg: Config{Name: "apply"}},
	} {
		t.Run("a command takes it "+name, func(t *testing.T) {
			t.Parallel()

			_, err := project(t, tc.cfg).prepareAndVerify()
			if !tc.refused {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrAttachedCommandName)
			assert.Contains(t, errors.FlattenHints(err), "NewCmdSelf of example.com/release-tools")
		})
	}

	t.Run("a feature takes it", func(t *testing.T) {
		t.Parallel()

		g := project(t, Config{})
		before, err := afero.ReadFile(g.props.FS, "/work/.gtb/manifest.yaml")
		require.NoError(t, err)

		_, err = g.ApplyFeatures(context.Background(), map[string]bool{"update": true})
		require.ErrorIs(t, err, ErrBuiltinCommandName)
		assert.Contains(t, errors.FlattenHints(err), "NewCmdSelf")
		assert.Contains(t, errors.FlattenHints(err), "gtb detach command example.com/release-tools")

		m, err := g.LoadManifest()
		require.NoError(t, err)

		cfg := SkeletonConfigFromManifest(*m)
		cfg.Features = []ManifestFeature{{Name: "update", Enabled: true}}
		require.ErrorIs(t, g.ApplyAuthorSettings(context.Background(), cfg), ErrBuiltinCommandName)

		after, err := afero.ReadFile(g.props.FS, "/work/.gtb/manifest.yaml")
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
	})

	t.Run("another feature is turned on", func(t *testing.T) {
		t.Parallel()

		m, err := project(t, Config{}).LoadManifest()
		require.NoError(t, err)
		m.Commands = nil
		require.NoError(t, checkEnabledFeatureNames(m, []ManifestFeature{{Name: "update"}, {Name: "config", Enabled: true}}))
	})

	for name, tc := range map[string]struct {
		attach []ManifestExternalAttach
		hint   string
	}{
		"another module's":     {attach: []ManifestExternalAttach{{Constructor: "NewCmdA", Name: "update"}}, hint: "NewCmdSelf of example.com/release-tools"},
		"one beside it":        {attach: []ManifestExternalAttach{{Constructor: "NewCmdA", Name: "ship"}, {Constructor: "NewCmdB", Name: "ship"}}, hint: "NewCmdB"},
		"several declare none": {attach: []ManifestExternalAttach{{Constructor: "NewCmdA"}, {Constructor: "NewCmdB"}}},
	} {
		t.Run("an attachment takes "+name, func(t *testing.T) {
			t.Parallel()

			m, err := project(t, Config{}).LoadManifest()
			require.NoError(t, err)

			err = checkAttachedNames(tc.attach, m)
			if tc.hint == "" {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrAttachedCommandName)
			assert.Contains(t, errors.FlattenHints(err), tc.hint)
		})
	}
}

// Attaching a command again is reported as that, not as a clash with itself.
func TestAttachingACommandAgainIsNotANameClash(t *testing.T) {
	t.Parallel()

	const module = "example.com/release-tools"

	for name, tc := range map[string]struct {
		spec ExternalCommandSpec
		want string
		ok   bool
	}{
		"at another version":  {spec: ExternalCommandSpec{Module: module, Version: "v1.1.0", Attach: []ManifestExternalAttach{{Constructor: "NewCmdSelf", Name: "self"}}}, want: "already attached at v1.0.0"},
		"at the same version": {spec: ExternalCommandSpec{Module: module, Version: "v1.0.0", Attach: []ManifestExternalAttach{{Constructor: "NewCmdSelf", Name: "self"}}}, want: "duplicate"},
		"another constructor": {spec: ExternalCommandSpec{Module: module, Version: "v1.0.0", Attach: []ManifestExternalAttach{{Constructor: "NewCmdB", Name: "self"}}}, want: "NewCmdSelf of " + module},
		"another name":        {spec: ExternalCommandSpec{Module: module, Version: "v1.0.0", Attach: []ManifestExternalAttach{{Constructor: "NewCmdB", Name: "ship"}}}, ok: true},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			g, _ := newFeatureProject(t)
			require.NoError(t, g.AttachExternalCommand(context.Background(), ExternalCommandSpec{
				Module: module, Version: "v1.0.0", Attach: []ManifestExternalAttach{{Constructor: "NewCmdSelf", Name: "self"}},
			}))

			err := g.AttachExternalCommand(context.Background(), tc.spec)
			if tc.ok {
				require.NoError(t, err)

				m, err := g.LoadManifest()
				require.NoError(t, err)
				require.Len(t, m.Properties.ExternalCommands, 1)
				assert.Len(t, m.Properties.ExternalCommands[0].Attach, 2)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error()+" "+errors.FlattenHints(err), tc.want)
		})
	}
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

// Wherever the holder sits among the modules and their constructors, it is found.
func TestAnAttachedNameIsFoundAtAnyPosition(t *testing.T) {
	t.Parallel()

	for module := range 3 {
		for attach := range 3 {
			m := &Manifest{Properties: ManifestProperties{Features: []ManifestFeature{{Name: "update"}}}}

			for i := range 3 {
				ec := ManifestExternalCommand{Module: fmt.Sprintf("example.com/m%d", i), Version: "v1.0.0"}

				for j := range 3 {
					a := ManifestExternalAttach{Constructor: fmt.Sprintf("NewCmd%d%d", i, j), Name: fmt.Sprintf("n%d%d", i, j)}
					if i == module && j == attach {
						a.Name = "update"
					}

					ec.Attach = append(ec.Attach, a)
				}

				m.Properties.ExternalCommands = append(m.Properties.ExternalCommands, ec)
			}

			holder, held := attachmentDeclaring(m, "update")
			require.True(t, held, "module %d, constructor %d", module, attach)
			assert.Equal(t, fmt.Sprintf("NewCmd%d%d", module, attach), holder.constructor)

			err := checkEnabledFeatureNames(m, []ManifestFeature{{Name: "update", Enabled: true}})
			require.ErrorIs(t, err, ErrBuiltinCommandName, "module %d, constructor %d", module, attach)
		}
	}
}
