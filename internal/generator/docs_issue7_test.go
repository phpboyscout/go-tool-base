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

// TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter reproduces defect (1)
// from issue #7: when the model's response carries conversational preamble ahead
// of the YAML frontmatter, the generator writes it straight through, so the file
// no longer begins with `---` and the frontmatter stops being parsed.
//
// RED on current main: sanitizeAIOutput only trims whitespace / a leading code
// fence, so the preamble survives and byte 0 is not `-`.
func TestGenerateDocs_Issue7_PreambleStrippedAboveFrontmatter(t *testing.T) {
	// Mirrors the reporter's captured output: two concatenated narration turns
	// ("...documentation.I now have...") ahead of the frontmatter.
	modelResponse := "I'll analyze the code and inspect the referenced packages to ensure accurate documentation." +
		"I now have enough context to generate accurate documentation.\n\n" +
		"---\n" +
		"title: mytool mycmd\n" +
		"description: My command description.\n" +
		"date: 2026-07-28\n" +
		"tags: [cli, command, mycmd]\n" +
		"authors: [A Maintainer <maintainer@example.com>]\n" +
		"---\n\n" +
		"# mytool mycmd\n\n## Description\n\nGenerated body.\n"

	mockClient := new(MockChatClient)
	mockClient.On("Chat", mock.Anything, mock.Anything).Return(modelResponse, nil)

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
}

// TestGenerateDocs_Issue7_AuthorsNotModelIdentity reproduces defect (2) from
// issue #7: the command documentation prompt instructs the model to write the AI
// model identity (e.g. "Claude (claude-opus-4-8)") into the frontmatter authors
// field, asserting AI authorship in a committed, machine-readable field.
//
// RED on current main: getPromptAndOutput injects aiAuthor into the prompt and
// tells the model it MUST append the current AI model to authors.
func TestGenerateDocs_Issue7_AuthorsNotModelIdentity(t *testing.T) {
	mockClient := new(MockChatClient)
	root := "/work"
	g, _ := newIssue7Generator(t, root, mockClient)

	moduleName := "gitlab.com/example/mytool"
	sysPrompt, _, _ := g.getPromptAndOutput("mycmd", "pkg/cmd/mycmd", moduleName, false)

	assert.NotContainsf(t, sysPrompt, "Claude (claude-opus-4-8)",
		"the AI model identity must not be injected into the authors instruction")
	assert.NotContainsf(t, strings.ToLower(sysPrompt), "append the current ai model",
		"the prompt must not instruct the model to append the AI model to authors")
}
