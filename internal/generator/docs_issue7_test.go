package generator

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newIssue7Generator wires a Generator with a MemMapFs, a config store, and an
// injected mock chat client, matching the fixture used by TestGenerateDocs_Command.
func newIssue7Generator(t *testing.T, root string, mockClient *MockChatClient) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	cfgContainer := testutil.StoreFromYAML(t, "ai:\n  provider: claude\n  model: claude-opus-4-8\n")

	require.NoError(t, fs.MkdirAll(filepath.Join(root, "pkg/cmd/mycmd"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "pkg/cmd/mycmd/mycmd.go"),
		[]byte("package mycmd\n\nimport \"github.com/spf13/cobra\"\n\nvar Command = &cobra.Command{}\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb/manifest.yaml"),
		[]byte("properties:\n  name: mytool\ncommands:\n  - name: mycmd\n    description: My commands\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, "mkdocs.yml"),
		[]byte("nav:\n  - Home: index.md\n"), 0o644))

	g := &Generator{
		props: &props.Props{
			FS:     fs,
			Logger: logger.NewNoop(),
			Config: cfgContainer,
		},
		config: &Config{
			Path:       root,
			Name:       "mycmd",
			AIProvider: "claude",
			AIModel:    "claude-opus-4-8",
		},
		chatClient: mockClient,
	}

	return g, fs
}

// modelResponseWithPreamble mirrors the reporter's captured output: two
// concatenated narration turns ("...documentation.I now have...") ahead of the
// frontmatter, which the generator must strip so the file is frontmatter-first.
const modelResponseWithPreamble = "I'll analyze the code and inspect the referenced packages to ensure accurate documentation." +
	"I now have enough context to generate accurate documentation.\n\n" +
	"---\n" +
	"title: mytool mycmd\n" +
	"description: My command description.\n" +
	"date: 2026-07-28\n" +
	"tags: [cli, command, mycmd]\n" +
	"authors: [A Maintainer <maintainer@example.com>]\n" +
	"---\n\n" +
	"# mytool mycmd\n\n## Description\n\nGenerated body.\n"

// TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter reproduces defect (1)
// from issue #7: when the model's response carries conversational preamble ahead
// of the YAML frontmatter, the generator must strip it so the written file begins
// with `---` and the frontmatter is parsed by static-site generators.
func TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter(t *testing.T) {
	mockClient := new(MockChatClient)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return(modelResponseWithPreamble, nil)

	root := "/work"
	g, fs := newIssue7Generator(t, root, mockClient)

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))

	outputPath := filepath.Join(root, "docs/commands/mycmd/index.md")
	content, err := afero.ReadFile(fs, outputPath)
	require.NoError(t, err)

	got := string(content)

	assert.Truef(t, strings.HasPrefix(got, "---\n"),
		"frontmatter must be the first bytes of the file; got leading content:\n%.120q", got)
	assert.NotContainsf(t, got, "I'll analyze the code",
		"model conversational preamble leaked into the generated doc:\n%.200q", got)
	// The human author present in the model output must survive the strip.
	assert.Containsf(t, got, "A Maintainer <maintainer@example.com>",
		"the human author must be preserved in the written doc:\n%.200q", got)
}

// TestGenerateDocs_Issue7_NoFrontmatterFallsBackToBoilerplate covers the failure
// path: a response with no `---` fence at all must not commit a frontmatter-less
// page — the generator falls back to deterministic boilerplate (which is itself
// frontmatter-first) rather than writing the narration verbatim.
func TestGenerateDocs_Issue7_NoFrontmatterFallsBackToBoilerplate(t *testing.T) {
	narrationOnly := "I'll analyze the code now. I have enough context but produced no document."

	mockClient := new(MockChatClient)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return(narrationOnly, nil)

	root := "/work"
	g, fs := newIssue7Generator(t, root, mockClient)

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))

	outputPath := filepath.Join(root, "docs/commands/mycmd/index.md")
	content, err := afero.ReadFile(fs, outputPath)
	require.NoError(t, err)

	got := string(content)

	assert.Truef(t, strings.HasPrefix(got, "---\n"),
		"fallback boilerplate must be frontmatter-first; got:\n%.120q", got)
	assert.NotContains(t, got, "I'll analyze the code",
		"a frontmatter-less model response must never be committed verbatim")
}

// TestGenerateDocs_Issue7_AuthorsAdditiveByDefault asserts the issue #7 maintainer
// decision for defect (2): by default AI attribution is ADDITIVE — the prompt
// still injects the AI model as a co-author but must instruct the model to
// PRESERVE existing (human) authors rather than replace them.
func TestGenerateDocs_Issue7_AuthorsAdditiveByDefault(t *testing.T) {
	mockClient := new(MockChatClient)
	root := "/work"
	g, _ := newIssue7Generator(t, root, mockClient)

	moduleName := "gitlab.com/example/mytool"
	sysPrompt, _, _ := g.getPromptAndOutput("mycmd", "pkg/cmd/mycmd", moduleName, false)

	lower := strings.ToLower(sysPrompt)

	assert.Contains(t, sysPrompt, "Claude (claude-opus-4-8)",
		"default behaviour appends the AI model as a co-author")
	assert.Contains(t, lower, "append",
		"the AI model must be appended, not made the sole author")
	assert.Contains(t, lower, "preserve",
		"the prompt must instruct preserving existing (human) authors")
	assert.Contains(t, lower, "co-author",
		"the AI model must be described as a co-author, not the author")
}

// TestGenerateDocs_Issue7_NoAIAttributionFlag asserts the --no-ai-attribution
// flag flips the system prompt: the AI/model identity must not appear, the model
// must not be told to append itself, and the authors field must be scoped to the
// project's human author(s) only.
func TestGenerateDocs_Issue7_NoAIAttributionFlag(t *testing.T) {
	mockClient := new(MockChatClient)
	root := "/work"
	g, _ := newIssue7Generator(t, root, mockClient)
	g.config.NoAIAttribution = true

	moduleName := "gitlab.com/example/mytool"
	sysPrompt, _, _ := g.getPromptAndOutput("mycmd", "pkg/cmd/mycmd", moduleName, false)

	lower := strings.ToLower(sysPrompt)

	assert.NotContains(t, sysPrompt, "Claude (claude-opus-4-8)",
		"with --no-ai-attribution the AI model identity must not be injected into the prompt")
	assert.NotContains(t, lower, "append the current ai model",
		"the prompt must not instruct the model to append the AI model to authors")
	assert.Contains(t, lower, "human author",
		"the authors instruction must scope authorship to the project's human author(s)")
}

// TestGenerateDocs_Issue7_NoAIAttributionPackagePrompt confirms the flag applies
// to the package doc path too (both prompts share authorsDirectives).
func TestGenerateDocs_Issue7_NoAIAttributionPackagePrompt(t *testing.T) {
	mockClient := new(MockChatClient)
	root := "/work"
	g, _ := newIssue7Generator(t, root, mockClient)
	g.config.NoAIAttribution = true

	moduleName := "gitlab.com/example/mytool"
	sysPrompt, _, _ := g.getPromptAndOutput("mycmd", "pkg/mycmd", moduleName, true)

	assert.NotContains(t, sysPrompt, "Claude (claude-opus-4-8)",
		"with --no-ai-attribution the package prompt must not inject the AI model identity")
}
