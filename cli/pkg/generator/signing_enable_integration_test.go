package generator

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestEnableDisableSigning_OnExistingProject scaffolds a plain project (no
// signing), enables signing through the generator's EnableSigning (the path
// `gtb enable signing` drives), and asserts the scaffold + manifest land and
// the project builds. It then disables signing and asserts the wiring is gone,
// the trustkeys package is retained (author content is never deleted), and the
// project still builds.
func TestEnableDisableSigning_OnExistingProject(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)

	path := t.TempDir()
	fs := afero.NewOsFs()
	l := logger.NewNoop()

	newGen := func(cfg *Config) *Generator {
		p := &props.Props{
			FS:     fs,
			Logger: l,
			Config: emptyTestStore(t),
		}
		g := New(p, cfg)
		g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
			return nil, nil
		}

		return g
	}

	// 1. Scaffold a plain project: no signing.
	require.NoError(t, newGen(&Config{}).GenerateSkeleton(
		context.Background(), signingSkeletonConfig(path, ManifestSigning{})))
	require.NoFileExists(t, filepath.Join(path, "internal", "trustkeys", "trustkeys.go"))

	gen := newGen(&Config{Path: path, Overwrite: "allow"})

	// 2. Enable signing (the work `gtb enable signing --email ...` does).
	require.NoError(t, gen.EnableSigning(context.Background(), ManifestSigning{
		ExternalKeyEmail: "release@example.test",
		KeySource:        "both",
	}))

	assert.FileExists(t, filepath.Join(path, "internal", "trustkeys", "trustkeys.go"))
	assert.FileExists(t, filepath.Join(path, "internal", "trustkeys", "keys", ".gitkeep"))
	assert.FileExists(t, filepath.Join(path, "pkg", "cmd", "root", "signing.go"))

	cmdGo, err := os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.Contains(t, string(cmdGo), "Signing: props.SigningConfig{")

	manifest, err := os.ReadFile(filepath.Join(path, ".gtb", "manifest.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(manifest), "external_key_email: release@example.test")

	injectGoToolBaseReplace(t, path, localModule)
	runGo(t, path, "mod", "tidy")
	runGo(t, path, "build", "./...")

	// 3. Disable signing: wiring gone, trustkeys retained, still builds.
	require.NoError(t, gen.DisableSigning(context.Background()))

	cmdGo, err = os.ReadFile(filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.NotContains(t, string(cmdGo), "Signing:")
	assert.NoFileExists(t, filepath.Join(path, "pkg", "cmd", "root", "signing.go"))
	assert.DirExists(t, filepath.Join(path, "internal", "trustkeys"))

	runGo(t, path, "build", "./...")
}
