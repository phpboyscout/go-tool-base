package generate

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/internal/generator"
)

func TestCommandRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	// Mock project structure in a subdir
	var err error
	projectRoot := "./test-project"
	err = fs.MkdirAll(projectRoot, 0755)
	require.NoError(t, err)

	// Mock go.mod in project root
	err = afero.WriteFile(fs, filepath.Join(projectRoot, "go.mod"), []byte("module github.com/phpboyscout/test-project\n"), 0644)
	require.NoError(t, err)

	// Mock parent command
	parentDir := filepath.Join(projectRoot, "pkg/cmd/root")
	err = fs.MkdirAll(parentDir, 0755)
	require.NoError(t, err)

	parentContent := `package root

import (
	"github.com/spf13/cobra"
	"github.com/phpboyscout/gtb/pkg/props"
)

func NewCmdRoot(props *props.Props) *cobra.Command {
	var cmd = &cobra.Command{
		Use:   "test-project",
	}
	return cmd
}
`
	err = afero.WriteFile(fs, filepath.Join(parentDir, "cmd.go"), []byte(parentContent), 0644)
	require.NoError(t, err)

	// Mock manifest
	manifestDir := filepath.Join(projectRoot, ".gtb")
	err = fs.MkdirAll(manifestDir, 0755)
	require.NoError(t, err)

	manifestContent := `properties:
  name: test-project
github:
  org: test-org
  repo: test-project
version:
  gtb: v1.0.0
`
	err = afero.WriteFile(fs, filepath.Join(manifestDir, "manifest.yaml"), []byte(manifestContent), 0644)
	require.NoError(t, err)

	opts := CommandOptions{
		Name:       "test-cmd",
		Short:      "A test command",
		Long:       "A longer description",
		Path:       projectRoot,
		WithAssets: true,
		Parent:     "root",

		Flags: []string{"name:string:Your name", "count:int:Number of items", "verbose:bool:Verbose output"},
	}

	err = opts.Run(context.Background(), p)
	require.NoError(t, err)

	expectedFiles := []string{
		filepath.Join(projectRoot, "pkg/cmd/test-cmd/cmd.go"),
		filepath.Join(projectRoot, "pkg/cmd/test-cmd/main.go"),
		filepath.Join(projectRoot, "pkg/cmd/test-cmd/assets/init/config.yaml"),
		filepath.Join(projectRoot, "docs/commands/test-cmd/index.md"),
		filepath.Join(projectRoot, ".gtb/manifest.yaml"),
	}

	for _, f := range expectedFiles {
		exists, err := afero.Exists(fs, f)
		assert.NoError(t, err)
		assert.True(t, exists, "file %s should exist", f)
	}

	// Verify go file content
	content, err := afero.ReadFile(fs, filepath.Join(projectRoot, "pkg/cmd/test-cmd/cmd.go"))
	assert.NoError(t, err)
	assert.Contains(t, string(content), "func NewCmdTestCmd")
	assert.Contains(t, string(content), "func NewCmdTestCmd")
	assert.Contains(t, string(content), "*cobra.Command")
	assert.Contains(t, string(content), "props.Assets.Register(\"test-cmd\", &assets)")
	assert.Contains(t, string(content), "return cmd")
	assert.Contains(t, string(content), "Use:   \"test-cmd\"")
	assert.Contains(t, string(content), "RunE: func(cmd *cobra.Command, args []string) error")
	assert.Contains(t, string(content), "return RunTestCmd(cmd.Context(), props, opts, args)")
	assert.Contains(t, string(content), "type TestCmdOptions struct")
	assert.Regexp(t, `Name\s+string`, string(content))
	assert.Regexp(t, `Count\s+int`, string(content))
	assert.Regexp(t, `Verbose\s+bool`, string(content))
	assert.Contains(t, string(content), "cmd.Flags().StringVar(&opts.Name, \"name\", \"\", \"Your name\")")
	assert.Contains(t, string(content), "cmd.Flags().IntVar(&opts.Count, \"count\", 0, \"Number of items\")")
	assert.Contains(t, string(content), "cmd.Flags().BoolVar(&opts.Verbose, \"verbose\", false, \"Verbose output\")")

	// Verify parent was updated correctly
	parentUpdated, err := afero.ReadFile(fs, filepath.Join(projectRoot, "pkg/cmd/root/cmd.go"))
	assert.NoError(t, err)
	assert.Contains(t, string(parentUpdated), "cmd.AddCommand(test_cmd.NewCmdTestCmd(props))")

	// Verify docs
	docContent, err := afero.ReadFile(fs, filepath.Join(projectRoot, "docs/commands/test-cmd/index.md"))
	assert.NoError(t, err)
	assert.Contains(t, string(docContent), "# test-cmd")
	assert.Contains(t, string(docContent), "A test command")

	// Verify manifest was updated
	manifestUpdated, err := afero.ReadFile(fs, filepath.Join(projectRoot, ".gtb/manifest.yaml"))
	assert.NoError(t, err)
	assert.Contains(t, string(manifestUpdated), "- name: test-cmd")
	assert.Contains(t, string(manifestUpdated), "description: A test command")
	assert.NotContains(t, string(manifestUpdated), "parent: root")
}

