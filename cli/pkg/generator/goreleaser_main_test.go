package generator

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestGeneratedGoreleaserMainCompilesEveryMainFile pins #79: the generator
// emits side-effect files into cmd/<name>/ (keychain.go, and signing.go when
// signing is on) as `package main`, and a .goreleaser.yaml whose `main:` must
// name that package, not main.go. `go build cmd/<name>/main.go` compiles the
// one file, so a `main:` pointing at the file ships a binary without whatever
// the other files link. Both halves come from this generator, so they are
// checked against each other here; go build ./..., go test and go run all
// compile the package, which is why the fault was invisible to them.
func TestGeneratedGoreleaserMainCompilesEveryMainFile(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	cfg := signingSkeletonConfig(path, ApplySigningDefaults(ManifestSigning{Enabled: true, KeyID: "alias/k"}))
	cfg.Features = append(cfg.Features, ManifestFeature{Name: KeychainFeature, Enabled: true})

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	raw, err := os.ReadFile(filepath.Join(path, ".goreleaser.yaml"))
	require.NoError(t, err)

	var doc struct {
		Builds []struct {
			Main string `yaml:"main"`
		} `yaml:"builds"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.Len(t, doc.Builds, 1)

	main := doc.Builds[0].Main
	assert.False(t, strings.HasSuffix(main, ".go"), "main: %q names a file; goreleaser then compiles that file alone", main)

	mainDir := filepath.Join(path, filepath.Clean(main))
	info, err := os.Stat(mainDir)
	require.NoError(t, err, "main: %q must be a directory in the generated tree", main)
	require.True(t, info.IsDir())

	// Every package main file the generator emitted must live in that
	// directory, so the package build picks it up. The tree is the test's
	// own temp dir, read through os.Root so the walk and the reads share one
	// scope.
	root, err := os.OpenRoot(filepath.Join(path, "cmd"))
	require.NoError(t, err)

	defer func() { _ = root.Close() }()

	var mainFiles []string

	require.NoError(t, fs.WalkDir(root.FS(), ".", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
			return walkErr
		}

		src, readErr := fs.ReadFile(root.FS(), p)
		if readErr != nil {
			return readErr
		}

		if strings.Contains(string(src), "\npackage main\n") || strings.HasPrefix(string(src), "package main\n") {
			mainFiles = append(mainFiles, filepath.Join(path, "cmd", p))
		}

		return nil
	}))

	require.GreaterOrEqual(t, len(mainFiles), 3, "main.go, keychain.go and signing.go are expected: %v", mainFiles)

	for _, f := range mainFiles {
		assert.Equal(t, mainDir, filepath.Dir(f), "%s is package main but outside the directory main: names", filepath.Base(f))
	}
}

// TestGeneratedGoreleaserRunsTheBinaryBeforePublishing (#95): keryx v0.19.0
// shipped green with a binary that could not start, because nothing in the
// release ran it. The scaffold's build carries a post hook that runs the
// artefact goreleaser built (not a go run of the package, which is a
// different build) on the target that matches the runner, so a release
// whose binary fails at start fails before anything is published.
func TestGeneratedGoreleaserRunsTheBinaryBeforePublishing(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

	require.NoError(t, g.GenerateSkeleton(context.Background(), signingSkeletonConfig(path, ManifestSigning{})))

	raw, err := os.ReadFile(filepath.Join(path, ".goreleaser.yaml"))
	require.NoError(t, err)

	var doc struct {
		Builds []struct {
			Hooks struct {
				Post []struct {
					Cmd string `yaml:"cmd"`
				} `yaml:"post"`
			} `yaml:"hooks"`
		} `yaml:"builds"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &doc))
	require.Len(t, doc.Builds, 1)
	require.Len(t, doc.Builds[0].Hooks.Post, 1)

	cmd := doc.Builds[0].Hooks.Post[0].Cmd
	assert.Contains(t, cmd, `"{{ .Path }}" version --ci`, "the built artefact is what runs")
	assert.Contains(t, cmd, `"{{ .Runtime.Goos }}/{{ .Runtime.Goarch }}"`, "only the target the runner can execute")
	assert.NotContains(t, cmd, "go run", "a go run is a different build from the one that ships")
}
