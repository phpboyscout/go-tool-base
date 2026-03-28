package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/go-tool-base/internal/generator/templates"
	"github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

// setupTestProject creates a minimal in-memory project via GenerateSkeleton
// and returns the Props that can be shared across generator instances.
func setupTestProject(t *testing.T, path string) *props.Props {
	t.Helper()

	fs := afero.NewMemMapFs()
	l := logger.NewNoop()

	p := &props.Props{
		FS:     fs,
		Logger: l,
		Config: config.NewFilesContainer(l, fs),
	}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	cfg := SkeletonConfig{
		Name:        "test-project",
		Repo:        "test/test-project",
		Host:        "github.com",
		Description: "A test project",
		Path:        path,
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	return p
}

// generateCmd calls Generate for a single command within the project at path.
// It pre-creates a doc stub so that handleDocumentationGeneration skips the AI
// generation path (which would make tests hang on network calls).
func generateCmd(t *testing.T, p *props.Props, path, name, parent string) {
	t.Helper()

	// Build the doc path respecting the parent hierarchy so the generator's
	// "docs already exist" check hits the right file.
	var docRelPath string
	if parent == "" || parent == "root" {
		docRelPath = name
	} else {
		docRelPath = filepath.Join(parent, name)
	}

	docPath := filepath.Join(path, "docs", "commands", docRelPath, "index.md")
	_ = p.FS.MkdirAll(filepath.Dir(docPath), 0o755)
	_ = afero.WriteFile(p.FS, docPath, []byte("# "+name+"\n"), 0o644)

	g := New(p, &Config{
		Path:   path,
		Name:   name,
		Parent: parent,
		Short:  name + " command",
		Force:  true,
	})

	require.NoError(t, g.Generate(context.Background()))
}

// TestPipeline_reRegisterChildren_noopWhenNoManifest verifies that
// reRegisterChildCommands returns nil (not an error) when no manifest exists.
func TestPipeline_reRegisterChildren_noopWhenNoManifest(t *testing.T) {
	fs := afero.NewMemMapFs()

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
	}

	g := New(p, &Config{Path: "/work", Name: "start"})

	cmdDir := "/work/pkg/cmd/start"
	require.NoError(t, fs.MkdirAll(cmdDir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "cmd.go"), []byte("package start"), 0o644))

	err := g.reRegisterChildCommands(cmdDir, map[string]string{})

	assert.NoError(t, err)
}

// TestPipeline_reRegisterChildren_noopWhenNoChildren verifies that
// reRegisterChildCommands is a no-op when the command has no children in the manifest.
func TestPipeline_reRegisterChildren_noopWhenNoChildren(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	path := "/work"
	p := setupTestProject(t, path)

	generateCmd(t, p, path, "start", "root")

	// start has no children — reRegisterChildCommands should be a no-op
	startCmdPath := filepath.Join(path, "pkg", "cmd", "start", "cmd.go")

	before, err := afero.ReadFile(p.FS, startCmdPath)
	require.NoError(t, err)

	g := New(p, &Config{Path: path, Name: "start", Parent: "root"})

	require.NoError(t, g.reRegisterChildCommands(filepath.Join(path, "pkg", "cmd", "start"), map[string]string{}))

	after, err := afero.ReadFile(p.FS, startCmdPath)
	require.NoError(t, err)

	assert.Equal(t, string(before), string(after), "cmd.go should be unchanged when command has no children")
}

// TestPipeline_skipRegistration_optionHonoured verifies that parent cmd.go is
// not modified when PipelineOptions.SkipRegistration is true.
func TestPipeline_skipRegistration_optionHonoured(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	path := "/work"
	p := setupTestProject(t, path)

	generateCmd(t, p, path, "start", "root")

	// Record root cmd.go before we run the pipeline for a new "status" command
	rootCmdPath := filepath.Join(path, "pkg", "cmd", "root", "cmd.go")

	rootBefore, err := afero.ReadFile(p.FS, rootCmdPath)
	require.NoError(t, err)

	// Create a minimal cmd.go and doc stub for "status" so the pipeline does not
	// attempt AI doc generation (which would hang on network calls in tests).
	statusCmdDir := filepath.Join(path, "pkg", "cmd", "status")
	require.NoError(t, p.FS.MkdirAll(statusCmdDir, 0o755))
	require.NoError(t, afero.WriteFile(p.FS, filepath.Join(statusCmdDir, "cmd.go"), []byte("package status\n"), 0o644))

	statusDocPath := filepath.Join(path, "docs", "commands", "status", "index.md")
	require.NoError(t, p.FS.MkdirAll(filepath.Dir(statusDocPath), 0o755))
	require.NoError(t, afero.WriteFile(p.FS, statusDocPath, []byte("# status\n"), 0o644))

	g := New(p, &Config{Path: path, Name: "status", Parent: "root"})

	data := templates.CommandData{
		Name:    "status",
		Package: "status",
		Hashes:  map[string]string{"cmd.go": "abc123"},
	}

	pipeline := newCommandPipeline(g, PipelineOptions{SkipRegistration: true})
	_, err = pipeline.Run(context.Background(), data, statusCmdDir)

	require.NoError(t, err)

	rootAfter, err := afero.ReadFile(p.FS, rootCmdPath)
	require.NoError(t, err)

	assert.Equal(t, string(rootBefore), string(rootAfter), "root cmd.go must not change when SkipRegistration=true")
}

