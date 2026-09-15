package generate

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"charm.land/huh/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// keysSeen drives the form to completion the way driveToCompletion does and
// records the key of every focused field on the way, so a test can assert
// which pages were shown. answer is consulted when a keyed field is focused.
func keysSeen(t *testing.T, o *SkeletonOptions, answer map[string]string) (*huh.Form, []string) {
	t.Helper()

	f := o.wizardForm()
	f.Update(f.Init())

	var (
		m    huh.Model = f
		seen []string
	)

	for _, r := range "my-app" {
		m, _ = m.Update(keypress(r))
	}

	for i := 0; i < 60 && f.State != huh.StateCompleted; i++ {
		field := f.GetFocusedField()
		if field == nil {
			break
		}

		key := field.GetKey()
		if key != "" {
			seen = append(seen, key)
		}

		if text, ok := answer[key]; ok && key != "" {
			m = typeAnswer(m, text)
		}

		var ok bool
		if m, ok = advance(f, m); !ok {
			break
		}
	}

	return f, seen
}

// TestWizard_HostedDefaultPath pins spec 0195 D3 for the common case: hosted
// on a forge (the default), self-update on. The forge page is shown and the
// module page is not; the self-update page offers this forge and takes it;
// the backend implies the forge feature.
func TestWizard_HostedDefaultPath(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"repo": "org/my-app"})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())

	assert.Contains(t, seen, "hosted")
	assert.Contains(t, seen, "backend")
	assert.Contains(t, seen, "channel")
	assert.NotContains(t, seen, "module", "a hosted project derives its module path")
	assert.NotContains(t, seen, "release-url", "the direct settings hide behind the direct channel")

	cfg := o.skeletonConfig(nil)
	assert.Equal(t, forge.GithubFeature, cfg.ForgeBackend)
	assert.Equal(t, generator.ReleaseChannelForge, cfg.ReleaseChannel)
	assert.Contains(t, enabledNames(cfg.Features), "github")
}

// TestWizard_NotHostedWithDirectUpdates: no on the start page shows the module
// page instead of the forge page, and the self-update page offers the direct
// channel only, whose settings are then asked for.
func TestWizard_NotHostedWithDirectUpdates(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{
		"hosted":              "n",
		"module":              "myapp",
		"channel":             "↓",
		"release-url":         "https://dl.example.com/{{.Version}}/{{.Asset}}",
		"release-version-url": "https://dl.example.com/latest",
	})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())

	assert.Contains(t, seen, "module")
	assert.NotContains(t, seen, "backend", "a project that is not hosted has no forge page")
	assert.Contains(t, seen, "release-url")

	cfg := o.skeletonConfig(nil)
	assert.True(t, o.NoForge)
	assert.Equal(t, "myapp", cfg.ModulePath)
	assert.Empty(t, cfg.ForgeBackend)
	assert.Equal(t, generator.ReleaseChannelDirect, cfg.ReleaseChannel)
	assert.Equal(t, "https://dl.example.com/latest", cfg.Direct.VersionURL)
	assert.NotContains(t, enabledNames(cfg.Features), "github")
}

// TestWizard_NoSelfUpdateShowsNoUpdatePages: with the update feature unticked
// the self-update page, the direct page and the signing pages do not appear.
func TestWizard_NoSelfUpdateShowsNoUpdatePages(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: []string{"init", "docs"}}
	f, seen := keysSeen(t, o, map[string]string{"repo": "org/my-app"})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())

	assert.NotContains(t, seen, "channel")
	assert.NotContains(t, seen, "release-url")
	assert.NotContains(t, seen, "signing")
	assert.Empty(t, o.skeletonConfig(nil).ReleaseChannel)
}

// TestWizard_NotHostedNoUpdate: the shortest path, a module name and nothing
// about forges or channels.
func TestWizard_NotHostedNoUpdate(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: []string{"docs"}}
	f, seen := keysSeen(t, o, map[string]string{"hosted": "n", "module": "myapp"})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())
	require.NoError(t, o.validateFields())

	assert.NotContains(t, seen, "backend")
	assert.NotContains(t, seen, "channel")
	assert.Equal(t, "myapp", o.skeletonConfig(nil).ModulePath)
}

// typeAnswer types text into the focused field; a down arrow in the text
// moves a select's cursor instead.
func typeAnswer(m huh.Model, text string) huh.Model {
	for _, r := range text {
		if r == '↓' {
			m, _ = m.Update(codeKeypress(tea.KeyDown))

			continue
		}

		m, _ = m.Update(keypress(r))
	}

	return m
}

func enabledNames(fs []generator.ManifestFeature) []string {
	var names []string
	for _, f := range fs {
		if f.Enabled {
			names = append(names, f.Name)
		}
	}

	return names
}

// TestWizard_NotHostedRefusesTheForgeChannel: the channel select offers both
// entries statically, so a not-hosted project that leaves the cursor on
// "This forge" is refused at the field rather than after the wizard.
func TestWizard_NotHostedRefusesTheForgeChannel(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"hosted": "n", "module": "myapp"})

	assert.NotEqual(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NotNil(t, f.GetFocusedField())
	assert.Equal(t, "channel", f.GetFocusedField().GetKey())
	require.Error(t, f.GetFocusedField().Error())
}
