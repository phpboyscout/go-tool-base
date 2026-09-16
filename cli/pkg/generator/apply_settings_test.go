package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const wizardManifest = "properties:\n  name: mytool\n  description: old\n  features:\n    - name: ai\n      enabled: true\n" +
	"  chat:\n    providers: [claude]\n    default:\n      provider: claude\n  docs_layout: diataxis\n  module_published: true\n  module_path: github.com/org/mytool\n" +
	"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n" +
	"version:\n  gtb: v1.0.0\n  go: \"1.26\"\nhashes:\n  justfile: abc\ncommands:\n  - name: hello\n    description: says hello\n"

// TestApplyAuthorSettings pins spec 0197 D13's write half: the wizard's
// answers, as a SkeletonConfig, replace the author-settable fields of the
// manifest and nothing else (commands, hashes, the generator's recorded
// fields survive), the result is validated, and the derived files follow.
func TestApplyAuthorSettings(t *testing.T) {
	t.Parallel()

	g, fs, _ := newPerimeterTestProject(t, wizardManifest)

	m, err := g.LoadManifest()
	require.NoError(t, err)

	cfg := SkeletonConfigFromManifest(*m)
	cfg.Description = "new"
	cfg.Chat.Providers = []string{"claude", "codex-local"}
	cfg.Chat.Default.Provider = "codex-local"
	cfg.TelemetryEndpoint = "https://t.internal"

	require.NoError(t, g.ApplyAuthorSettings(context.Background(), cfg))

	after, err := g.LoadManifest()
	require.NoError(t, err)
	assert.Equal(t, "new", string(after.Properties.Description))
	assert.Equal(t, "codex-local", after.Properties.Chat.Default.Provider)
	assert.Equal(t, "https://t.internal", after.Properties.Telemetry.Endpoint)
	assert.Equal(t, "hello", after.Commands[0].Name, "commands are not the wizard's to touch")
	assert.Equal(t, "abc", after.Hashes["justfile"], "hashes survive")
	assert.True(t, after.Properties.ModulePublished, "recorded fields survive")
	assert.Equal(t, "diataxis", after.Properties.DocsLayout)

	bundle, err := afero.ReadFile(fs, "/work/cmd/mytool/chat/assets/config.yaml")
	require.NoError(t, err, "the sync ran")
	assert.Contains(t, string(bundle), "codex-local")

	cfg.Chat.Default.Provider = "gemini"
	require.ErrorIs(t, g.ApplyAuthorSettings(context.Background(), cfg), ErrChatDefaultNotLinked)
}

// TestDiffAuthorSettings: the dry run says which settings would change, by
// path, old to new, and nothing about what would not.
func TestDiffAuthorSettings(t *testing.T) {
	t.Parallel()

	g, _, _ := newPerimeterTestProject(t, wizardManifest)
	before, err := g.LoadManifest()
	require.NoError(t, err)

	cfg := SkeletonConfigFromManifest(*before)
	cfg.Description = "new"
	cfg.Bootstrap.SkipConfigCheck = []string{"version"}

	after := *before
	applyAuthorSettings(&after, cfg)

	diff := DiffAuthorSettings(before, &after)
	assert.Equal(t, []string{
		`bootstrap.skip_config_check: [] -> [version]`,
		`description: old -> new`,
	}, diff)

	assert.Empty(t, DiffAuthorSettings(before, before))
}
