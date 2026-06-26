package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestRegenerateManifest_PreservesProjectProperties guards against the wipe where
// rebuilding the manifest from the root cmd.go AST replaced the whole Properties
// struct, dropping every manifest-only field the AST cannot recover.
func TestRegenerateManifest_PreservesProjectProperties(t *testing.T) {
	fs := afero.NewMemMapFs()
	var logBuf strings.Builder
	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0o644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0o755))

	// A valid root cmd.go so extractProjectProperties succeeds (and would trigger
	// the wipe if Properties were replaced wholesale).
	rootCode := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)
func NewCmdRoot(v version.Info) (*setup.Command, *props.Props) {
	p := &props.Props{
		Tool: props.Tool{
			Name:        "test-tool",
			Description: "desc",
			ReleaseSource: props.ReleaseSource{Host: "github.com", Owner: "acme", Repo: "test-tool", Type: "github"},
		},
	}
	return gtbRoot.NewCmdRoot(p), p
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0o644))

	manifestYAML := `properties:
  name: test-tool
  description: desc
  env_prefix: TT
  update_policy: prompt
  update_check_interval: 48h
  docs_layout: diataxis
  module_published: true
  features:
    - name: config
      enabled: true
`
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte(manifestYAML), 0o644))

	g := New(&props.Props{FS: fs, Logger: l, Config: config.NewFilesContainer(fs), Tool: props.Tool{Name: "test-tool"}}, &Config{Path: workDir})
	require.NoError(t, g.RegenerateManifest(context.Background()))

	data, err := afero.ReadFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	// Manifest-only author configuration must survive the AST-driven rebuild.
	assert.Equal(t, "TT", m.Properties.EnvPrefix, "env_prefix must be preserved")
	assert.Equal(t, "prompt", m.Properties.UpdatePolicy, "update_policy must be preserved")
	assert.Equal(t, "48h", m.Properties.UpdateCheckInterval, "update_check_interval must be preserved")
	assert.Equal(t, DocsLayoutDiataxis, m.Properties.DocsLayout, "docs_layout must be preserved")
	assert.True(t, m.Properties.ModulePublished, "module_published must be preserved")
	// And the AST-recoverable fields stay correct.
	assert.Equal(t, "test-tool", m.Properties.Name)
}
