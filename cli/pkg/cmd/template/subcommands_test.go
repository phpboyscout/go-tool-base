package template

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	icmd "gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator"
	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

const projectRoot = "/work"

type logBuffer interface{ Contains(string) bool }

// newProject generates a skeleton project at projectRoot and writes a local
// template source at /tmpl that adds EXTRA.md.
func newProject(t *testing.T) (*props.Props, afero.Fs, logBuffer) {
	t.Helper()

	fs := afero.NewMemMapFs()
	buf := logger.NewBuffer()
	p := &props.Props{
		FS:      fs,
		Logger:  buf,
		Version: version.NewInfo("v1.0.0", "", ""),
		IO:      props.StdIO{Stdin: io.LimitReader(nil, 0), Stdout: io.Discard, Stderr: io.Discard},
	}

	require.NoError(t, generator.New(p, &generator.Config{Path: projectRoot, Overwrite: "allow"}).GenerateSkeleton(
		context.Background(),
		generator.SkeletonConfig{Name: "mytool", Repo: "acme/mytool", Host: "github.com", Path: projectRoot},
	))

	require.NoError(t, fs.MkdirAll("/tmpl", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/tmpl/EXTRA.md", []byte("extra {{ .Name }}"), 0o644))

	return p, fs, buf
}

func run(t *testing.T, p *props.Props, args ...string) (string, error) {
	t.Helper()

	cmd := NewCmdTemplate(p).Command

	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs(args)

	err := cmd.ExecuteContext(context.Background())

	return out.String(), err
}

func listSources(t *testing.T, p *props.Props) []generator.TemplateSource {
	t.Helper()

	sources, err := generator.New(p, &generator.Config{Path: projectRoot}).ListTemplateSources()
	require.NoError(t, err)

	return sources
}

func TestTemplateAdd_LocalSourceLifecycle(t *testing.T) {
	t.Parallel()

	p, fs, log := newProject(t)

	_, err := run(t, p, "add", "/tmpl", "--name", "house", "--path", projectRoot)
	require.NoError(t, err)

	extra, err := afero.ReadFile(fs, projectRoot+"/EXTRA.md")
	require.NoError(t, err)
	assert.Equal(t, "extra mytool", string(extra))
	assert.True(t, log.Contains("added template source"))

	sources := listSources(t, p)
	require.Len(t, sources, 1)
	assert.Equal(t, "house", sources[0].Name)
	assert.Equal(t, generator.TemplateSourceLocal, sources[0].Type)

	out, err := run(t, p, "list", "--path", projectRoot)
	require.NoError(t, err)
	assert.Contains(t, out, "house")
	assert.Contains(t, out, "ref=(default)")
	assert.Contains(t, out, "pin=(local, fingerprint)")

	require.NoError(t, afero.WriteFile(fs, "/tmpl/EXTRA.md", []byte("updated {{ .Name }}"), 0o644))

	_, err = run(t, p, "update", "house", "--path", projectRoot)
	require.NoError(t, err)

	extra, err = afero.ReadFile(fs, projectRoot+"/EXTRA.md")
	require.NoError(t, err)
	assert.Equal(t, "updated mytool", string(extra))
	assert.True(t, log.Contains("updated template source"))

	_, err = run(t, p, "remove", "house", "--path", projectRoot)
	require.NoError(t, err)
	assert.Empty(t, listSources(t, p))
	assert.True(t, log.Contains("removed template source"))

	_, err = run(t, p, "list", "--path", projectRoot)
	require.NoError(t, err)
	assert.True(t, log.Contains("No template sources configured."))
}

func TestTemplateAdd_Errors(t *testing.T) {
	t.Parallel()

	t.Run("empty source", func(t *testing.T) {
		t.Parallel()

		p, _, _ := newProject(t)
		_, err := run(t, p, "add", " ", "--path", projectRoot)
		require.Error(t, err)
		assert.Empty(t, listSources(t, p))
	})

	t.Run("source the overlay rejects is rolled back", func(t *testing.T) {
		t.Parallel()

		p, fs, _ := newProject(t)
		require.NoError(t, fs.MkdirAll("/evil", 0o755))
		require.NoError(t, afero.WriteFile(fs, "/evil/go.mod", []byte("module evil"), 0o644))

		_, err := run(t, p, "add", "/evil", "--path", projectRoot)
		require.Error(t, err)
		assert.Empty(t, listSources(t, p))
	})

	t.Run("duplicate name", func(t *testing.T) {
		t.Parallel()

		p, _, _ := newProject(t)
		_, err := run(t, p, "add", "/tmpl", "--name", "house", "--path", projectRoot)
		require.NoError(t, err)

		_, err = run(t, p, "add", "/tmpl", "--name", "house", "--path", projectRoot)
		require.Error(t, err)
		assert.Len(t, listSources(t, p), 1)
	})
}

// A declined trust prompt stops the add before anything is cloned or written.
func TestTemplateAdd_DeclinedRemoteSource(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "")

	p, _, _ := newProject(t)
	p.IO = formtest.AccessibleTTY(formtest.Answers("n"))

	_, err := run(t, p, "add", "https://gitlab.com/acme/tmpl@v1", "--path", projectRoot)
	require.ErrorIs(t, err, icmd.ErrRemoteTemplateDeclined)
	assert.Empty(t, listSources(t, p))
}

func TestTemplateUpdateRemove_UnknownSource(t *testing.T) {
	t.Parallel()

	for _, verb := range []string{"update", "remove"} {
		t.Run(verb, func(t *testing.T) {
			t.Parallel()

			p, _, _ := newProject(t)
			_, err := run(t, p, verb, "missing", "--path", projectRoot)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `no template source named "missing"`)
		})
	}
}

func TestTemplateList_NotAProject(t *testing.T) {
	t.Parallel()

	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
	_, err := run(t, p, "list", "--path", "/empty")
	require.Error(t, err)
}

func TestPrintSources_GitPinAndUnnamed(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	m := &generator.Manifest{
		Properties: generator.ManifestProperties{
			Name: "mytool",
			Templates: []generator.TemplateSource{
				{Type: generator.TemplateSourceGit, Location: "https://gitlab.com/acme/tmpl", Ref: "v2", Resolved: "deadbeef"},
			},
		},
		Version: generator.ManifestVersion{GoToolBase: "v1.0.0"},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(projectRoot, ".gtb"), 0o755))
	require.NoError(t, generator.EncodeManifestFile(fs, generator.ManifestPathFor(projectRoot), m))

	out, err := run(t, p, "list", "--path", projectRoot)
	require.NoError(t, err)
	assert.Contains(t, out, "https://gitlab.com/acme/tmpl")
	assert.Contains(t, out, "ref=v2")
	assert.Contains(t, out, "pin=deadbeef")
}

func TestIsCI(t *testing.T) {
	t.Parallel()

	withFlag := func(v bool) *cobra.Command {
		c := &cobra.Command{}
		c.Flags().Bool("ci", v, "")

		return c
	}

	tests := []struct {
		name string
		cmd  *cobra.Command
		cfg  string
		want bool
	}{
		{name: "flag set", cmd: withFlag(true), want: true},
		{name: "flag unset, no config", cmd: withFlag(false), want: false},
		{name: "no flag, config ci", cmd: &cobra.Command{}, cfg: "ci: true\n", want: true},
		{name: "no flag, config not ci", cmd: &cobra.Command{}, cfg: "ci: false\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{}
			if tt.cfg != "" {
				p.Config = testutil.StoreFromYAML(t, tt.cfg)
			}

			assert.Equal(t, tt.want, isCI(tt.cmd, p))
		})
	}
}
