package generator

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

// regenerateWithClient scaffolds a one-command project on an in-memory
// filesystem with a chat provider configured and an injected client, then
// regenerates it. The mock records every call; the assertions are on that.
func regenerateWithClient(t *testing.T, cfg *Config, prime ...func(*MockChatClient)) *MockChatClient {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Config:  testutil.StoreFromYAML(t, "ai:\n  provider: mock\n"),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	root := "/work"
	require.NoError(t, fs.MkdirAll(root+"/.gtb", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\n  docs_layout: diataxis\nversion:\n  gtb: v1.0.0\ncommands:\n  - name: existing\n"), 0o644))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module test-mod\n"), 0o644))
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/root", 0o755))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\nfunc NewCmdRoot(p interface{}) {}\n"), 0o644))

	cfg.Path = root

	client := new(MockChatClient)
	for _, f := range prime {
		f(client)
	}

	g := New(p, cfg)
	g.chatClient = client
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }

	require.NoError(t, g.RegenerateProject(context.Background()))

	return client
}

// TestRegenerateProject_DoesNotCallAIWithoutUpdateDocs (#35): a regenerate
// is a deterministic rewrite of what the manifest describes. A missing doc
// page gets boilerplate; the AI path is what --update-docs asks for.
func TestRegenerateProject_DoesNotCallAIWithoutUpdateDocs(t *testing.T) {
	t.Parallel()

	client := regenerateWithClient(t, &Config{})

	client.AssertNotCalled(t, "Chat")
	client.AssertNotCalled(t, "Ask")
}

// TestRegenerateProject_UpdateDocsIsTheAsk: with --update-docs the same run
// consults the provider for the page.
func TestRegenerateProject_UpdateDocsIsTheAsk(t *testing.T) {
	t.Parallel()

	client := regenerateWithClient(t, &Config{UpdateDocs: true}, func(c *MockChatClient) {
		c.On("Chat", mock.Anything, mock.Anything).Return("---\ntitle: existing\n---\n# existing\n", nil)
	})

	client.AssertCalled(t, "Chat", mock.Anything, mock.Anything)
}

// TestAIChoice_CIConfigKey (#35, spec 0211 D8): under --ci a person's
// generator.ai is not read, and --ai is still an explicit ask.
func TestAIChoice_CIConfigKey(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	ci := testutil.StoreFromYAML(t, "ci: true\ngenerator:\n  ai: true\n")

	fromConfig := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop(), Config: ci}, config: &Config{}}
	assert.False(t, fromConfig.aiChoice().wanted, "generator.ai under --ci")

	fromFlag := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop(), Config: ci}, config: &Config{AI: true}}
	assert.True(t, fromFlag.aiChoice().wanted, "--ai is an explicit ask")
}
