package generator

import (
	"context"
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

// A generated signing project's own tests fail when one key is embedded as
// both a release key and an anchor-only key (#208).
func TestGeneratedTrustKeysTest_RefusesAKeyInBothDirectories(t *testing.T) {
	t.Parallel()

	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)
	path := t.TempDir()

	p := &props.Props{FS: afero.NewOsFs(), Logger: logger.NewNoop(), Config: emptyTestStore(t)}
	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name: "keystool", Repo: "test/keystool", Host: "github.com", Path: path,
		ForgeBackend: "github", ReleaseChannel: ReleaseChannelForge,
		Features: []ManifestFeature{{Name: "github", Enabled: true}},
		Signing:  ManifestSigning{Enabled: true},
	}))

	injectGoToolBaseReplace(t, path, localModule)
	runGo(t, path, "mod", "tidy")
	runGo(t, path, "test", "./internal/trustkeys/")

	osFS := afero.NewOsFs()
	key, err := afero.ReadFile(osFS, filepath.Join(localModule, "cli/pkg/trustkeys/authority/rotation-authority.asc"))
	require.NoError(t, err)

	release := filepath.Join(path, "internal/trustkeys/keys/authority-copy.asc")
	require.NoError(t, afero.WriteFile(osFS, release, key, 0o600))
	require.NoError(t, afero.WriteFile(osFS, filepath.Join(path, "internal/trustkeys/authority/authority.asc"), key, 0o600))

	cmd := exec.Command("go", "test", "./internal/trustkeys/")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	require.Error(t, err, string(out))
	assert.Contains(t, string(out), "is both a release key and an anchor-only key")
	assert.Contains(t, string(out), "keep one copy")

	require.NoError(t, osFS.Remove(release))
	runGo(t, path, "test", "./internal/trustkeys/")
}
