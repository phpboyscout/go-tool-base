package setup_test

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func nopFactory(context.Context, config.Reader, setup.ConfigBootstrap) (config.Backend, error) {
	return nil, nil //nolint:nilnil // a stand-in the registry never calls
}

func TestConfigSourceDescriptor_IsALink(t *testing.T) {
	t.Parallel()

	d := setup.ConfigSourceDescriptor("vault")
	assert.Equal(t, props.FeatureID("config-source-vault"), d.ID)
	assert.Equal(t, props.KindLink, d.Kind)
	assert.True(t, d.Default)
}

// Spec 0204 D3: a kind's link package registers its factory and its
// initialiser, and the root finds every linked kind by the import alone.
func TestConfigSourceKindsIn(t *testing.T) {
	t.Parallel()

	reg := features.NewRegistry()
	require.NoError(t, reg.Declare(setup.ConfigSourceDescriptor("vault")))
	require.NoError(t, reg.Declare(setup.ConfigSourceDescriptor("keychain")))
	setup.RegisterConfigSourceKindOn(reg, "vault", nopFactory, nil)
	setup.RegisterConfigSourceKindOn(reg, "keychain", nopFactory, nil, setup.WritableByDefault())

	set, err := features.Resolve(reg.Snapshot(), nil)
	require.NoError(t, err)

	kinds := setup.ConfigSourceKindsIn(set)
	require.Len(t, kinds, 2)
	assert.NotNil(t, kinds["vault"].Factory)
	assert.False(t, kinds["vault"].WritableByDefault)
	assert.True(t, kinds["keychain"].WritableByDefault, "the keychain is writable by design (D12)")
}

// Spec 0204 D19: an override replaces the factory for one named slot, and
// several can be registered without their feature colliding.
func TestConfigSourceOverridesIn(t *testing.T) {
	t.Parallel()

	reg := features.NewRegistry()
	require.NoError(t, setup.OverrideConfigSourceOn(reg, "team", nopFactory))
	require.NoError(t, setup.OverrideConfigSourceOn(reg, "etcd", nopFactory))

	set, err := features.Resolve(reg.Snapshot(), nil)
	require.NoError(t, err)

	overrides := setup.ConfigSourceOverridesIn(set)
	assert.Len(t, overrides, 2)
	assert.Contains(t, overrides, "team")
	assert.Contains(t, overrides, "etcd")
}

func TestIsOverrideOnlyKind(t *testing.T) {
	t.Parallel()

	for _, k := range []string{"etcd", "sftp", "billy", "iofs", "afero"} {
		assert.Truef(t, setup.IsOverrideOnlyKind(k), "%s", k)
	}

	assert.False(t, setup.IsOverrideOnlyKind("vault"))
}

// An unconfigured required slot names the command that configures it.
func TestConfigSourceUnconfiguredError(t *testing.T) {
	t.Parallel()

	p := &props.Props{Tool: props.Tool{Name: "mytool"}, Logger: logger.NewNoop()}
	err := setup.ConfigSourceUnconfiguredError(p, props.ConfigSource{Name: "team", Kind: "consul"})

	require.ErrorIs(t, err, setup.ErrConfigSourceUnconfigured)
	assert.Contains(t, err.Error(), "team")
	assert.Contains(t, errors.FlattenHints(err), "mytool init config team")
}

// Spec 0204 D3: a kind's initialiser asks for its settings and writes them
// under config.sources.<slot name>; an empty optional answer writes nothing.
func TestSettingsInitialiser(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Tool:   props.Tool{Name: "mytool"},
		Logger: logger.NewNoop(),
		IO:     props.StdIO{Stdin: formtest.Answers("/srv/config", ""), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true},
	}

	init := setup.SettingsInitialiser(props.ConfigSource{Name: "platform", Kind: "filekv"},
		setup.SourceSetting{Key: "dir", Title: "Directory", Required: true},
		setup.SourceSetting{Key: "prefix", Title: "Key prefix"})

	editor := &recordingEditor{}
	require.NoError(t, init.Configure(t.Context(), p, editor))
	assert.Equal(t, map[string]any{"config.sources.platform.dir": "/srv/config"}, editor.set)
	assert.Equal(t, "platform", init.Name())
}

// recordingEditor is a setup.Editor that records what was written.
type recordingEditor struct{ set map[string]any }

func (e *recordingEditor) View() *config.View { return nil }

func (e *recordingEditor) Set(key string, value any) error {
	if e.set == nil {
		e.set = map[string]any{}
	}

	e.set[key] = value

	return nil
}

func (e *recordingEditor) Apply(...config.Change) error { return nil }

// A setting's default fills the answer when nothing is configured, so
// accepting it writes the default.
func TestSettingsInitialiser_Default(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		Tool:   props.Tool{Name: "mytool"},
		Logger: logger.NewNoop(),
		IO:     props.StdIO{Stdin: formtest.Answers(""), Stdout: io.Discard, Stderr: io.Discard, AccessibleMode: true},
	}

	init := setup.SettingsInitialiser(props.ConfigSource{Name: "tokens", Kind: "keychain"},
		setup.SourceSetting{Key: "service", Title: "Service", Default: "mytool", Required: true})

	editor := &recordingEditor{}
	require.NoError(t, init.Configure(t.Context(), p, editor))
	assert.Equal(t, "mytool", editor.set["config.sources.tokens.service"])
	assert.False(t, init.IsConfigured(emptyReader(t)))
}

func emptyReader(t *testing.T) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "empty"}))
	require.NoError(t, err)

	return store.View()
}
