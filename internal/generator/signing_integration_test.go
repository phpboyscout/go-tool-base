package generator

import (
	"context"
	"os/exec"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
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
		Config: config.NewFilesContainer(fs, config.WithLogger(l)),
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
