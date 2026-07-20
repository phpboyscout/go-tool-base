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

// TestGeneratedSigningProjectCompiles scaffolds a project with signing
// enabled and runs `go build ./...` to prove the generated trustkeys
// package, the props.Signing wiring, and the generated signing.go all
// compile end-to-end. The //go:embed all:keys directive needs the
// keys/.gitkeep the scaffold emits, so a missing placeholder would fail
// here rather than only in a downstream user's build.
func TestGeneratedSigningProjectCompiles(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)

	path := t.TempDir()
	fs := afero.NewOsFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: emptyTestStore(t),
	}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := SkeletonConfig{
		Name:        "sign-compile-tool",
		Repo:        "test/sign-compile-tool",
		Host:        "github.com",
		Description: "Signing compile-time regression test project",
		Path:        path,
		Features: []ManifestFeature{
			{Name: "changelog", Enabled: false},
			{Name: "docs", Enabled: false},
		},
		Signing: ManifestSigning{
			Enabled:          true,
			ExternalKeyEmail: "release@example.test",
		},
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg), "signing skeleton generation must succeed")

	injectGoToolBaseReplace(t, path, localModule)

	runGo(t, path, "mod", "tidy")
	runGo(t, path, "build", "./...")
}

// TestGeneratedSigningProjectWithSignsBlock scaffolds a project with a signing
// key id recorded, proving the generated .goreleaser.yaml carries the direct
// (shim-free) gtb sign block, the project still compiles, and — when goreleaser
// is available — the release config validates with `goreleaser check`.
func TestGeneratedSigningProjectWithSignsBlock(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator", "generator_build")

	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("`go` not on PATH: %v", err)
	}

	localModule := localGoToolBasePath(t)

	path := t.TempDir()
	fs := afero.NewOsFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: emptyTestStore(t),
	}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	cfg := SkeletonConfig{
		Name:        "sign-pipeline-tool",
		Repo:        "test/sign-pipeline-tool",
		Host:        "github.com",
		Description: "Signing release-pipeline regression test project",
		Path:        path,
		Features: []ManifestFeature{
			{Name: "changelog", Enabled: false},
			{Name: "docs", Enabled: false},
		},
		Signing: ApplySigningDefaults(ManifestSigning{
			Enabled:          true,
			ExternalKeyEmail: "release@example.test",
			KeyID:            "alias/sign-pipeline-tool-v1",
		}),
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg), "signing skeleton generation must succeed")

	goreleaser, err := os.ReadFile(filepath.Join(path, ".goreleaser.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(goreleaser), "signs:")
	assert.Contains(t, string(goreleaser), "alias/sign-pipeline-tool-v1")

	injectGoToolBaseReplace(t, path, localModule)

	runGo(t, path, "mod", "tidy")
	runGo(t, path, "build", "./...")
}
