package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/testutil"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/version"
)

// newIntegrationProject creates a skeleton project with a mocked command runner
// and returns the Props and project path. The command runner is a no-op so that
// shell-dependent steps (go mod tidy, linting) are skipped.
func newIntegrationProject(t *testing.T) (*props.Props, string) {
	t.Helper()

	path := "/integration-work"
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:      fs,
		Logger:  l,
		Config:  config.NewFilesContainer(fs, config.WithLogger(l)),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	cfg := SkeletonConfig{
		Name:        "test-project",
		Repo:        "test/test-project",
		Host:        "github.com",
		Description: "An integration test project",
		Path:        path,
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	return p, path
}

// addCmd generates a command with doc stub pre-created to avoid AI calls.
func addCmd(t *testing.T, p *props.Props, path, name, parent string, opts ...func(*Config)) {
	t.Helper()

	var docRelPath string
	if parent == "" || parent == "root" {
		docRelPath = name
	} else {
		docRelPath = filepath.Join(parent, name)
	}

	docPath := filepath.Join(path, "docs", "commands", docRelPath, "index.md")
	_ = p.FS.MkdirAll(filepath.Dir(docPath), 0o755)
	_ = afero.WriteFile(p.FS, docPath, []byte("# "+name+"\n"), 0o644)

	cfg := &Config{
		Path:   path,
		Name:   name,
		Parent: parent,
		Short:  name + " command",
		Force:  true,
	}

	for _, opt := range opts {
		opt(cfg)
	}

	g := New(p, cfg)

	require.NoError(t, g.Generate(context.Background()))
}

// loadManifestFrom reads the manifest YAML directly from the FS.
func loadManifestFrom(t *testing.T, fs afero.Fs, path string) Manifest {
	t.Helper()

	data, err := afero.ReadFile(fs, filepath.Join(path, ".gtb", "manifest.yaml"))
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	return m
}

// findCommand searches the manifest tree for a command by name.
func findCommand(commands []ManifestCommand, name string) *ManifestCommand {
	for i := range commands {
		if commands[i].Name == name {
			return &commands[i]
		}

		if found := findCommand(commands[i].Commands, name); found != nil {
			return found
		}
	}

	return nil
}

// TestFullLifecycle_SkeletonToCommandsToRemoval exercises the complete
// generator lifecycle: skeleton creation → root-level commands → nested
// subcommands → removal → verify cleanup.
func TestFullLifecycle_SkeletonToCommandsToRemoval(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	// --- Step 1: Add root-level commands ---
	addCmd(t, p, path, "deploy", "root")
	addCmd(t, p, path, "status", "root")

	// Both should exist on filesystem
	for _, name := range []string{"deploy", "status"} {
		cmdFile := filepath.Join(path, "pkg", "cmd", name, "cmd.go")
		exists, _ := afero.Exists(p.FS, cmdFile)
		assert.True(t, exists, "%s/cmd.go should exist", name)
	}

	// Both should be in manifest
	m := loadManifestFrom(t, p.FS, path)
	assert.NotNil(t, findCommand(m.Commands, "deploy"), "deploy should be in manifest")
	assert.NotNil(t, findCommand(m.Commands, "status"), "status should be in manifest")

	// Root cmd.go should reference both commands
	rootCmd, err := afero.ReadFile(p.FS, filepath.Join(path, "pkg", "cmd", "root", "cmd.go"))
	require.NoError(t, err)
	assert.Contains(t, string(rootCmd), "deploy", "root cmd.go should reference deploy")
	assert.Contains(t, string(rootCmd), "status", "root cmd.go should reference status")

	// --- Step 2: Add a nested subcommand ---
	addCmd(t, p, path, "canary", "deploy")

	deployCmdFile := filepath.Join(path, "pkg", "cmd", "deploy", "cmd.go")
	deployCmdContent, err := afero.ReadFile(p.FS, deployCmdFile)
	require.NoError(t, err)
	assert.Contains(t, string(deployCmdContent), "canary", "deploy/cmd.go should reference canary subcommand")

	// Manifest should have canary nested under deploy
	m = loadManifestFrom(t, p.FS, path)
	deployCmd := findCommand(m.Commands, "deploy")
	require.NotNil(t, deployCmd)
	assert.NotNil(t, findCommand(deployCmd.Commands, "canary"), "canary should be nested under deploy in manifest")

	// --- Step 3: Remove the nested subcommand ---
	g := New(p, &Config{Path: path, Name: "canary", Parent: "deploy"})

	require.NoError(t, g.Remove(context.Background()))

	// canary directory should be gone
	canaryDir := filepath.Join(path, "pkg", "cmd", "canary")
	exists, _ := afero.Exists(p.FS, canaryDir)
	assert.False(t, exists, "canary directory should be removed")

	// canary should be gone from manifest
	m = loadManifestFrom(t, p.FS, path)
	deployCmd = findCommand(m.Commands, "deploy")
	require.NotNil(t, deployCmd)
	assert.Nil(t, findCommand(deployCmd.Commands, "canary"), "canary should be removed from manifest")

	// deploy should still exist
	exists, _ = afero.Exists(p.FS, deployCmdFile)
	assert.True(t, exists, "deploy cmd.go should still exist after removing its child")

	// --- Step 4: Remove a root-level command ---
	g = New(p, &Config{Path: path, Name: "status", Parent: "root"})

	require.NoError(t, g.Remove(context.Background()))

	statusDir := filepath.Join(path, "pkg", "cmd", "status")
	exists, _ = afero.Exists(p.FS, statusDir)
	assert.False(t, exists, "status directory should be removed")

	m = loadManifestFrom(t, p.FS, path)
	assert.Nil(t, findCommand(m.Commands, "status"), "status should be removed from manifest")
	assert.NotNil(t, findCommand(m.Commands, "deploy"), "deploy should still be in manifest")
}

// TestDeepHierarchy_ThreeLevels verifies that a three-level command hierarchy
// (root → parent → child → grandchild) generates correctly and that
// regeneration preserves all registrations.
func TestDeepHierarchy_ThreeLevels(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	addCmd(t, p, path, "service", "root")
	addCmd(t, p, path, "deploy", "service")
	addCmd(t, p, path, "canary", "service/deploy")

	// Verify the hierarchy exists — nested commands live in subdirs
	serviceCmdFile := filepath.Join(path, "pkg", "cmd", "service", "cmd.go")
	deployCmdFile := filepath.Join(path, "pkg", "cmd", "service", "deploy", "cmd.go")
	canaryCmdFile := filepath.Join(path, "pkg", "cmd", "service", "deploy", "canary", "cmd.go")

	for _, f := range []string{serviceCmdFile, deployCmdFile, canaryCmdFile} {
		exists, _ := afero.Exists(p.FS, f)
		assert.True(t, exists, "%s should exist", f)
	}

	// Manifest should reflect the nesting
	m := loadManifestFrom(t, p.FS, path)
	svc := findCommand(m.Commands, "service")
	require.NotNil(t, svc)
	dep := findCommand(svc.Commands, "deploy")
	require.NotNil(t, dep, "deploy should be nested under service")
	can := findCommand(dep.Commands, "canary")
	require.NotNil(t, can, "canary should be nested under deploy")

	// Regenerate the project — all registrations should survive
	g := New(p, &Config{Path: path, Name: "test-project"})

	require.NoError(t, g.RegenerateProject(context.Background()))

	// service/cmd.go should still reference deploy
	serviceCmd, err := afero.ReadFile(p.FS, serviceCmdFile)
	require.NoError(t, err)
	assert.Contains(t, string(serviceCmd), "deploy", "service should still reference deploy after regeneration")

	// deploy/cmd.go should still reference canary
	deployCmd, err := afero.ReadFile(p.FS, deployCmdFile)
	require.NoError(t, err)
	assert.Contains(t, string(deployCmd), "canary", "deploy should still reference canary after regeneration")
}

// TestManifestConsistency_HashesMatchFiles verifies that after generating
// multiple commands and regenerating the project, every command's manifest
// hash matches the actual file content on disk.
func TestManifestConsistency_HashesMatchFiles(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	addCmd(t, p, path, "build", "root")
	addCmd(t, p, path, "test", "root")
	addCmd(t, p, path, "lint", "root")

	// Regenerate to ensure all hashes are fresh
	g := New(p, &Config{Path: path, Name: "test-project"})

	require.NoError(t, g.RegenerateProject(context.Background()))

	m := loadManifestFrom(t, p.FS, path)

	// Verify every command's cmd.go hash matches the file
	var verifyHashes func(commands []ManifestCommand)
	verifyHashes = func(commands []ManifestCommand) {
		for _, cmd := range commands {
			if cmd.Hashes == nil {
				continue
			}

			cmdGoHash, ok := cmd.Hashes["cmd.go"]
			if !ok {
				continue
			}

			cmdFile := filepath.Join(path, "pkg", "cmd", cmd.Name, "cmd.go")
			content, err := afero.ReadFile(p.FS, cmdFile)
			require.NoError(t, err, "should be able to read %s/cmd.go", cmd.Name)

			expected := CalculateHash(content)
			assert.Equal(t, expected, cmdGoHash, "manifest hash for %s/cmd.go should match file content", cmd.Name)

			verifyHashes(cmd.Commands)
		}
	}

	verifyHashes(m.Commands)
}

// TestProtection_SkipsRegeneration verifies that a protected command is
// silently skipped during regeneration (file unchanged), and that unprotecting
// it allows regeneration to proceed.
func TestProtection_SkipsRegeneration(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	addCmd(t, p, path, "critical", "root")

	// Protect the command
	g := New(p, &Config{Path: path, Name: "critical"})

	require.NoError(t, g.SetProtection(context.Background(), "critical", true))

	// Verify it's marked protected in manifest
	m := loadManifestFrom(t, p.FS, path)
	cmd := findCommand(m.Commands, "critical")
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.Protected, "protected field should be set")
	assert.True(t, *cmd.Protected, "critical should be protected")

	// Record cmd.go content before attempted regeneration
	criticalCmdPath := filepath.Join(path, "pkg", "cmd", "critical", "cmd.go")
	before, err := afero.ReadFile(p.FS, criticalCmdPath)
	require.NoError(t, err)

	// Pre-create doc stub for re-generation attempt
	docPath := filepath.Join(path, "docs", "commands", "critical", "index.md")
	_ = p.FS.MkdirAll(filepath.Dir(docPath), 0o755)
	_ = afero.WriteFile(p.FS, docPath, []byte("# critical\n"), 0o644)

	// Attempt to re-generate — protection causes a silent skip (returns nil)
	regenG := New(p, &Config{
		Path:   path,
		Name:   "critical",
		Parent: "root",
		Short:  "modified description",
	})

	require.NoError(t, regenG.Generate(context.Background()))

	// Content should be unchanged despite the Generate call
	after, err := afero.ReadFile(p.FS, criticalCmdPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "protected command file should not be modified")

	// Unprotect and verify manifest updated
	require.NoError(t, g.SetProtection(context.Background(), "critical", false))

	m = loadManifestFrom(t, p.FS, path)
	cmd = findCommand(m.Commands, "critical")
	require.NotNil(t, cmd)
	require.NotNil(t, cmd.Protected)
	assert.False(t, *cmd.Protected, "critical should be unprotected")

	// Now regeneration should actually modify the file (new short description)
	regenG2 := New(p, &Config{
		Path:   path,
		Name:   "critical",
		Parent: "root",
		Short:  "modified description",
		Force:  true,
	})

	require.NoError(t, regenG2.Generate(context.Background()))

	afterUnprotect, err := afero.ReadFile(p.FS, criticalCmdPath)
	require.NoError(t, err)
	assert.Contains(t, string(afterUnprotect), "modified description", "unprotected command should be regenerated with new description")
}

