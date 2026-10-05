package attach

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

const signingCLI = "gitlab.com/phpboyscout/go/signing-cli"

func newProject(t *testing.T) (*props.Props, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	require.NoError(t, generator.New(p, &generator.Config{}).GenerateSkeleton(context.Background(), generator.SkeletonConfig{
		Name:         "attach-tool",
		Repo:         "acme/attach-tool",
		Host:         "github.com",
		ForgeBackend: forge.GithubFeature,
		Description:  "attach fixture",
		Path:         "/work",
	}))

	return p, fs
}

func readFile(t *testing.T, fs afero.Fs, path string) string {
	t.Helper()

	b, err := afero.ReadFile(fs, path)
	require.NoError(t, err)

	return string(b)
}

// run executes the top-level attach group with args and returns stdout.
func run(t *testing.T, p *props.Props, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer

	cmd := NewCmdAttach(p).Command
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(args)

	err := cmd.Execute()

	return out.String(), err
}

func TestAttachCommand_WiresConstructor(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	out, err := run(t, p, "command", signingCLI+"@v0.1.0",
		"--constructor", "NewCmdSign", "--arg", "logger", "--wrap", "--name", "sign", "--path", "/work")
	require.NoError(t, err)
	assert.Equal(t, "attached NewCmdSign from "+signingCLI+"@v0.1.0\n", out)

	manifest := readFile(t, fs, "/work/.gtb/manifest.yaml")
	assert.Contains(t, manifest, signingCLI)
	assert.Contains(t, manifest, "v0.1.0")
	assert.Contains(t, manifest, "NewCmdSign")

	assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"),
		`setup.Wrap("", signingcli.NewCmdSign(p.GetLogger()))`)
}

func TestAttachCommand_ImportPathAndAlias(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	_, err := run(t, p, "command", "example.com/tools@v1.2.3",
		"--constructor", "NewCmdThing", "--arg", "props",
		"--import-path", "example.com/tools/cmd/thing", "--alias", "thingcmd", "--path", "/work")
	require.NoError(t, err)

	root := readFile(t, fs, "/work/pkg/cmd/root/cmd.go")
	assert.Contains(t, root, `"example.com/tools/cmd/thing"`)
	assert.Contains(t, root, "thingcmd.NewCmdThing(p)")
}

func TestAttachCommand_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		project bool
		args    []string
		wantErr error
	}{
		{
			name:    "missing version",
			project: true,
			args:    []string{"command", signingCLI, "--constructor", "NewCmdSign", "--path", "/work"},
			wantErr: generator.ErrInvalidInput,
		},
		{
			name:    "unknown injection token",
			project: true,
			args:    []string{"command", signingCLI + "@v0.1.0", "--constructor", "NewCmdSign", "--arg", "bogus", "--path", "/work"},
			wantErr: generator.ErrInvalidInput,
		},
		{
			name:    "missing manifest",
			args:    []string{"command", signingCLI + "@v0.1.0", "--constructor", "NewCmdSign", "--path", "/work"},
			wantErr: generator.ErrNotGoToolBaseProject,
		},
		{
			name:    "constructor flag required",
			project: true,
			args:    []string{"command", signingCLI + "@v0.1.0", "--path", "/work"},
		},
		{
			name:    "no module argument",
			project: true,
			args:    []string{"command", "--constructor", "NewCmdSign", "--path", "/work"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
			if tc.project {
				p, _ = newProject(t)
			}

			out, err := run(t, p, tc.args...)
			require.Error(t, err)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}

			assert.NotContains(t, out, "attached")
		})
	}
}

func TestAttachAdapter_Scaffolds(t *testing.T) {
	t.Parallel()

	p, fs := newProject(t)

	out, err := run(t, p, "adapter", "--path", "/work")
	require.NoError(t, err)
	assert.Contains(t, out, "scaffolded pkg/cmd/external/attach.go")

	exists, err := afero.Exists(fs, "/work/pkg/cmd/external/attach.go")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Contains(t, readFile(t, fs, "/work/pkg/cmd/root/cmd.go"), "external.Commands(p)")
}

func TestAttachAdapter_Errors(t *testing.T) {
	t.Parallel()

	t.Run("missing manifest", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}

		out, err := run(t, p, "adapter", "--path", "/work")
		require.ErrorIs(t, err, generator.ErrNotGoToolBaseProject)
		assert.NotContains(t, out, "scaffolded")
	})

	t.Run("rejects arguments", func(t *testing.T) {
		t.Parallel()

		p, _ := newProject(t)

		_, err := run(t, p, "adapter", "extra", "--path", "/work")
		require.Error(t, err)
	})
}

func TestAttachList(t *testing.T) {
	t.Parallel()

	t.Run("empty project", func(t *testing.T) {
		t.Parallel()

		p, _ := newProject(t)

		out, err := run(t, p, "list", "--path", "/work")
		require.NoError(t, err)
		assert.Equal(t, "No external command attachments.\n", out)
	})

	t.Run("attachments and adapter", func(t *testing.T) {
		t.Parallel()

		p, _ := newProject(t)

		_, err := run(t, p, "command", signingCLI+"@v0.1.0", "--constructor", "NewCmdSign", "--arg", "logger", "--wrap", "--path", "/work")
		require.NoError(t, err)
		_, err = run(t, p, "command", signingCLI+"@v0.1.0", "--constructor", "NewCmdKeys", "--arg", "logger", "--arg", "config", "--path", "/work")
		require.NoError(t, err)
		_, err = run(t, p, "adapter", "--path", "/work")
		require.NoError(t, err)

		out, err := run(t, p, "list", "--path", "/work")
		require.NoError(t, err)
		assert.Equal(t, signingCLI+"@v0.1.0\n"+
			"  - NewCmdSign(logger) (wrapped)\n"+
			"  - NewCmdKeys(logger, config)\n"+
			"adapter: pkg/cmd/external/attach.go (external.Commands)\n", out)
	})

	t.Run("missing manifest", func(t *testing.T) {
		t.Parallel()

		p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}

		out, err := run(t, p, "list", "--path", "/work")
		require.Error(t, err)
		assert.NotContains(t, out, "No external command attachments.")
	})

	t.Run("rejects arguments", func(t *testing.T) {
		t.Parallel()

		p, _ := newProject(t)

		_, err := run(t, p, "list", "extra", "--path", "/work")
		require.Error(t, err)
	})
}

func TestPrintAttachments_AdapterOnly(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	printAttachments(&out, nil, true)
	assert.Equal(t, "adapter: pkg/cmd/external/attach.go (external.Commands)\n", out.String())
}
