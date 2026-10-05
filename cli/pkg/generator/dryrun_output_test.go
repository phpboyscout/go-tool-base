package generator

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// A dry run's preview belongs on the invocation's stdout, where a caller
// (and a test) can read it, not on the process's.
func TestDryRun_PrintsThePreviewToTheInvocationsStdout(t *testing.T) {
	t.Parallel()

	newProps := func(fs afero.Fs, out *bytes.Buffer) *props.Props {
		return &props.Props{
			FS:      fs,
			Logger:  logger.NewNoop(),
			Version: version.NewInfo("v1.0.0", "", ""),
			IO:      props.StdIO{Stdout: out},
		}
	}

	t.Run("generate command", func(t *testing.T) {
		t.Parallel()

		fs, out := afero.NewMemMapFs(), &bytes.Buffer{}
		setupDryRunProject(t, fs, "/project")

		gen := New(newProps(fs, out), &Config{Name: "hello", Short: "Say hello", Path: "/project", Parent: "root", DryRun: true})
		require.NoError(t, gen.Generate(context.Background()))

		assert.Contains(t, out.String(), "pkg/cmd/hello")
	})

	t.Run("generate skeleton", func(t *testing.T) {
		t.Parallel()

		fs, out := afero.NewMemMapFs(), &bytes.Buffer{}

		gen := New(newProps(fs, out), &Config{DryRun: true})
		require.NoError(t, gen.GenerateSkeleton(context.Background(), SkeletonConfig{
			Name: "dry-tool", Repo: "acme/dry-tool", Host: "github.com",
			ForgeBackend: forge.GithubFeature, Description: "dry run", Path: "/work",
		}))

		assert.Contains(t, out.String(), "Files to create")
	})

	t.Run("regenerate project", func(t *testing.T) {
		t.Parallel()

		fs, out := afero.NewMemMapFs(), &bytes.Buffer{}
		setupDryRunProject(t, fs, "/project")

		gen := New(newProps(fs, out), &Config{Path: "/project", DryRun: true})
		require.NoError(t, gen.RegenerateProject(context.Background()))

		assert.NotEmpty(t, out.String())
	})
}