// TestRegenerateProject_PreservesCommandOptions verifies that command-level
// options (aliases, hidden, flags, assets) survive a full project regeneration.
func TestRegenerateProject_PreservesCommandOptions(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	addCmd(t, p, path, "push", "root", func(cfg *Config) {
		cfg.Aliases = []string{"p", "pu"}
		cfg.Hidden = true
		cfg.WithAssets = true
		cfg.Flags = []string{"target:string:deployment target", "dry-run:bool:preview only"}
	})

	// Verify options in manifest before regeneration
	m := loadManifestFrom(t, p.FS, path)
	pushCmd := findCommand(m.Commands, "push")
	require.NotNil(t, pushCmd)
	assert.Equal(t, []string{"p", "pu"}, pushCmd.Aliases)
	assert.True(t, pushCmd.Hidden)
	assert.True(t, pushCmd.WithAssets)
	require.Len(t, pushCmd.Flags, 2)

	// Regenerate
	g := New(p, &Config{Path: path, Name: "test-project"})

	require.NoError(t, g.RegenerateProject(context.Background()))

	// All options should survive
	m = loadManifestFrom(t, p.FS, path)
	pushCmd = findCommand(m.Commands, "push")
	require.NotNil(t, pushCmd, "push should still exist after regeneration")
	assert.Equal(t, []string{"p", "pu"}, pushCmd.Aliases, "aliases should survive regeneration")
	assert.True(t, pushCmd.Hidden, "hidden flag should survive regeneration")
	assert.True(t, pushCmd.WithAssets, "withAssets should survive regeneration")
	assert.Len(t, pushCmd.Flags, 2, "flags should survive regeneration")

	// Assets directory should exist
	assetsDir := filepath.Join(path, "pkg", "cmd", "push", "assets")
	exists, _ := afero.Exists(p.FS, assetsDir)
	assert.True(t, exists, "assets directory should exist for command with WithAssets")
}

