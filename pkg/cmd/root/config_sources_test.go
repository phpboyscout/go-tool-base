package root

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	configafero "gitlab.com/phpboyscout/go/config-afero"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// memfileFactory is a fake kind: its slot's settings name a YAML file, and
// the backend reads it. A plain file backend is writable and watchable, which
// is what the read-only and watch tests need.
func memfileFactory(fs afero.Fs) setup.SourceFactory {
	return func(_ context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
		return config.NewCodecBackend(configafero.Wrap(fs), settings.GetString("path"), config.YAMLCodec{}), nil
	}
}

func brokenFactory(context.Context, config.Reader, setup.ConfigBootstrap) (config.Backend, error) {
	return nil, errors.New("connection refused")
}

// unwatchable is a file backend whose Watch always fails, as a flaky remote's
// subscription would.
type unwatchable struct{ config.Backend }

func (unwatchable) Watch(context.Context, time.Duration, func()) (func(), error) {
	return nil, errors.New("subscription refused")
}

type sourcedTool struct {
	fs        afero.Fs
	sources   []p.ConfigSource
	layers    []p.ConfigLayer
	overrides map[string]setup.SourceFactory
	log       logger.Logger
}

func (st sourcedTool) props(t *testing.T) *p.Props {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	for kind, f := range map[string]setup.SourceFactory{
		"memfile": memfileFactory(st.fs),
		"broken":  brokenFactory,
		"flaky": func(ctx context.Context, s config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
			inner, err := memfileFactory(st.fs)(ctx, s, b)

			return unwatchable{inner}, err
		},
	} {
		require.NoError(t, reg.Declare(setup.ConfigSourceDescriptor(kind)))
		setup.RegisterConfigSourceKindOn(reg, kind, f, nil)
	}

	for name, f := range st.overrides {
		require.NoError(t, setup.OverrideConfigSourceOn(reg, name, f))
	}

	log := st.log
	if log == nil {
		log = logger.NewNoop()
	}

	tool := p.Tool{Name: "mytool", Config: p.ConfigSpec{Sources: st.sources, Layers: st.layers}}

	props, err := p.New(tool, log, st.fs, p.WithFeatures(reg.Snapshot()))
	require.NoError(t, err)

	return props
}

func (st sourcedTool) build(t *testing.T) (*config.Store, error) {
	t.Helper()

	return buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:      st.props(t),
		CfgPaths:   []string{"/cfg/config.yaml"},
		AllowEmpty: true,
	})
}

func stackWith(source string, below p.ConfigLayer) []p.ConfigLayer {
	stack := []p.ConfigLayer{p.LayerDefaults}
	for _, l := range []p.ConfigLayer{p.LayerFiles, p.LayerEnv, p.LayerFlags} {
		if l == below {
			stack = append(stack, p.ConfigLayer(source))
		}

		stack = append(stack, l)
	}

	return stack
}

func writeFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0o600))
}

const teamConfigured = "log:\n  level: warn\nconfig:\n  sources:\n    team:\n      path: /src/team.yaml\n"

// Spec 0204 D1, D4: a configured source is a layer in its declared place.
func TestSources_ASourceTakesItsDeclaredPlace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		below p.ConfigLayer
		want  string
	}{
		{name: "above the user's file", below: p.LayerEnv, want: "debug"},
		{name: "below it", below: p.LayerFiles, want: "warn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
			writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

			store, err := sourcedTool{fs: fs, sources: []p.ConfigSource{{Name: "team", Kind: "memfile"}}, layers: stackWith("team", tc.below)}.build(t)
			require.NoError(t, err)
			assert.Equal(t, tc.want, store.View().GetString("log.level"))
		})
	}
}

// Spec 0204 D5: the view a factory reads holds the user's file and not the
// project file, structurally, whatever the trust filter does.
func TestSources_TheBootstrapViewExcludesTheProjectFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	fs := afero.NewOsFs()
	dir := t.TempDir()
	user := filepath.Join(dir, "config.yaml")
	project := filepath.Join(dir, ".mytool.yaml")
	writeFile(t, fs, user, "user_marker: yes\nconfig:\n  sources:\n    team:\n      path: "+filepath.Join(dir, "team.yaml")+"\n")
	writeFile(t, fs, project, "project_marker: yes\n")

	var seen *config.View

	st := sourcedTool{
		fs:      fs,
		sources: []p.ConfigSource{{Name: "team", Kind: "memfile"}},
		layers:  []p.ConfigLayer{p.LayerDefaults, p.LayerFiles, "team", p.LayerProject, p.LayerEnv, p.LayerFlags},
		overrides: map[string]setup.SourceFactory{"team": func(_ context.Context, _ config.Reader, b setup.ConfigBootstrap) (config.Backend, error) {
			seen = b.View()

			return config.NewReaderBackend("team", nil), nil
		}},
	}

	_, err := buildConfigStore(t.Context(), ConfigLoadOptions{Props: st.props(t), CfgPaths: []string{user}, ProjectConfigPath: project})
	require.NoError(t, err)
	require.NotNil(t, seen)
	assert.True(t, seen.IsSet("user_marker"))
	assert.False(t, seen.IsSet("project_marker"), "a repository cannot reach what a factory reads")
}

