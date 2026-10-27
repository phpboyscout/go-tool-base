package generator

import (
	"context"
	"os"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

type archiveFile struct {
	Src  string `yaml:"src"`
	Info struct {
		Mode int `yaml:"mode"`
	} `yaml:"info"`
}

func archiveFiles(t *testing.T, raw []byte) []archiveFile {
	t.Helper()

	var doc struct {
		Archives []struct {
			Files []archiveFile `yaml:"files"`
		} `yaml:"archives"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.Len(t, doc.Archives, 1)

	return doc.Archives[0].Files
}

func assertArchiveFiles(t *testing.T, files []archiveFile, want ...string) {
	t.Helper()

	srcs := make([]string, 0, len(files))
	for _, f := range files {
		srcs = append(srcs, f.Src)
		assert.Equal(t, 0o644, f.Info.Mode, "%s takes its mode from the CI checkout unless pinned", f.Src)
	}

	assert.Equal(t, want, srcs)
}

// Release archives otherwise carry the CI checkout's modes, which its umask
// makes world-writable (#141). Listing files replaces goreleaser's defaults,
// so the scaffold lists those same globs; a glob matching nothing is skipped,
// where a literal name would fail a project without a LICENSE.
func TestGenerateSkeleton_ArchiveFilesArePinnedTo0644(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)

	cfg := SkeletonConfig{
		Name: "artool", Repo: "acme/artool", Host: "gitlab.com", ForgeBackend: forge.GitlabFeature,
		Description: "archive modes", Path: "/work",
		Features: []ManifestFeature{{Name: "gitlab", Enabled: true}},
	}
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	raw, err := afero.ReadFile(fs, "/work/.goreleaser.yaml")
	require.NoError(t, err)

	assertArchiveFiles(t, archiveFiles(t, raw),
		"license*", "LICENSE*", "readme*", "README*", "changelog*", "CHANGELOG*")
}

func TestOwnGoreleaser_ArchiveFilesArePinnedTo0644(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("../../../.goreleaser.yaml")
	require.NoError(t, err)

	assertArchiveFiles(t, archiveFiles(t, raw), "README.md", "CHANGELOG.md", "LICENSE")
}
