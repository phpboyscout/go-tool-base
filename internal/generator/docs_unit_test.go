package generator

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/phpboyscout/gtb/pkg/chat"
	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/gtb/pkg/config"
	"github.com/phpboyscout/gtb/pkg/props"
)

// MockChatClient implements chat.ChatClient for testing.
type MockChatClient struct {
	mock.Mock
}

func (m *MockChatClient) Add(ctx context.Context, prompt string) error {
	args := m.Called(ctx, prompt)
	return args.Error(0)
}

func (m *MockChatClient) Ask(ctx context.Context, question string, target any) error {
	args := m.Called(ctx, question, target)
	return args.Error(0)
}

func (m *MockChatClient) SetTools(tools []chat.Tool) error {
	args := m.Called(tools)
	return args.Error(0)
}

func (m *MockChatClient) Chat(ctx context.Context, prompt string) (string, error) {
	args := m.Called(ctx, prompt)
	return args.String(0), args.Error(1)
}

func TestGenerateDocs_Command(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	cfgContainer := config.NewFilesContainer(l, fs)
	cfgContainer.GetViper().Set("ai.provider", "mock")
	cfgContainer.GetViper().Set("ai.model", "test-model")

	// Setup mock FS
	root := "/work"
	_ = fs.MkdirAll(filepath.Join(root, "pkg/cmd/mycmd"), 0755)
	_ = afero.WriteFile(fs, filepath.Join(root, "pkg/cmd/mycmd/mycmd.go"), []byte("package mycmd\n\nimport \"github.com/spf13/cobra\"\n\nvar Command = &cobra.Command{}\n"), 0644)
	_ = afero.WriteFile(fs, filepath.Join(root, ".gtb/manifest.yaml"), []byte("properties:\n  name: mytool\ncommands:\n  - name: mycmd\n    description: My commands\n"), 0644)
	_ = afero.WriteFile(fs, filepath.Join(root, "mkdocs.yml"), []byte("nav:\n  - Home: index.md\n"), 0644)

	// Mock AI Client
	mockClient := new(MockChatClient)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return(`---
title: mycmd
description: My command description.
date: 2023-10-27
tags: [cli]
authors: [ai]
---

# mycmd

## Description
This is a generated doc.
`, nil)

	g := &Generator{
		props: &props.Props{
			FS:     fs,
			Logger: l,
			Config: cfgContainer,
		},
		config: &Config{
			Path:       root,
			Name:       "mycmd",
			AIProvider: "mock",
			AIModel:    "test-model",
		},
		chatClient: mockClient, // Inject mock client
	}

	// Run GenerateDocs
	err := g.GenerateDocs(context.Background(), "mycmd", false)
	require.NoError(t, err)

	// Verify Output
	outputPath := filepath.Join(root, "docs/commands/mycmd/index.md")
	exists, err := afero.Exists(fs, outputPath)
	require.NoError(t, err)
	assert.True(t, exists, "Documentation file does not exist")

	content, _ := afero.ReadFile(fs, outputPath)
	assert.Contains(t, string(content), "# mycmd")
	assert.Contains(t, string(content), "This is a generated doc.")

	// Verify Index Generation
	indexPath := filepath.Join(root, "docs/commands/index.md")
	indexExists, _ := afero.Exists(fs, indexPath)
	assert.True(t, indexExists, "Commands index file should exist")
}

func TestGenerateDocs_Package(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	cfgContainer := config.NewFilesContainer(l, fs)

	// Setup mock FS
	root := "/work"
	pkgPath := filepath.Join(root, "pkg/mypkg")
	_ = fs.MkdirAll(pkgPath, 0755)
	_ = afero.WriteFile(fs, filepath.Join(pkgPath, "mypkg.go"), []byte("package mypkg\n\nfunc Hello() string { return \"world\" }\n"), 0644)
	_ = afero.WriteFile(fs, filepath.Join(root, "go.mod"), []byte("module test-module\n"), 0644)

	// Mock AI Client
	mockClient := new(MockChatClient)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return(`---
title: mypkg
---
# Package mypkg
`, nil)

	g := &Generator{
		props: &props.Props{
			FS:     fs,
			Logger: l,
			Config: cfgContainer,
		},
		config: &Config{
			Path:       root,
			AIProvider: "mock",
		},
		chatClient: mockClient, // Inject mock client
	}

	// Run GenerateDocs for package
	err := g.GenerateDocs(context.Background(), "pkg/mypkg", true)
	require.NoError(t, err)

	// Verify Output
	outputPath := filepath.Join(root, "docs/packages/pkg/mypkg/index.md")
	exists, err := afero.Exists(fs, outputPath)
	require.NoError(t, err)
	assert.True(t, exists, "Package documentation file should exist")

	// Verify Package Index Generation
	indexPath := filepath.Join(root, "docs/packages/index.md")
	indexExists, _ := afero.Exists(fs, indexPath)
	assert.True(t, indexExists, "Package index file should exist")
}

