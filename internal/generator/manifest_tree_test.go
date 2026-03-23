package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestRegenerateManifestRecursive(t *testing.T) {
	fs := afero.NewMemMapFs()
	var logBuf strings.Builder
	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))

	// mock project structure
	// pkg/cmd/root/cmd.go -> calls parent.NewCmdParent
	// pkg/cmd/parent/cmd.go -> calls child.NewCmdChild, has a flag
	// pkg/cmd/parent/child/cmd.go

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent/child"), 0755))

	rootCode := `package root
import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/parent"
)
func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "root"}
	cmd.AddCommand(parent.NewCmdParent(p))
	return cmd
}`

	parentCode := `package parent
import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/parent/child"
)
func NewCmdParent(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent", Short: "parent desc"}
	cmd.Flags().String("parent-flag", "", "desc")
	cmd.Flags().String("a", "", "")
	cmd.Flags().String("b", "", "")
	cmd.Flags().String("c", "", "")
	cmd.Flags().String("d", "", "")
	cmd.Flags().String("h", "", "")
	cmd.MarkFlagRequired("parent-flag")
	cmd.MarkFlagsMutuallyExclusive("a", "b")
	cmd.MarkFlagsRequiredTogether("c", "d")
	cmd.Flags().MarkHidden("h")
	cmd.AddCommand(child.NewCmdChild(p))
	return cmd
}`

	childCode := `package child
import "github.com/spf13/cobra"
func NewCmdChild(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "child", Short: "child desc"}
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/child/cmd.go"), []byte(childCode), 0644))

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	conf := config.NewFilesContainer(nil, fs)
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
		Tool:   props.Tool{Name: "test-tool"},
	}

	g := New(p, &Config{Path: workDir})

	err := g.RegenerateManifest(context.Background())
	require.NoError(t, err)

	// Verify manifest
	manifestPath := filepath.Join(workDir, ".gtb/manifest.yaml")
	data, err := afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	err = yaml.Unmarshal(data, &m)
	require.NoError(t, err)

	assert.Equal(t, "test-tool", m.Properties.Name)
	require.Len(t, m.Commands, 1)
	assert.Equal(t, "parent", m.Commands[0].Name)
	assert.Equal(t, "parent desc", string(m.Commands[0].Description))
	require.Len(t, m.Commands[0].Flags, 6)
	assert.Equal(t, "parent-flag", m.Commands[0].Flags[0].Name)
	assert.True(t, m.Commands[0].Flags[0].Required)
	assert.True(t, m.Commands[0].Flags[5].Hidden) // 'h' is the 6th flag

	require.Len(t, m.Commands[0].MutuallyExclusive, 1)
	assert.ElementsMatch(t, []string{"a", "b"}, m.Commands[0].MutuallyExclusive[0])

	require.Len(t, m.Commands[0].RequiredTogether, 1)
	assert.ElementsMatch(t, []string{"c", "d"}, m.Commands[0].RequiredTogether[0])

	require.Len(t, m.Commands[0].Commands, 1)
	assert.Equal(t, "child", m.Commands[0].Commands[0].Name)
	assert.Equal(t, "child desc", string(m.Commands[0].Commands[0].Description))
}

func TestScanCommands_OrphansAndDuplicates(t *testing.T) {
	fs := afero.NewMemMapFs()
	var logBuf strings.Builder
	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))

	// Structure:
	// pkg/cmd/root -> root
	// pkg/cmd/orphan -> cmd "orphan" (not added to root)
	// pkg/cmd/dup1 -> cmd "dup"
	// pkg/cmd/dup2 -> cmd "dup" (duplicate name)

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/orphan"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/dup1"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/dup2"), 0755))

	rootCode := `package root
import "github.com/spf13/cobra"
func NewCmdRoot(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "root"}
}`
	orphanCode := `package orphan
import "github.com/spf13/cobra"
func NewCmdOrphan(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "orphan"}
}`
	dup1Code := `package dup1
import "github.com/spf13/cobra"
func NewCmdDup1(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "dup"}
}`
	dup2Code := `package dup2