// Spec 0204 D6: absence is fatal unless the slot says otherwise.
func TestSources_Absence(t *testing.T) {
	t.Parallel()

	no := false

	tests := []struct {
		name     string
		kind     string
		required *bool
		userFile string
		want     error
	}{
		{name: "required and never configured", kind: "memfile", userFile: "log:\n  level: warn\n", want: setup.ErrConfigSourceUnconfigured},
		{name: "optional and never configured", kind: "memfile", required: &no, userFile: "log:\n  level: warn\n"},
		{name: "required and unreachable", kind: "broken", userFile: teamConfigured, want: setup.ErrConfigSourceUnavailable},
		{name: "optional and unreachable", kind: "broken", required: &no, userFile: teamConfigured},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", tc.userFile)

			log := logger.NewBuffer()
			store, err := sourcedTool{
				fs: fs, log: log,
				sources: []p.ConfigSource{{Name: "team", Kind: tc.kind, Required: tc.required}},
				layers:  stackWith("team", p.LayerEnv),
			}.build(t)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				assert.Contains(t, err.Error(), "team")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "warn", store.View().GetString("log.level"), "the stack resolves without it")
			assert.True(t, logged(log.Entries(), "team"), "and says so")
		})
	}
}

// Spec 0204 D7: a source is never a write target unless it says so.
func TestSources_ReadOnlyUnlessWritable(t *testing.T) {
	t.Parallel()

	yes := true

	for _, tc := range []struct {
		name     string
		writable *bool
		target   string
	}{
		{name: "by default", target: "/cfg/config.yaml"},
		{name: "declared writable", writable: &yes, target: "/src/team.yaml"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
			writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

			store, err := sourcedTool{fs: fs,
				sources: []p.ConfigSource{{Name: "team", Kind: "memfile", Writable: tc.writable}},
				layers:  stackWith("team", p.LayerEnv)}.build(t)
			require.NoError(t, err)

			plan, err := store.Plan(config.Set("log.level", "info"))
			require.NoError(t, err)
			require.NotEmpty(t, plan.Operations)
			assert.Equal(t, tc.target, plan.Operations[0].Target.Name)
		})
	}
}

// Spec 0204 D19: an override replaces the factory for its slot and is the
// only way to build an override-only kind; one for an undeclared slot, or an
// override-only slot with none, is refused.
func TestSources_Overrides(t *testing.T) {
	t.Parallel()

	fromOverride := func(context.Context, config.Reader, setup.ConfigBootstrap) (config.Backend, error) {
		return config.NewReaderBackend("override", []byte("log:\n  level: error\n")), nil
	}

	tests := []struct {
		name      string
		kind      string
		overrides map[string]setup.SourceFactory
		want      error
		level     string
	}{
		{name: "an override wins over the kind's factory", kind: "memfile", overrides: map[string]setup.SourceFactory{"team": fromOverride}, level: "error"},
		{name: "an override builds an override-only kind", kind: "etcd", overrides: map[string]setup.SourceFactory{"team": fromOverride}, level: "error"},
		{name: "an override-only kind with none", kind: "etcd", want: setup.ErrConfigSourceNeedsOverride},
		{name: "a kind the binary does not link", kind: "vault", want: setup.ErrConfigSourceKindNotLinked},
		{name: "an override for an undeclared slot", kind: "memfile", overrides: map[string]setup.SourceFactory{"ghost": fromOverride}, want: setup.ErrConfigSourceOverrideUndeclared},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
			writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

			store, err := sourcedTool{fs: fs, overrides: tc.overrides,
				sources: []p.ConfigSource{{Name: "team", Kind: tc.kind}},
				layers:  stackWith("team", p.LayerEnv)}.build(t)

			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.level, store.View().GetString("log.level"))
		})
	}
}