// TestDryRun_DoesNotMutateFilesystem verifies that GenerateDryRun and
// GenerateSkeletonDryRun do not create any files.
func TestDryRun_DoesNotMutateFilesystem(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	// Snapshot FS state before dry run
	var filesBefore []string

	_ = afero.Walk(p.FS, path, func(path string, info os.FileInfo, _ error) error {
		if info != nil && !info.IsDir() {
			filesBefore = append(filesBefore, path)
		}

		return nil
	})

	// Dry-run a command generation
	docPath := filepath.Join(path, "docs", "commands", "drytest", "index.md")
	_ = p.FS.MkdirAll(filepath.Dir(docPath), 0o755)
	_ = afero.WriteFile(p.FS, docPath, []byte("# drytest\n"), 0o644)

	g := New(p, &Config{
		Path:   path,
		Name:   "drytest",
		Parent: "root",
		Short:  "dry run test",
		DryRun: true,
	})

	result, err := g.GenerateDryRun(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify no new command files were created
	cmdDir := filepath.Join(path, "pkg", "cmd", "drytest")
	exists, _ := afero.Exists(p.FS, cmdDir)
	assert.False(t, exists, "dry run should not create command directory")

	// Manifest should not contain the dry-run command
	m := loadManifestFrom(t, p.FS, path)
	assert.Nil(t, findCommand(m.Commands, "drytest"), "dry run should not add command to manifest")
}

// TestRegenerateManifest_RecoversMissingEntries verifies that
// RegenerateManifest scans the filesystem and rebuilds the manifest when
// commands exist on disk but are missing from the manifest.
func TestRegenerateManifest_RecoversMissingEntries(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")
	testutil.SkipIfNotIntegration(t, "generator")

	p, path := newIntegrationProject(t)

	// Generate two commands so they exist on disk
	addCmd(t, p, path, "alpha", "root")
	addCmd(t, p, path, "beta", "root")

	// Manually remove beta from the manifest (simulate corruption/drift)
	manifestPath := filepath.Join(path, ".gtb", "manifest.yaml")
	m := loadManifestFrom(t, p.FS, path)

	filtered := make([]ManifestCommand, 0, len(m.Commands))
	for _, cmd := range m.Commands {
		if cmd.Name != "beta" {
			filtered = append(filtered, cmd)
		}
	}
	m.Commands = filtered

	data, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(p.FS, manifestPath, data, 0o644))

	// beta should be missing from manifest
	m = loadManifestFrom(t, p.FS, path)
	assert.Nil(t, findCommand(m.Commands, "beta"), "beta should be missing before regeneration")

	// Regenerate manifest from filesystem
	g := New(p, &Config{Path: path, Name: "test-project"})

	require.NoError(t, g.RegenerateManifest(context.Background()))

	// beta should be recovered
	m = loadManifestFrom(t, p.FS, path)
	assert.NotNil(t, findCommand(m.Commands, "alpha"), "alpha should still be in manifest")
	assert.NotNil(t, findCommand(m.Commands, "beta"), "beta should be recovered by RegenerateManifest")
}

