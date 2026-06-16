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

// TestRegenerateManifest_PreservesDescriptionsThroughWrapper is the regression
// guard for keryx v0.19.0 Bug 2: regenerate manifest blanked command
// Short/Long.
//
// Real generated commands wrap the cobra literal in the setup.Command
// middleware helper — `setup.Wrap("name", &cobra.Command{Use, Short, Long})` —
// so the &cobra.Command literal is an *argument* of the Wrap call, not the bare
// RHS the extractor looked at. The scanner therefore never read Short/Long and
// wrote blank descriptions, which a following regenerate project then rendered
// into every cmd.go, wiping all help text.
func TestRegenerateManifest_PreservesDescriptionsThroughWrapper(t *testing.T) {
	fs := afero.NewMemMapFs()

	var logBuf strings.Builder

	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/social"), 0755))

	rootCode := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/social"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	return gtbRoot.NewCmdRoot(p, social.NewCmdSocial(p))
}`

	// Mirrors real generated output: the cobra literal lives inside setup.Wrap.
	socialCode := `package social
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdSocial(props *props.Props) *setup.Command {
	return setup.Wrap("social", &cobra.Command{
		Use:   "social",
		Short: "social media tools",
		Long:  "manage social media posts and schedules",
	})
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/social/cmd.go"), []byte(socialCode), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	p := &props.Props{FS: fs, Logger: l, Config: config.NewFilesContainer(fs), Tool: props.Tool{Name: "test-tool"}}

	g := New(p, &Config{Path: workDir})
	require.NoError(t, g.RegenerateManifest(context.Background()))

	data, err := afero.ReadFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	require.Len(t, m.Commands, 1)
	assert.Equal(t, "social", m.Commands[0].Name)
	assert.Equal(t, "social media tools", string(m.Commands[0].Description),
		"Short must be recovered from the setup.Wrap-wrapped cobra literal")
	assert.Equal(t, "manage social media posts and schedules", string(m.Commands[0].LongDescription),
		"Long must be recovered from the setup.Wrap-wrapped cobra literal")
}

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

	conf := config.NewFilesContainer(fs)
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

// TestRegenerateManifest_PreservesRegisterWrappedSubcommands is the regression
// guard for the keryx "regenerate drops nested subcommands" data-loss bug.
//
// Real generated code wires a parent to its children through the
// setup.Command middleware wrapper — `cmd.Register(child.NewCmdChild(props))` —
// NOT cobra's `cmd.AddCommand(...)`. The manifest scanner only recognised
// AddCommand, so a Register-wired child was treated as orphaned and dropped
// from the rebuilt manifest, which then made `regenerate project` emit a parent
// with no Register call and destroy the command tree. This test mirrors the
// real generated shape (setup.Wrap + cmd.Register) and asserts the nesting
// survives RegenerateManifest.
func TestRegenerateManifest_PreservesRegisterWrappedSubcommands(t *testing.T) {
	fs := afero.NewMemMapFs()

	var logBuf strings.Builder

	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/reel"), 0755))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/reel/build"), 0755))

	// root registers the top-level command via the variadic NewCmdRoot pattern
	// (which the scanner already handles).
	rootCode := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/reel"
)
func NewCmdRoot(p *props.Props) *setup.Command {
	rootCmd := gtbRoot.NewCmdRoot(p, reel.NewCmdReel(p))
	return rootCmd
}`

	// parent wires its child via the setup.Command wrapper's Register — the
	// pattern the scanner regressed on.
	reelCode := `package reel
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"test-tool/pkg/cmd/reel/build"
)
func NewCmdReel(props *props.Props) *setup.Command {
	cmd := setup.Wrap("reel", &cobra.Command{Use: "reel", Short: "reel cmds"})
	cmd.Register(build.NewCmdBuild(props))
	return cmd
}`

	buildCode := `package build
