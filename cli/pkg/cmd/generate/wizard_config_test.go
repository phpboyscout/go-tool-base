package generate

import (
	"testing"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
)

// finishWizard accepts every remaining page as offered.
func finishWizard(t *testing.T, f *huh.Form, m huh.Model) {
	t.Helper()

	for i := 0; i < 60 && f.State != huh.StateCompleted; i++ {
		var ok bool
		if m, ok = advance(f, m); !ok {
			break
		}
	}

	require.Equal(t, huh.StateCompleted, f.State)
}

// Spec 0204 D9: the Configuration page asks nothing a tool must answer, so
// accepting it declares no format, no own format and no source.
func TestWizard_ConfigurationPageDefaultsDeclareNothing(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f := driveToCompletion(t, o)
	require.Equal(t, huh.StateCompleted, f.State)
	require.NoError(t, o.afterWizard())

	assert.Empty(t, o.ConfigFormats)
	assert.Empty(t, o.ConfigFormat)
	assert.Empty(t, o.ConfigSources)
	assert.Empty(t, o.ConfigLayers)
}

func TestWizard_ConfigurationPageFormats(t *testing.T) {
	t.Parallel()

	t.Run("the formats are chosen", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app"}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-formats")
		require.Equal(t, "config-formats", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "x") // toml, the first row
		m, ok := advance(f, m)
		require.True(t, ok)

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())

		assert.Equal(t, []string{"toml"}, o.ConfigFormats)
		assert.Empty(t, o.ConfigFormat, "YAML stays the own format unless chosen")
	})

	// The own format's options follow the formats through a command the
	// test drive does not run, so they are given up front, as on the AI page.
	t.Run("the own format is one of them", func(t *testing.T) {
		t.Parallel()

		o := &SkeletonOptions{Repo: "org/my-app", ConfigFormats: []string{"toml", "ini"}}
		f, m := startWizard(o)
		m = driveUntilKey(f, m, o, "config-format")
		require.Equal(t, "config-format", f.GetFocusedField().GetKey())

		m = typeAnswer(m, "↓") // yaml, toml; ini is read-only and not offered
		m, ok := advance(f, m)
		require.True(t, ok)

		finishWizard(t, f, m)
		require.NoError(t, o.afterWizard())

		assert.Equal(t, "toml", o.ConfigFormat)
	})
}

// A slot is a kind and a name; the order is placed against the built-in
// layers, and required and writable are per slot (D6, D7).
func TestWizard_ConfigurationPageSources(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	require.Equal(t, "config-source-0-kind", f.GetFocusedField().GetKey())

	m = typeAnswer(m, "↓↓↓↓") // none, file, keychain, vault, consul
	m, ok := advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-name", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "team")
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-required", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "l")
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-access", f.GetFocusedField().GetKey())
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-0-placement", f.GetFocusedField().GetKey())
	m = typeAnswer(m, "↓") // above the user's files
	m, ok = advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-1-kind", f.GetFocusedField().GetKey(), "another slot may be added")

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"team=consul"}, o.ConfigSources)
	assert.Equal(t, []string{"team"}, o.ConfigSourcesOptional)
	assert.Empty(t, o.ConfigSourcesWritable)
	assert.Equal(t, []string{"defaults", "files", "team", "project", "env", "flags"}, o.ConfigLayers)
	require.NoError(t, o.validateFields())
}

// A slot left at the default placement records no layer list: R9 places it.
func TestWizard_ADefaultPlacedSourceRecordsNoLayers(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, "↓↓↓") // vault

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"vault=vault"}, o.ConfigSources, "the name defaults to the kind")
	assert.Empty(t, o.ConfigLayers)
}

func TestWizard_ASlotNameMustBeUnique(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, "↓↓↓")
	m = driveUntilKey(f, m, o, "config-source-1-kind")
	m = typeAnswer(m, "↓↓↓")
	m, ok := advance(f, m)
	require.True(t, ok)

	require.Equal(t, "config-source-1-name", f.GetFocusedField().GetKey())
	_, ok = advance(f, m)
	assert.False(t, ok, "a second vault slot needs its own name")
}

// The keychain is the one kind writable by default (D12): the access row
// offers that default first.
func TestWizard_AKeychainSlotIsWritableByDefault(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Repo: "org/my-app"}
	f, m := startWizard(o)
	m = driveUntilKey(f, m, o, "config-source-0-kind")
	m = typeAnswer(m, "↓↓") // keychain

	finishWizard(t, f, m)
	require.NoError(t, o.afterWizard())

	assert.Equal(t, []string{"keychain=keychain"}, o.ConfigSources)
	assert.Empty(t, o.ConfigSourcesWritable, "the kind's default is recorded as nothing")
	assert.Empty(t, o.readOnlySources)
}

// D13 of spec 0197 for this page: a revisit pre-fills every slot and its
// placement, and accepting them changes nothing.
func TestWizard_ConfigurationPageRevisitChangesNothing(t *testing.T) {
	t.Parallel()

	start := &SkeletonOptions{
		Name: "tool", Repo: "org/tool", ForgeBackend: "github", Features: generator.DefaultSelectedFeatures,
		ConfigFormats: []string{"toml"}, ConfigFormat: "toml",
		ConfigLayers:  []string{"defaults", "files", "team", "project", "env", "tokens", "flags"},
		ConfigSources: []string{"team=consul", "tokens=keychain"}, ConfigSourcesOptional: []string{"team"},
	}
	start.readOnlySources = []string{"tokens"}
	require.NoError(t, start.validateFields())

	m := generator.ManifestFromSkeletonConfig(start.skeletonConfig(nil), nil, "v1.0.0")
	o := optionsFromManifest(m)

	f := o.wizardForm()
	f.Update(f.Init())

	var model huh.Model = f
	finishWizard(t, f, model)
	require.NoError(t, o.afterWizard())

	again := generator.ManifestFromSkeletonConfig(o.skeletonConfig(nil), nil, "v1.0.0")
	assert.Equal(t, m.Properties.Config, again.Properties.Config)
}