import "github.com/spf13/cobra"
func NewCmdDup2(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "dup"} // Duplicate name
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/orphan/cmd.go"), []byte(orphanCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/dup1/cmd.go"), []byte(dup1Code), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/dup2/cmd.go"), []byte(dup2Code), 0644))

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	conf := config.NewFilesContainer(nil, fs)
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
		Tool:   props.Tool{Name: "test-tool"},
	}

	g := New(p, &Config{Path: workDir})

	// scanCommands is private, but RegenerateManifest calls it.
	// However, RegenerateManifest only uses key "root" logic?
	// scanCommands:
	//   for _, root := range roots {
	//     if root.cmd.Name == "root" -> add children
	//     else -> warn orphan
	//   }
	// The duplicates "dup" are effectively orphans because they are not connected to "root".
	// But scanCommands logic will handle them as orphans and log warnings.
	// AND if we manage to get them into "commands" list?
	// scanCommands logic filters for "root" to build the tree.
	// To test "duplicate command name", we need them to be CHILDREN of root (or some reachable node).

	// Let's modify:
	// root -> adds dup1
	// root -> adds dup2

	// But dup1 and dup2 have same Use: "dup".

	rootCodeWithDups := `package root
import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/dup1"
	"test-tool/pkg/cmd/dup2"
)
func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "root"}
	cmd.AddCommand(dup1.NewCmdDup1(p))
	cmd.AddCommand(dup2.NewCmdDup2(p))
	return cmd
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCodeWithDups), 0644))

	// Now run RegenerateManifest
	err := g.RegenerateManifest(context.Background())
	require.NoError(t, err)

	logs := logBuf.String()

	// Check for orphan warning
	assert.Contains(t, logs, "Skipping orphaned command orphan")

	// Check for duplicate warning
	// "Duplicate command name detected: dup. Renamed to dup-1"
	assert.Contains(t, logs, "Duplicate command name detected: dup")

	// Verify manifest content
	manifestPath := filepath.Join(workDir, ".gtb/manifest.yaml")
	data, err := afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	err = yaml.Unmarshal(data, &m)
	require.NoError(t, err)

	// We expect 2 commands in root's list: "dup" and "dup-1" (sorted)
	// Actually root is skipped in Top Level Commands list?
	// scanCommands returns:
	//    if root.cmd.Name == "root" { appendCmd(g.buildCmdTree(child)) }
	// So "commands" will contain children of root.

	// dup and dup-1
	require.Len(t, m.Commands, 2)
	names := []string{m.Commands[0].Name, m.Commands[1].Name}
	assert.Contains(t, names, "dup")
	assert.Contains(t, names, "dup-2")
}

func TestScanCommands_RecursiveDuplicates(t *testing.T) {
	fs := afero.NewMemMapFs()
	var logBuf strings.Builder
	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))

	// root -> parent
	// parent -> child1 (Use: "x")
	// parent -> child2 (Use: "x")

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/parent"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/child1"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/child2"), 0755))

	rootCode := `package root
import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/parent"
)
func NewCmdRoot(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "root"}
	cmd.AddCommand(parent.NewCmdParent(p))
	return cmd
}`
	parentCode := `package parent
import (
	"github.com/spf13/cobra"
	"test-tool/pkg/cmd/child1"
	"test-tool/pkg/cmd/child2"
)
func NewCmdParent(p *props.Props) *cobra.Command {
	cmd := &cobra.Command{Use: "parent"}
	cmd.AddCommand(child1.NewCmdChild1(p))
	cmd.AddCommand(child2.NewCmdChild2(p))
	return cmd
}`
	child1Code := `package child1
import "github.com/spf13/cobra"
func NewCmdChild1(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "x"}
}`
	child2Code := `package child2
import "github.com/spf13/cobra"
func NewCmdChild2(p *props.Props) *cobra.Command {
	return &cobra.Command{Use: "x"} // Duplicate name
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/parent/cmd.go"), []byte(parentCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/child1/cmd.go"), []byte(child1Code), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/child2/cmd.go"), []byte(child2Code), 0644))

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	conf := config.NewFilesContainer(nil, fs)
	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: conf,
		Tool:   props.Tool{Name: "test-tool"},
	}

	g := New(p, &Config{Path: workDir})

	err := g.RegenerateManifest(context.Background())
	require.NoError(t, err)

	// Verify manifest
	manifestPath := filepath.Join(workDir, ".gtb/manifest.yaml")
	data, err := afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	err = yaml.Unmarshal(data, &m)
	require.NoError(t, err)

	require.Len(t, m.Commands, 1) // parent
	assert.Equal(t, "parent", m.Commands[0].Name)
	require.Len(t, m.Commands[0].Commands, 2) // x and x-2

	childNames := []string{m.Commands[0].Commands[0].Name, m.Commands[0].Commands[1].Name}
	assert.Contains(t, childNames, "x")
	assert.Contains(t, childNames, "x-2")
}