import (
	"github.com/spf13/cobra"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdBuild(props *props.Props) *setup.Command {
	return setup.Wrap("build", &cobra.Command{Use: "build", Short: "build a reel"})
}`

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/reel/cmd.go"), []byte(reelCode), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/reel/build/cmd.go"), []byte(buildCode), 0644))

	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte("properties:\n  name: test-tool\n"), 0644))

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(fs),
		Tool:   props.Tool{Name: "test-tool"},
	}

	g := New(p, &Config{Path: workDir})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	data, err := afero.ReadFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	require.Len(t, m.Commands, 1, "reel must be recorded as a top-level command")
	assert.Equal(t, "reel", m.Commands[0].Name)

	require.Len(t, m.Commands[0].Commands, 1,
		"the Register-wired child must survive regenerate manifest (not be dropped as orphaned)")
	assert.Equal(t, "build", m.Commands[0].Commands[0].Name)

	assert.NotContains(t, logBuf.String(), "orphaned command build",
		"the child must be linked to its parent, not skipped as orphaned")
}

// TestRegenerateManifest_PreservesFeatures is the regression guard for keryx
// defect B: regenerate manifest must not reset properties.features.
//
// Feature state (especially opt-ins like config/telemetry and the scaffold-only
// keychain) is author configuration that lives in the manifest and cannot be
// losslessly recovered from the generated root command. The scanner previously
// replaced properties.features with a stale re-derivation from root cmd.go
// (which only knew init/update/mcp/docs), silently dropping config, telemetry,
// keychain, doctor and changelog — and the following regenerate project then
// emitted a root with the matching Enable() calls missing. RegenerateManifest
// must preserve the existing manifest's features.
func TestRegenerateManifest_PreservesFeatures(t *testing.T) {
	fs := afero.NewMemMapFs()

	var logBuf strings.Builder

	l := logger.NewCharm(&logBuf)
	workDir := "/work"

	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "go.mod"), []byte("module test-tool\n"), 0644))
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, "pkg/cmd/root"), 0755))

	// The generated root only encodes non-default features as Enable()/Disable()
	// calls; keychain (a scaffold-time decision) never appears here at all.
	rootCode := `package root
import (
	gtbRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)
func NewCmdRoot(v version.Info) (*setup.Command, *props.Props) {
	p := &props.Props{
		Tool: props.Tool{
			Name:        "test-tool",
			Description: "desc",
			ReleaseSource: props.ReleaseSource{Host: "github.com", Owner: "acme", Repo: "test-tool", Type: "github"},
			Features:    props.SetFeatures(props.Enable(props.ConfigCmd), props.Enable(props.TelemetryCmd)),
		},
	}
	rootCmd := gtbRoot.NewCmdRoot(p)
	return rootCmd, p
}`
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, "pkg/cmd/root/cmd.go"), []byte(rootCode), 0644))

	// The existing manifest is the source of truth for features, including
	// keychain (which cannot be recovered from root) and the opt-ins.
	manifestYAML := `properties:
  name: test-tool
  features:
    - name: init
      enabled: true
    - name: update
      enabled: true
    - name: mcp
      enabled: true
    - name: docs
      enabled: true
    - name: doctor
      enabled: true
    - name: changelog
      enabled: true
    - name: keychain
      enabled: true
    - name: config
      enabled: true
    - name: telemetry
      enabled: true
`
	require.NoError(t, fs.MkdirAll(filepath.Join(workDir, ".gtb"), 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"), []byte(manifestYAML), 0644))

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(fs),
		Tool:   props.Tool{Name: "test-tool"},
	}

	g := New(p, &Config{Path: workDir})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	data, err := afero.ReadFile(fs, filepath.Join(workDir, ".gtb/manifest.yaml"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	enabled := map[string]bool{}
	for _, f := range m.Properties.Features {
		enabled[f.Name] = f.Enabled
	}

	// The full author feature set must survive — not be replaced by the lossy
	// init/update/mcp/docs re-derivation.
	for _, name := range []string{"config", "telemetry", "keychain", "doctor", "changelog"} {
		assert.Truef(t, enabled[name], "feature %q must be preserved through regenerate manifest", name)
	}
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

	conf := config.NewFilesContainer(fs)
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

	conf := config.NewFilesContainer(fs)
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