// Spec 0204 D11: an optional source that cannot be watched drops out of
// watching alone; a required one still fails the watch, as any layer would.
func TestSources_AnUnwatchableOptionalSourceDoesNotStopTheWatch(t *testing.T) {
	t.Parallel()

	no := false

	for _, tc := range []struct {
		name     string
		required *bool
		wantErr  bool
	}{
		{name: "optional", required: &no},
		{name: "required", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
			writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

			log := logger.NewBuffer()
			store, err := sourcedTool{fs: fs, log: log,
				sources: []p.ConfigSource{{Name: "team", Kind: "flaky", Required: tc.required}},
				layers:  stackWith("team", p.LayerEnv)}.build(t)
			require.NoError(t, err)

			stop, err := store.Watch(t.Context())
			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			stop()
			assert.True(t, logged(log.Entries(), "team"), "the source that dropped out is named")
		})
	}
}

// wrapSource implements exactly the optional interfaces it should: writable
// only when declared so and the backend can be, watchable whenever the
// backend is, and the forwarded methods still reach the backend.
func TestWrapSource_Capabilities(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/src/team.yaml", "a: 1\n")

	file := func() config.Backend {
		return config.NewCodecBackend(configafero.Wrap(fs), "/src/team.yaml", config.YAMLCodec{})
	}

	tests := []struct {
		name               string
		backend            config.Backend
		writable, optional bool
		wantWrite          bool
		wantWatch          bool
	}{
		{name: "required, declared writable: untouched", backend: file(), writable: true, wantWrite: true, wantWatch: true},
		{name: "optional and writable", backend: file(), writable: true, optional: true, wantWrite: true, wantWatch: true},
		{name: "required, read-only", backend: file(), wantWatch: true},
		{name: "optional, read-only", backend: file(), optional: true, wantWatch: true},
		{name: "a reader declared writable cannot be written", backend: config.NewReaderBackend("r", nil), writable: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := wrapSource(tc.backend, tc.writable, tc.optional, logger.NewNoop(), "team")

			w, isWritable := got.(config.WritableBackend)
			_, isWatchable := got.(config.WatchableBackend)
			assert.Equal(t, tc.wantWrite, isWritable)
			assert.Equal(t, tc.wantWatch, isWatchable)
			assert.Equal(t, tc.backend.ID(), got.ID(), "writes route back by ID")
			assert.Equal(t, tc.backend.Capabilities(), got.Capabilities(), "the leak guard reads Capabilities")

			if d, ok := got.(config.SourceKindDeclarer); ok {
				assert.NotEmpty(t, d.SourceKind())
			}

			if isWritable {
				pending, err := w.Prepare(t.Context(), []config.Edit{{Path: "a", Value: 2}})
				require.NoError(t, err)
				require.NoError(t, pending.Discard(t.Context()))
			}

			if wa, ok := got.(config.WatchableBackend); ok {
				stop, err := wa.Watch(t.Context(), time.Hour, func() {})
				require.NoError(t, err)
				stop()
			}
		})
	}
}

// A command that opts out of the config check (doctor, config convert) must
// run on a tool whose required source is not configured: it is how the user
// finds out and puts it right. Every other command is refused.
func TestSources_CommandsThatSkipTheConfigCheckTreatSourcesAsOptional(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		skip bool
	}{
		{name: "an ordinary command is refused"},
		{name: "a command that skips the check runs", skip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			writeFile(t, fs, "/cfg/config.yaml", "log:\n  level: warn\n")

			props := sourcedTool{fs: fs, sources: []p.ConfigSource{{Name: "team", Kind: "memfile"}}, layers: stackWith("team", p.LayerEnv)}.props(t)

			cmd := newConfigFlagCmd(t)
			cmd.SetContext(t.Context())

			if tc.skip {
				setup.SkipConfigCheck(cmd)
			}

			_, err := resolveBootstrapConfig(props, cmd, nil, []string{"/cfg/config.yaml"}, nil)
			if tc.skip {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, setup.ErrConfigSourceUnconfigured)
		})
	}
}

// --config names files; it is never configuration. Bound by the hyphen-to-dot
// rule it became the key "config", a list of paths at the highest precedence,
// and hid the whole config.* subtree, config.sources with it.
func TestBuildConfigStore_TheConfigFlagIsNotAConfigKey(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	writeFile(t, fs, "/cfg/config.yaml", teamConfigured)
	writeFile(t, fs, "/src/team.yaml", "log:\n  level: debug\n")

	cmd := newConfigFlagCmd(t, "/cfg/config.yaml")

	store, err := buildConfigStore(t.Context(), ConfigLoadOptions{
		Props:    sourcedTool{fs: fs, sources: []p.ConfigSource{{Name: "team", Kind: "memfile"}}, layers: stackWith("team", p.LayerEnv)}.props(t),
		CfgPaths: []string{"/cfg/config.yaml"},
		Flags:    cmd.Flags(),
	})
	require.NoError(t, err)

	view := store.View()
	assert.Equal(t, "/src/team.yaml", view.GetString("config.sources.team.path"))
	assert.Equal(t, "debug", view.GetString("log.level"), "the source was built from its settings")
}