func TestCommandRun_PathTargeting(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	// Mock project structure
	projectRoot := "./test-project"
	fs.MkdirAll(projectRoot, 0755)
	afero.WriteFile(fs, filepath.Join(projectRoot, "go.mod"), []byte("module test-project\n"), 0644)

	// Mock manifest with duplicate 'cat'
	manifestDir := filepath.Join(projectRoot, ".gtb")
	fs.MkdirAll(manifestDir, 0755)
	manifestContent := `properties:
  name: test
version:
  gtb: v1.0.0
commands:
- name: dog
  description: Dog command
  commands:
  - name: cat
    description: Dog's cat
- name: cat
  description: Root's cat
`
	afero.WriteFile(fs, filepath.Join(manifestDir, "manifest.yaml"), []byte(manifestContent), 0644)

	// Mock parent files
	fs.MkdirAll(filepath.Join(projectRoot, "pkg/cmd/cat"), 0755)
	afero.WriteFile(fs, filepath.Join(projectRoot, "pkg/cmd/cat/cmd.go"), []byte("package cat\nimport \"github.com/spf13/cobra\"\nimport \"github.com/phpboyscout/gtb/pkg/props\"\nfunc NewCmdCat(props *props.Props) *cobra.Command {\n\tcmd := &cobra.Command{Use: \"cat\"}\n\treturn cmd\n}\n"), 0644)

	fs.MkdirAll(filepath.Join(projectRoot, "pkg/cmd/dog/cat"), 0755)
	afero.WriteFile(fs, filepath.Join(projectRoot, "pkg/cmd/dog/cat/cmd.go"), []byte("package cat\nimport \"github.com/spf13/cobra\"\nimport \"github.com/phpboyscout/gtb/pkg/props\"\nfunc NewCmdCat(props *props.Props) *cobra.Command {\n\tcmd := &cobra.Command{Use: \"cat\"}\n\treturn cmd\n}\n"), 0644)

	// Target the ROOT cat
	opts := CommandOptions{
		Name:   "mouse",
		Short:  "Mouse command",
		Path:   projectRoot,
		Parent: "/cat",
	}

	err := opts.Run(context.Background(), p)
	require.NoError(t, err)

	// Verify mouse was generated in pkg/cmd/cat/mouse (not pkg/cmd/dog/cat/mouse)
	mousePath := filepath.Clean(filepath.Join(projectRoot, "pkg/cmd/cat/mouse/cmd.go"))
	exists, err := afero.Exists(fs, mousePath)
	assert.NoError(t, err)
	assert.True(t, exists, "mouse should be under root cat")

	dogCatMousePath := filepath.Clean(filepath.Join(projectRoot, "pkg/cmd/dog/cat/mouse/cmd.go"))
	exists, _ = afero.Exists(fs, dogCatMousePath)
	assert.False(t, exists, "mouse should NOT be under dog's cat")

	// Target the DOG'S cat
	opts2 := CommandOptions{
		Name:   "flea",
		Short:  "Flea command",
		Path:   projectRoot,
		Parent: "dog/cat",
	}

	err = opts2.Run(context.Background(), p)
	require.NoError(t, err)

	fleaPath := filepath.Clean(filepath.Join(projectRoot, "pkg/cmd/dog/cat/flea/cmd.go"))
	exists, err = afero.Exists(fs, fleaPath)
	assert.NoError(t, err)
	assert.True(t, exists, "flea should be under dog's cat")
}

func TestCommandRun_SubcommandNoAssets(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	// Mock project structure
	projectRoot := "./test-project"
	fs.MkdirAll(projectRoot, 0755)
	afero.WriteFile(fs, filepath.Join(projectRoot, "go.mod"), []byte("module test-project\n"), 0644)

	parentDir := filepath.Join(projectRoot, "pkg/cmd/root")
	fs.MkdirAll(parentDir, 0755)
	parentContent := `package root
import (
	"github.com/spf13/cobra"
	"github.com/phpboyscout/gtb/pkg/props"
)
func NewCmdRoot(props *props.Props) *cobra.Command {
	return &cobra.Command{Use: "root"}
}
`
	afero.WriteFile(fs, filepath.Join(parentDir, "cmd.go"), []byte(parentContent), 0644)

	manifestDir := filepath.Join(projectRoot, ".gtb")
	fs.MkdirAll(manifestDir, 0755)
	afero.WriteFile(fs, filepath.Join(manifestDir, "manifest.yaml"), []byte("properties:\n  name: test\nversion:\n  gtb: v1.0.0\n"), 0644)

	opts := CommandOptions{
		Name:       "no-assets-cmd",
		Short:      "No assets",
		Path:       projectRoot,
		WithAssets: false,
		Parent:     "root",
	}

	err := opts.Run(context.Background(), p)
	require.NoError(t, err)

	// Verify go file content
	content, err := afero.ReadFile(fs, filepath.Join(projectRoot, "pkg/cmd/no-assets-cmd/cmd.go"))
	assert.NoError(t, err)
	assert.NotContains(t, string(content), "embed")
	assert.NotContains(t, string(content), "allAssets")
	assert.Contains(t, string(content), "return cmd")
	assert.NotRegexp(t, `return .*allAssets`, string(content))

	// Verify parent was updated correctly (no asset collection)
	parentUpdated, err := afero.ReadFile(fs, filepath.Join(projectRoot, "pkg/cmd/root/cmd.go"))
	assert.NoError(t, err)
	assert.Contains(t, string(parentUpdated), "cmd.AddCommand(no_assets_cmd.NewCmdNoAssetsCmd(props))")
	assert.NotContains(t, string(parentUpdated), "append(allAssets")
}

func TestCommandRun_NoManifest(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	opts := CommandOptions{
		Name:  "test-cmd",
		Short: "A test command",
		Path:  ".",
	}

	err := opts.Run(context.Background(), p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not a gtb project")
}

func TestPascalCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"kube", "Kube"},
		{"aks-login", "AksLogin"},
		{"test_command", "TestCommand"},
		{"multi word command", "MultiWordCommand"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, generator.PascalCase(tt.input))
	}
}
