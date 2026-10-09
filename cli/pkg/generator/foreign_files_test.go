package generator

// #113: a file gtb never created is the developer's. Generating into an
// existing repository must not overwrite it silently.

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const handWrittenGitignore = "/bin/\n/dist/\n.DS_Store\n"

func TestResolveConflict_AFileGtbNeverCreated(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		overwrite OverwriteMode
		force     bool
		content   string
		wantWrite bool
	}{
		{name: "--overwrite deny keeps it", overwrite: OverwriteDeny, content: handWrittenGitignore},
		{name: "ask with no terminal keeps it", overwrite: OverwriteAsk, content: handWrittenGitignore},
		{name: "--overwrite allow replaces it", overwrite: OverwriteAllow, content: handWrittenGitignore, wantWrite: true},
		{name: "--force replaces it", overwrite: OverwriteDeny, force: true, content: handWrittenGitignore, wantWrite: true},
		{name: "identical content is no conflict", overwrite: OverwriteDeny, content: "rendered\n", wantWrite: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work", Overwrite: tt.overwrite, Force: tt.force})
			require.NoError(t, afero.WriteFile(fs, "/work/.gitignore", []byte(tt.content), 0o644))

			decision := g.resolveConflict("/work/.gitignore", ".gitignore", "", []byte("rendered\n"))

			assert.Equal(t, tt.wantWrite, decision.Write())

			if !tt.wantWrite {
				assert.Contains(t, decision.Reason, keepReasonNotCreatedByGtb)
				assert.Empty(t, decision.RecordHash, "a file gtb never created gets no baseline hash")
				assert.True(t, g.conflicts.wasKept(".gitignore"))
			}
		})
	}
}

func newForeignFilesProject(t *testing.T, existing map[string]string) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	fs := afero.NewMemMapFs()
	buf := logger.NewBuffer()

	g := New(&props.Props{FS: fs, Logger: buf}, &Config{Overwrite: OverwriteDeny})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return []byte("done"), nil }

	for path, content := range existing {
		require.NoError(t, afero.WriteFile(fs, "/work/"+path, []byte(content), 0o644))
	}

	return g, fs, buf
}

func foreignFilesConfig() SkeletonConfig {
	return SkeletonConfig{
		Name:        "dunscaith",
		Repo:        "phpboyscout/dunscaith",
		Host:        "gitlab.com",
		Description: "A test project",
		Path:        "/work",
		Features:    []ManifestFeature{{Name: "init", Enabled: true}},
	}
}

func TestGenerateSkeleton_KeepsAFileItNeverCreated(t *testing.T) {
	t.Parallel()

	g, fs, buf := newForeignFilesProject(t, map[string]string{".gitignore": handWrittenGitignore})

	require.NoError(t, g.GenerateSkeleton(context.Background(), foreignFilesConfig()))

	kept, err := afero.ReadFile(fs, "/work/.gitignore")
	require.NoError(t, err)
	assert.Equal(t, handWrittenGitignore, string(kept))

	_, hashed := g.loadProjectFileHashes("/work")[".gitignore"]
	assert.False(t, hashed, "the manifest must not adopt the file as gtb's")

	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "kept your version"), "generate names the kept file: %v", buf.Messages())

	// A second run still treats it as the developer's.
	g.config.Path = "/work"
	require.NoError(t, g.RegenerateProject(context.Background()))

	kept, err = afero.ReadFile(fs, "/work/.gitignore")
	require.NoError(t, err)
	assert.Equal(t, handWrittenGitignore, string(kept))
}

func TestGenerateSkeleton_DoesNotAddASecondRenovateConfig(t *testing.T) {
	t.Parallel()

	g, fs, buf := newForeignFilesProject(t, map[string]string{"renovate.json": "{}\n"})

	require.NoError(t, g.GenerateSkeleton(context.Background(), foreignFilesConfig()))

	exists, err := afero.Exists(fs, "/work/renovate.json5")
	require.NoError(t, err)
	assert.False(t, exists, "renovate.json already configures Renovate")

	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "kept your version"), "the summary says why: %v", buf.Messages())
}

func TestExistingRenovateConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{Path: "/work"})

	assert.Empty(t, g.existingRenovateConfig("/work", "renovate.json5"))

	require.NoError(t, afero.WriteFile(fs, "/work/renovate.json5", []byte("{}"), 0o644))
	assert.Empty(t, g.existingRenovateConfig("/work", "renovate.json5"), "the target itself is not another config")

	require.NoError(t, afero.WriteFile(fs, "/work/.github/renovate.json", []byte("{}"), 0o644))
	assert.Equal(t, ".github/renovate.json", g.existingRenovateConfig("/work", "renovate.json5"))
}