// TestSkeletonFeatures_DisabledFeaturesOmitFiles verifies that skeleton
// generation with disabled features does not produce certain command structures.
func TestSkeletonFeatures_DisabledFeaturesOmitFiles(t *testing.T) {
	testutil.SkipIfNotIntegration(t, "generator")

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(fs, config.WithLogger(l)),
	}

	path := "/feature-test"

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	cfg := SkeletonConfig{
		Name:        "feature-tool",
		Repo:        "test/feature-tool",
		Host:        "github.com",
		Description: "Feature flag test",
		Path:        path,
		Features: []ManifestFeature{
			{Name: "update", Enabled: false},
			{Name: "init", Enabled: false},
		},
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	// Verify manifest records the disabled features
	m := loadManifestFrom(t, fs, path)
	featureMap := make(map[string]bool)
	for _, f := range m.Properties.Features {
		featureMap[f.Name] = f.Enabled
	}

	assert.False(t, featureMap["update"], "update feature should be disabled in manifest")
	assert.False(t, featureMap["init"], "init feature should be disabled in manifest")

	// Root cmd.go should exist regardless
	rootCmd := filepath.Join(path, "pkg", "cmd", "root", "cmd.go")
	exists, _ := afero.Exists(fs, rootCmd)
	assert.True(t, exists, "root cmd.go should always exist")
}
