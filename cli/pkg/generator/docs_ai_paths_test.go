package generator

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"
)

const aiDocWithFrontmatter = "---\ntitle: mycmd\ndescription: d\n---\n\n# mycmd\n\nFrom the model.\n"

// streamingChatClient adds StreamChat to the mock, so writeAIDocs takes its
// streaming branch.
type streamingChatClient struct {
	MockChatClient
}

func (m *streamingChatClient) StreamChat(ctx context.Context, prompt string, callback gochat.StreamCallback, _ ...gochat.Media) (string, error) {
	args := m.Called(ctx, prompt)

	if err := callback(gochat.StreamEvent{Type: gochat.EventTextDelta, Delta: "---"}); err != nil {
		return "", err
	}

	return args.String(0), args.Error(1)
}

func readDoc(t *testing.T, fs afero.Fs, root string) string {
	t.Helper()

	data, err := afero.ReadFile(fs, filepath.Join(root, "docs/commands/mycmd/index.md"))
	require.NoError(t, err)

	return string(data)
}

func TestGenerateDocs_DryRunWritesNothing(t *testing.T) {
	t.Parallel()

	root := "/work"
	g, fs := newIssue7Generator(t, root, new(MockChatClient))
	g.config.DryRun = true

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))

	exists, err := afero.Exists(fs, filepath.Join(root, "docs"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestGenerateDocs_UnknownTargetFails(t *testing.T) {
	t.Parallel()

	g, _ := newIssue7Generator(t, "/work", new(MockChatClient))

	require.Error(t, g.GenerateDocs(context.Background(), "pkg/absent", true))
}

func TestGenerateDocs_StreamingClient(t *testing.T) {
	t.Parallel()

	root := "/work"
	client := new(streamingChatClient)
	client.On("StreamChat", mock.Anything, mock.Anything).Return(aiDocWithFrontmatter, nil)

	g, fs := newIssue7Generator(t, root, nil)
	g.chatClient = client

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))
	assert.Contains(t, readDoc(t, fs, root), "From the model.")
}

func TestGenerateDocs_ChatFailureIsReturned(t *testing.T) {
	t.Parallel()

	client := new(MockChatClient)
	client.On("Chat", mock.Anything, mock.Anything).Return("", assert.AnError)

	g, _ := newIssue7Generator(t, "/work", client)

	err := g.GenerateDocs(context.Background(), "mycmd", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AI request failed")
}

func TestGenerateDocs_IgnoredDocPathIsNotWritten(t *testing.T) {
	t.Parallel()

	root := "/work"
	client := new(MockChatClient)
	client.On("Chat", mock.Anything, mock.Anything).Return(aiDocWithFrontmatter, nil)

	g, fs := newIssue7Generator(t, root, client)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb/ignore"), []byte("docs/**\n"), DefaultFileMode))

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))

	exists, err := afero.Exists(fs, filepath.Join(root, "docs/commands/mycmd/index.md"))
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestGenerateDocs_LinkedProviderGetsTheProjectTools(t *testing.T) {
	t.Parallel()

	root := "/work"
	client := new(MockChatClient)
	client.On("SetTools", mock.MatchedBy(func(tools []gochat.Tool) bool {
		return len(tools) == 3 && tools[0].Name == "read_file" && tools[1].Name == "list_dir" && tools[2].Name == "go_doc"
	})).Return(nil)
	client.On("Chat", mock.Anything, mock.Anything).Return(aiDocWithFrontmatter, nil)

	provider := registerTestProvider("gtb-generator-test-docs-ok", client)

	g, fs := newIssue7Generator(t, root, nil)
	g.chatClient = nil
	g.config.AIProvider = string(provider)

	require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))
	assert.Contains(t, readDoc(t, fs, root), "From the model.")
	client.AssertExpectations(t)
}

func TestGenerateDocs_ClientSetupFailureFallsBackToBoilerplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		provider func() gochat.Provider
	}{
		{
			name: "tools refused",
			provider: func() gochat.Provider {
				client := new(MockChatClient)
				client.On("SetTools", mock.Anything).Return(assert.AnError)

				return registerTestProvider("gtb-generator-test-docs-notools", client)
			},
		},
		{
			name:     "provider not linked",
			provider: func() gochat.Provider { return "gtb-generator-test-docs-unlinked" },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := "/work"
			g, fs := newIssue7Generator(t, root, nil)
			g.chatClient = nil
			g.config.AIProvider = string(tt.provider())

			require.NoError(t, g.GenerateDocs(context.Background(), "mycmd", false))
			assert.NotEmpty(t, readDoc(t, fs, root))
		})
	}
}

func TestDocTools_RejectBadArguments(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/work"})

	type tool func(context.Context, json.RawMessage) (any, error)

	tests := []struct {
		name string
		tool tool
		args string
		want string
	}{
		{name: "read malformed json", tool: g.handleReadFileTool, args: "{", want: "failed to parse tool arguments"},
		{name: "list malformed json", tool: g.handleListDirTool, args: "{", want: "failed to parse tool arguments"},
		{name: "go doc malformed json", tool: g.handleGoDocTool, args: "{", want: "failed to parse tool arguments"},
		{name: "read escapes root", tool: g.handleReadFileTool, args: `{"path":"../../etc/passwd"}`, want: "escapes the project root"},
		{name: "read missing file", tool: g.handleReadFileTool, args: `{"path":"absent.go"}`, want: "failed to read file"},
		{name: "list escapes root", tool: g.handleListDirTool, args: `{"path":"../.."}`, want: "escapes the project root"},
		{name: "list missing dir", tool: g.handleListDirTool, args: `{"path":"absent"}`, want: "failed to list dir"},
		{name: "go doc refuses a shell-ish package", tool: g.handleGoDocTool, args: `{"package":"fmt; rm -rf /"}`, want: ErrInvalidPackageName.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := tt.tool(context.Background(), json.RawMessage(tt.args))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestHandleGoDocTool_ReportsTheCommandOutputOnFailure(t *testing.T) {
	t.Parallel()

	g := &Generator{
		config: &Config{Path: "/work"},
		runCommand: func(context.Context, string, string, ...string) ([]byte, error) {
			return []byte("no such package"), assert.AnError
		},
	}

	_, err := g.handleGoDocTool(context.Background(), []byte(`{"package":"nope"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such package")
}

func TestReadSourceFailures(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})
	require.NoError(t, fs.MkdirAll("/work/pkg/empty", DefaultDirMode))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/empty/only_test.go", []byte("package empty\n"), DefaultFileMode))

	_, err := g.readCommandSource("/work/pkg/cmd/absent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat command source path")

	_, err = g.readPackageSource("/work/pkg/empty")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .go files found")

	_, err = g.readPackageSource("/work/pkg/absent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read package directory")
}

func TestCapitalizeAndReadExistingDocs(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/work"})

	assert.Empty(t, g.capitalize(""))
	assert.Equal(t, "Widget", g.capitalize("widget"))

	assert.Empty(t, g.readExistingDocs("/work/docs/absent.md"))

	require.NoError(t, afero.WriteFile(fs, "/work/docs/present.md", []byte("# hi\n"), DefaultFileMode))
	assert.Equal(t, "# hi\n", g.readExistingDocs("/work/docs/present.md"))
}