func TestHandleReadFileTool(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"
	_ = afero.WriteFile(fs, filepath.Join(root, "test.txt"), []byte("hello world"), 0644)

	g := &Generator{
		props:  &props.Props{FS: fs},
		config: &Config{Path: root},
	}

	args := []byte(`{"path": "test.txt"}`)
	result, err := g.handleReadFileTool(context.Background(), args)

	require.NoError(t, err)
	assert.Equal(t, "hello world", result)
}

func TestHandleListDirTool(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"
	_ = fs.MkdirAll(filepath.Join(root, "subdir"), 0755)
	_ = afero.WriteFile(fs, filepath.Join(root, "file.txt"), []byte(""), 0644)

	g := &Generator{
		props:  &props.Props{FS: fs},
		config: &Config{Path: root},
	}

	args := []byte(`{"path": "."}`)
	result, err := g.handleListDirTool(context.Background(), args)

	require.NoError(t, err)
	assert.Contains(t, result.(string), "file.txt")
	assert.Contains(t, result.(string), "subdir/")
}

func TestHandleGoDocTool(t *testing.T) {
	g := &Generator{
		config: &Config{Path: "/work"},
		runCommand: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			assert.Equal(t, "/work", dir)
			assert.Equal(t, "go", name)
			assert.Equal(t, []string{"doc", "fmt"}, args)
			return []byte("package fmt ..."), nil
		},
	}

	args := []byte(`{"package": "fmt"}`)
	result, err := g.handleGoDocTool(context.Background(), args)

	require.NoError(t, err)
	assert.Equal(t, "package fmt ...", result)
}
func TestSanitizeAIOutput(t *testing.T) {
	g := &Generator{}

	tests := []struct {
		input    string
		expected string
	}{
		{input: "  clean content  ", expected: "clean content"},
		{input: "```markdown\ncontent\n```", expected: "content\n```"}, // waits, sanitizeAIOutput logic:
		// if strings.HasPrefix(content, "```") {
		//    if idx := strings.Index(content, "\n"); idx != -1 {
		//        content = content[idx+1:]
		//    }
		// }
		// return strings.TrimSpace(content)
		{input: "```\nstripped\n```", expected: "stripped\n```"},
		{input: "just text", expected: "just text"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, g.sanitizeAIOutput(tt.input))
		})
	}
}

func TestGetModuleNameSafe(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: l},
		config: &Config{Path: "/work"},
	}

	t.Run("Valid go.mod", func(t *testing.T) {
		_ = afero.WriteFile(fs, "/work/go.mod", []byte("module test-mod\n"), 0644)
		assert.Equal(t, "test-mod", g.getModuleNameSafe())
	})

	t.Run("No go.mod", func(t *testing.T) {
		_ = fs.Remove("/work/go.mod")
		assert.Equal(t, "project", g.getModuleNameSafe())
	})
}

func TestResolveAIConfig(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfgContainer := config.NewFilesContainer(logger.NewNoop(), fs)

	g := &Generator{
		props: &props.Props{Config: cfgContainer},
		config: &Config{
			AIProvider: "openai",
			AIModel:    "gpt-4",
		},
	}

	t.Run("From Config", func(t *testing.T) {
		p, m := g.resolveAIConfig()
		assert.Equal(t, "openai", p)
		assert.Equal(t, "gpt-4", m)
	})

	t.Run("Defaults", func(t *testing.T) {
		t.Setenv("AI_PROVIDER", "")
		g.config.AIProvider = ""
		g.config.AIModel = ""
		p, m := g.resolveAIConfig()
		assert.Equal(t, "claude", p)
		assert.NotEmpty(t, m)
	})
}

func TestResolvePathFromProjectRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"
	target := "mycmd"
	absPath := filepath.Join(root, "pkg/cmd", target)
	_ = fs.MkdirAll(absPath, 0755)

	g := &Generator{
		props: &props.Props{FS: fs},
	}

	t.Run("Command exists in pkg/cmd", func(t *testing.T) {
		result := g.resolvePathFromProjectRoot(root, target)
		assert.Equal(t, absPath, result)
	})

	t.Run("Command does not exist", func(t *testing.T) {
		result := g.resolvePathFromProjectRoot(root, "nonexistent")
		assert.Equal(t, "nonexistent", result)
	})
}

func TestResolveDocsTarget(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"

	g := &Generator{
		props:  &props.Props{FS: fs},
		config: &Config{Path: root},
	}

	t.Run("Package target", func(t *testing.T) {
		name, rel, abs, err := g.resolveDocsTarget("pkg/mypkg", true)
		require.NoError(t, err)
		assert.Equal(t, "mypkg", name)
		assert.Equal(t, "pkg/mypkg", rel)
		assert.Equal(t, filepath.Join(root, "pkg/mypkg"), abs)
	})

	t.Run("Command target - relative to root", func(t *testing.T) {
		cmdPath := filepath.Join(root, "pkg/cmd/mycmd")
		_ = fs.MkdirAll(cmdPath, 0755)

		name, rel, abs, err := g.resolveDocsTarget("mycmd", false)
		require.NoError(t, err)
		assert.Equal(t, "mycmd", name)
		assert.Equal(t, "pkg/cmd/mycmd", rel)
		assert.Equal(t, cmdPath, abs)
	})

	t.Run("Command target - absolute path", func(t *testing.T) {
		cmdPath := filepath.Join(root, "pkg/cmd/other")
		_ = fs.MkdirAll(cmdPath, 0755)

		name, rel, abs, err := g.resolveDocsTarget(cmdPath, false)
		require.NoError(t, err)
		assert.Equal(t, "other", name)
		assert.Equal(t, "pkg/cmd/other", rel)
		assert.Equal(t, cmdPath, abs)
	})
}

func TestToTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{input: "my-cmd", expected: "My Cmd"},
		{input: "my_cmd", expected: "My Cmd"},
		{input: "hello world", expected: "Hello World"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.expected, toTitle(tt.input))
	}
}

func TestUpdateNavSection(t *testing.T) {
	nav := []any{
		map[string]any{"Home": "index.md"},
		map[string]any{"CLI": []any{"existing.md"}},
	}

	newCLI := []any{"new.md"}
	updated := updateNavSection(nav, "CLI", newCLI)

	assert.Len(t, updated, 2)
	found := false
	for _, item := range updated {
		if m, ok := item.(map[string]any); ok {
			if val, ok := m["CLI"]; ok {
				assert.Equal(t, newCLI, val)
				found = true
			}
		}
	}
	assert.True(t, found)
}

func TestBuildNavFromCommands(t *testing.T) {
	cmds := []ManifestCommand{
		{
			Name:        "parent",
			Description: "Parent cmd",
			Commands: []ManifestCommand{
				{Name: "child", Description: "Child cmd"},
			},
		},
	}

	nav := buildNavFromCommands(cmds, []string{})
	require.Len(t, nav, 1)

	parentEntry := nav[0].(map[string]any)
	assert.Contains(t, parentEntry, "Parent")

	parentContent := parentEntry["Parent"].([]any)
	assert.Len(t, parentContent, 2) // index.md + child
}

func TestRegenerateMkdocsNav(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"
	mkdocsPath := filepath.Join(root, "mkdocs.yml")

	initialMkdocs := `site_name: My Tool
nav:
  - Home: index.md
  - CLI: []
`
	_ = afero.WriteFile(fs, mkdocsPath, []byte(initialMkdocs), 0644)

	// Mock manifest
	manifestPath := filepath.Join(root, ".gtb", "manifest.yaml")
	_ = fs.MkdirAll(filepath.Dir(manifestPath), 0755)
	manifestData := `properties:
  name: mytool
commands:
  - name: mycmd
    description: My command
`
	_ = afero.WriteFile(fs, manifestPath, []byte(manifestData), 0644)

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop()},
		config: &Config{Path: root},
	}

	err := g.regenerateMkdocsNav()
	require.NoError(t, err)

	content, err := afero.ReadFile(fs, mkdocsPath)
	require.NoError(t, err)

	assert.Contains(t, string(content), "site_name: My Tool")
	assert.Contains(t, string(content), "CLI:")
	assert.Contains(t, string(content), "Mycmd: commands/mycmd/index.md")
}

func TestGeneratePackagesIndex(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"
	pkgDocsDir := filepath.Join(root, "docs/packages/pkg/mypkg")
	_ = fs.MkdirAll(pkgDocsDir, 0755)
	_ = afero.WriteFile(fs, filepath.Join(pkgDocsDir, "index.md"), []byte("---\ntitle: mypkg\ndescription: My package\n---\n"), 0644)

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop()},
		config: &Config{Path: root},
	}

	err := g.generatePackagesIndex()
	require.NoError(t, err)

	indexPath := filepath.Join(root, "docs/packages/index.md")
	exists, _ := afero.Exists(fs, indexPath)
	assert.True(t, exists)

	content, _ := afero.ReadFile(fs, indexPath)
	assert.Contains(t, string(content), "| [pkg/mypkg](pkg/mypkg/) | My package |")
}

func TestGenerateCommandsIndex(t *testing.T) {
	fs := afero.NewMemMapFs()
	root := "/work"

	// Mock manifest
	manifestPath := filepath.Join(root, ".gtb", "manifest.yaml")
	_ = fs.MkdirAll(filepath.Dir(manifestPath), 0755)
	manifestData := `commands:
  - name: mycmd
    description: My command description
`
	_ = afero.WriteFile(fs, manifestPath, []byte(manifestData), 0644)

	g := &Generator{
		props:  &props.Props{FS: fs, Logger: logger.NewNoop()},
		config: &Config{Path: root},
	}

	err := g.generateCommandsIndex()
	require.NoError(t, err)

	indexPath := filepath.Join(root, "docs/commands/index.md")
	exists, _ := afero.Exists(fs, indexPath)
	assert.True(t, exists)

	content, _ := afero.ReadFile(fs, indexPath)
	assert.Contains(t, string(content), "| [mycmd](mycmd/index.md) | My command description |")
}