// TestPipeline_persistManifest_updatesHashAfterRegistration verifies that
// after a full Generate the manifest hash for cmd.go matches the generated file.
func TestPipeline_persistManifest_updatesHashAfterRegistration(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	path := "/work"
	p := setupTestProject(t, path)

	generateCmd(t, p, path, "start", "root")

	g := New(p, &Config{Path: path, Name: "start"})

	m, err := g.loadManifest()
	require.NoError(t, err)

	var startCmd *ManifestCommand

	for i := range m.Commands {
		if m.Commands[i].Name == "start" {
			startCmd = &m.Commands[i]

			break
		}
	}

	require.NotNil(t, startCmd, "start command should be in manifest")

	cmdFile := filepath.Join(path, "pkg", "cmd", "start", "cmd.go")

	fileContent, err := afero.ReadFile(p.FS, cmdFile)
	require.NoError(t, err)

	expected := CalculateHash(fileContent)

	assert.Equal(t, expected, startCmd.Hashes["cmd.go"], "manifest hash should match generated cmd.go content")
}

// TestGenerateAndRegenerate is an end-to-end integration test that verifies:
//  1. Child AddCommand registrations survive a re-generate of the parent command.
//  2. Child AddCommand registrations survive a full RegenerateProject.
//  3. Manifest hashes remain consistent with file content after regeneration.
func TestGenerateAndRegenerate(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	path := "/work"
	p := setupTestProject(t, path)

	generateCmd(t, p, path, "start", "root")
	generateCmd(t, p, path, "stop", "start")

	startCmdPath := filepath.Join(path, "pkg", "cmd", "start", "cmd.go")

	// Confirm stop is registered in start after initial generation
	content, err := afero.ReadFile(p.FS, startCmdPath)
	require.NoError(t, err)
	require.Contains(t, string(content), "stop", "stop must be registered in start/cmd.go after generation")

	// Re-generate start — stop registration must survive the overwrite
	generateCmd(t, p, path, "start", "root")

	content, err = afero.ReadFile(p.FS, startCmdPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "stop", "stop registration must survive re-generating the start command")

	// Full RegenerateProject — stop registration must survive
	g := New(p, &Config{Path: path, Name: "test-project"})

	require.NoError(t, g.RegenerateProject(context.Background()))

	content, err = afero.ReadFile(p.FS, startCmdPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "stop", "stop registration must survive RegenerateProject")

	// Verify manifest hash for start/cmd.go is consistent with the file on disk
	m, err := g.loadManifest()
	require.NoError(t, err)

	for _, cmd := range m.Commands {
		if cmd.Name != "start" {
			continue
		}

		fileContent, err := afero.ReadFile(p.FS, startCmdPath)
		require.NoError(t, err)

		expected := CalculateHash(fileContent)

		assert.Equal(t, expected, cmd.Hashes["cmd.go"], "manifest hash must match start/cmd.go file content after RegenerateProject")
	}
}

// TestRegenerateProject_preservesHelpConfig verifies that help channel config
// stored in the manifest survives a RegenerateProject call.  Before the fix,
// regenerateRootCommand did not read ManifestHelp and silently zeroed the
// HelpType / SlackChannel / etc. fields in the regenerated root/cmd.go.
func TestRegenerateProject_preservesHelpConfig(t *testing.T) {
	path := "/work"
	p := setupTestProject(t, path)

	// Patch the manifest on disk to include help config.
	manifestPath := filepath.Join(path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(p.FS, manifestPath)
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	m.Properties.Help = ManifestHelp{
		Type:         "slack",
		SlackChannel: "#platform-alerts",
		SlackTeam:    "my-workspace",
	}

	updated, err := yaml.Marshal(m)
	require.NoError(t, err)
	require.NoError(t, afero.WriteFile(p.FS, manifestPath, updated, 0o644))

	// Run regeneration.
	g := New(p, &Config{Path: path, Name: "test-project"})
	require.NoError(t, g.RegenerateProject(context.Background()))

	// The rendered root/cmd.go should contain the slack channel string.
	rootCmdPath := filepath.Join(path, "pkg", "cmd", "root", "cmd.go")

	content, err := afero.ReadFile(p.FS, rootCmdPath)
	require.NoError(t, err)

	assert.Contains(t, string(content), "#platform-alerts", "slack channel must survive RegenerateProject")
	assert.Contains(t, string(content), "my-workspace", "slack team must survive RegenerateProject")
}
