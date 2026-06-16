package template

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestNewCmdTemplate_WiresSubcommands(t *testing.T) {
	t.Parallel()

	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}
	cmd := NewCmdTemplate(p).Command

	require.Equal(t, "template", cmd.Name())

	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}

	for _, want := range []string{"add", "update", "remove", "list"} {
		assert.Truef(t, got[want], "expected subcommand %q", want)
	}
}

func TestTemplateList_PrintsRecordedSources(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}

	root := "/project"
	m := &generator.Manifest{
		Properties: generator.ManifestProperties{
			Name: "mytool",
			Templates: []generator.TemplateSource{
				{Name: "house", Type: generator.TemplateSourceLocal, Location: "/tmpl", Fingerprint: "abc"},
			},
		},
		Version: generator.ManifestVersion{GoToolBase: "v1.0.0"},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, generator.EncodeManifestFile(fs, generator.ManifestPathFor(root), m))

	cmd := newCmdTemplateList(p).Command
	require.NoError(t, cmd.Flags().Set("path", root))

	var out bytes.Buffer
	cmd.SetOut(&out)

	require.NoError(t, cmd.RunE(cmd, nil))
	assert.Contains(t, out.String(), "house")
	assert.Contains(t, out.String(), "/tmpl")
}
