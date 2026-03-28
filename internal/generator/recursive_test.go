package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestRecursiveManifestUpdate(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
	}

	manifestPath := ".gtb/manifest.yaml"
	require.NoError(t, fs.MkdirAll(".gtb", 0755))

	initialManifest := Manifest{
		Commands: []ManifestCommand{
			{
				Name: "parent",
				Commands: []ManifestCommand{
					{
						Name: "child",
					},
				},
			},
		},
	}
	data, _ := yaml.Marshal(initialManifest)
	require.NoError(t, afero.WriteFile(fs, manifestPath, data, 0644))

	g := New(p, &Config{
		Path:   ".",
		Name:   "grandchild",
		Parent: "parent/child",
	})

	// Test adding a grandchild
	err := g.updateManifest(nil, map[string]string{"cmd.go": "some-hash"})
	require.NoError(t, err)

	// Read and verify
	data, err = afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	err = yaml.Unmarshal(data, &m)
	require.NoError(t, err)

	require.Len(t, m.Commands, 1)
	assert.Equal(t, "parent", m.Commands[0].Name)
	require.Len(t, m.Commands[0].Commands, 1)
	assert.Equal(t, "child", m.Commands[0].Commands[0].Name)
	require.Len(t, m.Commands[0].Commands[0].Commands, 1)
	assert.Equal(t, "grandchild", m.Commands[0].Commands[0].Commands[0].Name)
	assert.Equal(t, "some-hash", m.Commands[0].Commands[0].Commands[0].Hashes["cmd.go"])
}

func TestUpdateCommandRecursive_Deep(t *testing.T) {
	cmds := []ManifestCommand{
		{
			Name: "a",
			Commands: []ManifestCommand{
				{
					Name: "b",
				},
			},
		},
	}

	// Update existing command 'b'
	updated := updateCommandRecursive(&cmds, []string{"a"}, ManifestCommandUpdate{
		Name: "b", Description: "new short", LongDescription: "new long",
		Hashes: map[string]string{"cmd.go": "new-hash"},
	})
	assert.True(t, updated)
	assert.Equal(t, "new short", string(cmds[0].Commands[0].Description))
	assert.Equal(t, "new-hash", cmds[0].Commands[0].Hashes["cmd.go"])

	// Add new command 'c' under 'b'
	updated = updateCommandRecursive(&cmds, []string{"a", "b"}, ManifestCommandUpdate{
		Name: "c", Description: "short c",
		Hashes: map[string]string{"cmd.go": "hash-c"},
	})
	assert.True(t, updated)
	require.Len(t, cmds[0].Commands[0].Commands, 1)
	assert.Equal(t, "c", cmds[0].Commands[0].Commands[0].Name)
}

func TestSetProtectionRecursive(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}
	require.NoError(t, fs.MkdirAll(".gtb", 0755))

	m := Manifest{
		Commands: []ManifestCommand{
			{
				Name: "p",
				Commands: []ManifestCommand{
					{Name: "c"},
				},
			},
		},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", data, 0644))

	g := New(p, &Config{Path: ".", Name: "c", Parent: "p"})
	err := g.SetProtection(context.Background(), "p/c", true)
	require.NoError(t, err)

	// Verify
	data, _ = afero.ReadFile(fs, ".gtb/manifest.yaml")
	require.NoError(t, yaml.Unmarshal(data, &m))
	assert.True(t, *m.Commands[0].Commands[0].Protected)
}

func TestRemoveCommandRecursive(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{FS: fs, Logger: l}
	require.NoError(t, fs.MkdirAll(".gtb", 0755))

	m := Manifest{
		Commands: []ManifestCommand{
			{
				Name: "p",
				Commands: []ManifestCommand{
					{Name: "c"},
					{Name: "d"},
				},
			},
		},
	}
	data, _ := yaml.Marshal(m)
	require.NoError(t, afero.WriteFile(fs, ".gtb/manifest.yaml", data, 0644))

	g := New(p, &Config{Path: ".", Name: "c", Parent: "p"})
	err := g.Remove(context.Background())
	require.NoError(t, err)

	// Verify
	data, _ = afero.ReadFile(fs, ".gtb/manifest.yaml")
	require.NoError(t, yaml.Unmarshal(data, &m))
	require.Len(t, m.Commands[0].Commands, 1)
	assert.Equal(t, "d", m.Commands[0].Commands[0].Name)
}

func TestRemoveCommandWithFilesystem(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	workDir := "/work"
	require.NoError(t, fs.MkdirAll(workDir, 0755))

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Tool:   props.Tool{Name: "test-tool"},
	}

	// Mock structure: pkg/cmd/parent/child/cmd.go exists
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent/child"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent"), 0755)) // Ensure parent dir exists
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))

	// Match the default names in prepareSubcommandContext (props, cmd)
	// AND use the correct nested import path: test-tool/pkg/cmd/parent/child
	parentCode := `package parent
import (
	"test-tool/pkg/cmd/parent/child"
	"github.com/spf13/cobra"
)
func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	childCmd := child.NewCmdChild(props)
	cmd.AddCommand(childCmd)
	return cmd
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0644))

	manifest := Manifest{
		Properties: ManifestProperties{Name: "test-tool"},
		Commands: []ManifestCommand{
			{
				Name: "parent",
				Commands: []ManifestCommand{
					{Name: "child"},
				},
			},
		},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	data, _ := yaml.Marshal(manifest)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), data, 0644))

	g := New(p, &Config{Path: workDir, Name: "child", Parent: "parent"})

	err := g.DeregisterSubcommand()
	require.NoError(t, err)

	// Verify parent code modified (import and call removed)
	updatedCode, _ := afero.ReadFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"))
	codeStr := string(updatedCode)
	// Should NOT contain the specific import
	assert.NotContains(t, codeStr, "test-tool/pkg/cmd/parent/child")
	// Should NOT contain the init call logic
	assert.NotContains(t, codeStr, "child.NewCmdChild")
}

func TestRegisterSubcommandWithFilesystem(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	workDir := "/work"
	require.NoError(t, fs.MkdirAll(workDir, 0755))

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Tool:   props.Tool{Name: "test-tool"},
	}

	// Mock structure: pkg/cmd/parent/child will be created/expected by generator logic
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent"), 0755))
	// We don't strictly need the child dir to exist for PARENT modification,
	// but let's create it for correctness if needed by other checks (though registerSubcommand mainly touches parent)
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent/child"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))

	parentCode := `package parent
import (
	"github.com/spf13/cobra"
)
func NewCmdParent(props *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	return cmd
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0644))

	manifest := Manifest{
		Properties: ManifestProperties{Name: "test-tool"},
		Commands: []ManifestCommand{
			{
				Name: "parent",
			},
		},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	data, _ := yaml.Marshal(manifest)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), data, 0644))

	g := New(p, &Config{Path: workDir, Name: "child", Parent: "parent"})

	err := g.RegisterSubcommand()
	require.NoError(t, err)

	// Verify parent code modified (import and call added)
	updatedCode, _ := afero.ReadFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"))
	codeStr := string(updatedCode)
	// It should now contain the nested path import
	assert.Contains(t, codeStr, "\"test-tool/pkg/cmd/parent/child\"")
	assert.Contains(t, codeStr, "child.NewCmdChild(props)")
}
