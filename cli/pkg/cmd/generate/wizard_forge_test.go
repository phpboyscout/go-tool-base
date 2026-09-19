package generate

import (
	"gitlab.com/phpboyscout/go/errors"

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
	assert.NotContains(t, seen, "release-base-url", "the forge channel has no location page")

	cfg := o.skeletonConfig(nil)
	assert.Equal(t, forge.GithubFeature, cfg.ForgeBackend)
	assert.Equal(t, generator.ReleaseChannelForge, cfg.ReleaseChannel)
	assert.Empty(t, cfg.ReleaseBaseURL)
	assert.Contains(t, enabledNames(cfg.Features), "github")
}

// TestWizard_HostedChoosesTheStaticLocation: a hosted project can opt into
// the static channel (spec 0203 D1); the location page follows and its URL
// is recorded, while the backend stays for init and credentials.
func TestWizard_HostedChoosesTheStaticLocation(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"repo": "org/my-app", "channel": "↓", "release-base-url": "https://pkg.acme.dev/my-app"})

	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())
	require.NoError(t, o.validateFields())

	assert.Contains(t, seen, "release-base-url", "the location page follows the channel select")

	cfg := o.skeletonConfig(nil)
	assert.Equal(t, generator.ReleaseChannelStatic, cfg.ReleaseChannel)
	assert.Equal(t, "https://pkg.acme.dev/my-app", cfg.ReleaseBaseURL)
	assert.Equal(t, forge.GithubFeature, cfg.ForgeBackend, "hosted and static: the backend stays")
}

// TestWizard_StaticLocationRefusesABadURL: the location page holds the base
// URL to the provider-endpoint rule at the field, so a plain-http location
// never reaches the manifest.
func TestWizard_StaticLocationRefusesABadURL(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"repo": "org/my-app", "channel": "↓", "release-base-url": "http://pkg.acme.dev/my-app"})

	assert.NotEqual(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NotNil(t, f.GetFocusedField())
	assert.Equal(t, "release-base-url", f.GetFocusedField().GetKey())
	require.Error(t, f.GetFocusedField().Error())
}

// TestWizard_NotHostedTakesTheStaticLocation: no on the start page shows the
// module page instead of the forge page, and the self-update page offers the
// static location alone (spec 0203 D1); its URL is asked on the page after.
func TestWizard_NotHostedTakesTheStaticLocation(t *testing.T) {
	t.Parallel()

	// The driver runs no commands, so the channel select keeps its static
	// rows (forge first) and the cursor moves down to the static location.
	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"hosted": "n", "module": "myapp", "channel": "↓", "release-base-url": "https://pkg.acme.dev/myapp"})

	assert.Contains(t, seen, "module")
	assert.NotContains(t, seen, "backend", "a project that is not hosted has no forge page")
	assert.Contains(t, seen, "channel", "the self-update page is reached")
	assert.Contains(t, seen, "release-base-url")
	require.Equal(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NoError(t, o.afterWizard())
	require.NoError(t, o.validateFields())

	cfg := o.skeletonConfig(nil)
	assert.Equal(t, generator.ReleaseChannelStatic, cfg.ReleaseChannel)
	assert.Equal(t, "https://pkg.acme.dev/myapp", cfg.ReleaseBaseURL)
	assert.Empty(t, cfg.ForgeBackend)
	assert.Equal(t, "myapp", cfg.ModulePath)
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

// TestWizard_NotHostedRefusesTheForgeChannel: in a terminal the channel
// select drops the forge row once the project is not hosted; a driver that
// runs no commands still sees it, and leaving the cursor there is refused at
// the field with a hint naming the static location.
func TestWizard_NotHostedRefusesTheForgeChannel(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{Features: generator.DefaultSelectedFeatures}
	f, seen := keysSeen(t, o, map[string]string{"hosted": "n", "module": "myapp"})

	assert.NotEqual(t, huh.StateCompleted, f.State, "seen %v", seen)
	require.NotNil(t, f.GetFocusedField())
	assert.Equal(t, "channel", f.GetFocusedField().GetKey())
	require.Error(t, f.GetFocusedField().Error())
	assert.Contains(t, errors.FlattenHints(f.GetFocusedField().Error()), "static location")
}
