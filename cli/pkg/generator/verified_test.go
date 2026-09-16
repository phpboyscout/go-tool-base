package generator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/errorhandling"
	"gitlab.com/phpboyscout/go/errors"
)

// TestGenerateSkeleton_EmittedIsNotVerified pins spec 0197 D10: when a
// post-processing step fails after the files are written, the run says so
// with ErrProjectNotVerified carrying exit code 3, names the step, and leaves
// the files. --no-verify skips the steps and reports nothing.
func TestGenerateSkeleton_EmittedIsNotVerified(t *testing.T) {
	t.Setenv(SkipLintEnv, "")

	t.Run("a failed tidy is exit 3 naming the step", func(t *testing.T) {
		path := t.TempDir()
		g := newSkeletonGeneratorForTest(t, afero.NewOsFs())
		g.runCommand = func(_ context.Context, _, name string, args ...string) ([]byte, error) {
			if name == "go" && len(args) > 0 && args[0] == "mod" {
				return nil, errors.New("module gitlab.com/phpboyscout/go-tool-base/cli@v9.9.9: not found")
			}

			return nil, nil
		}

		err := g.GenerateSkeleton(context.Background(), bootstrapSkeletonConfig(path, ManifestBootstrap{}))
		require.ErrorIs(t, err, ErrProjectNotVerified)
		assert.Equal(t, ExitCodeNotVerified, errorhandling.ExitCode(err))
		assert.Contains(t, err.Error(), "go mod tidy")
		assert.Contains(t, err.Error(), "not found")

		_, statErr := os.Stat(filepath.Join(path, "go.mod"))
		require.NoError(t, statErr, "the files stay")
	})

	t.Run("every step passing is exit 0", func(t *testing.T) {
		path := t.TempDir()
		g := newSkeletonGeneratorForTest(t, afero.NewOsFs())

		require.NoError(t, g.GenerateSkeleton(context.Background(), bootstrapSkeletonConfig(path, ManifestBootstrap{})))
	})

	t.Run("no-verify runs no step", func(t *testing.T) {
		path := t.TempDir()
		g := newSkeletonGeneratorForTest(t, afero.NewOsFs())
		g.config.NoVerify = true

		var ran []string
		g.runCommand = func(_ context.Context, _, name string, args ...string) ([]byte, error) {
			ran = append(ran, name+" "+strings.Join(args, " "))

			return nil, errors.New("would have failed")
		}

		require.NoError(t, g.GenerateSkeleton(context.Background(), bootstrapSkeletonConfig(path, ManifestBootstrap{})))

		for _, cmd := range ran {
			assert.NotContains(t, cmd, "go mod tidy")
			assert.NotContains(t, cmd, "golangci-lint")
		}
	})
}

// TestSkeletonGoMod_HasNoGTBToolLine pins spec 0197 D12: gtb is installed,
// not a tool directive that drags every adapter and SDK into the generated
// module's requirements and cannot resolve before cli/vX is tagged.
func TestSkeletonGoMod_HasNoGTBToolLine(t *testing.T) {
	t.Parallel()

	path := t.TempDir()
	g := newSkeletonGeneratorForTest(t, afero.NewOsFs())
	require.NoError(t, g.GenerateSkeleton(context.Background(), bootstrapSkeletonConfig(path, ManifestBootstrap{})))

	gomod, err := os.ReadFile(filepath.Join(path, "go.mod"))
	require.NoError(t, err)
	assert.NotContains(t, string(gomod), "cli/cmd/gtb")
	assert.NotContains(t, string(gomod), "golangci-lint", "the v1 tool line (#31) is gone; the binary is installed")
	assert.NotContains(t, string(gomod), "mockery")
	assert.Contains(t, string(gomod), "go-tool-base/cmd/changelog", "the framework's own tool lines stay")

	readme, err := os.ReadFile(filepath.Join(path, "README.md"))
	require.NoError(t, err)
	assert.Contains(t, string(readme), "go install gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb@", "the README says how to get the pinned gtb")
}
