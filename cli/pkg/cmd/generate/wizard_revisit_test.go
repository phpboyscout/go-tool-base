package generate

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const revisitManifest = "properties:\n  name: mytool\n  description: old\n  features:\n    - name: ai\n      enabled: true\n" +
	"  chat:\n    providers: [claude]\n    default:\n      provider: claude\n  module_path: github.com/org/mytool\n" +
	"release_source:\n  type: github\n  backend: github\n  host: github.com\n  owner: org\n  repo: mytool\n" +
	"version:\n  gtb: v1.0.0\n  go: \"1.26\"\ncommands: []\n"

func revisitProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/work/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml", []byte(revisitManifest), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module github.com/org/mytool\n"), 0o644))
	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/root/cmd.go", []byte("package root\n"), 0o644))

	return &props.Props{FS: fs, Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, ""), Version: version.NewInfo("v1.0.0", "", "")}, fs
}

// TestWizardRun pins spec 0197 D13: the wizard loads from the manifest,
// the answers are validated and applied with the derived-file sync, a dry
// run prints the diff and writes nothing, and outside a project it refuses.
func TestWizardRun(t *testing.T) {
	t.Parallel()

	t.Run("applies the answers and syncs", func(t *testing.T) {
		t.Parallel()

		p, fs := revisitProject(t)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			assert.True(t, so.revisit)
			assert.Equal(t, "mytool", so.Name, "pre-filled from the manifest")
			so.Description = "new"
			so.ChatProviders = []string{"claude", "codex-local"}
			so.ChatDefault.Provider = "codex-local"

			return so.afterWizard()
		}}

		var out bytes.Buffer
		require.NoError(t, o.Run(context.Background(), p, &out))

		m, err := generator.New(p, &generator.Config{Path: "/work"}).LoadManifest()
		require.NoError(t, err)
		assert.Equal(t, "new", string(m.Properties.Description))
		assert.Equal(t, "codex-local", m.Properties.Chat.Default.Provider)

		bundle, err := afero.ReadFile(fs, "/work/cmd/mytool/chat/assets/config.yaml")
		require.NoError(t, err, "the sync ran")
		assert.Contains(t, string(bundle), "codex-local")
	})

	t.Run("dry run prints the diff and writes nothing", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		o := &WizardOptions{Path: "/work", DryRun: true, runForm: func(so *SkeletonOptions) error {
			so.Description = "new"

			return so.afterWizard()
		}}

		var out bytes.Buffer
		require.NoError(t, o.Run(context.Background(), p, &out))
		assert.Contains(t, out.String(), "description: old -> new")

		m, err := generator.New(p, &generator.Config{Path: "/work"}).LoadManifest()
		require.NoError(t, err)
		assert.Equal(t, "old", string(m.Properties.Description))
	})

	t.Run("an answer the flags would refuse is refused", func(t *testing.T) {
		t.Parallel()

		p, _ := revisitProject(t)
		o := &WizardOptions{Path: "/work", runForm: func(so *SkeletonOptions) error {
			so.ChatDefault.Provider = "gemini"

			return so.afterWizard()
		}}

		require.ErrorIs(t, o.Run(context.Background(), p, &bytes.Buffer{}), generator.ErrChatDefaultNotLinked)
	})

	t.Run("outside a project", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop(), Config: testutil.StoreFromYAML(t, "")}
		o := &WizardOptions{Path: "/nowhere", runForm: func(*SkeletonOptions) error { return nil }}
		require.Error(t, o.Run(context.Background(), p, &bytes.Buffer{}))
	})
}
