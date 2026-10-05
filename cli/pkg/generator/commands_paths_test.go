package generator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/verifier"
	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// registerTestProvider links a chat provider under a name no other test uses,
// so the registry's process-wide state cannot leak between tests.
func registerTestProvider(name string, client gochat.ChatClient) gochat.Provider {
	provider := gochat.Provider(name)

	gochat.RegisterProvider(provider, func(context.Context, gochat.Settings) (gochat.ChatClient, error) {
		return client, nil
	})

	return provider
}

func newCommandProject(t *testing.T, cfgYAML string, cfg *Config) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/work/go.mod", []byte("module example.com/tool\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml",
		[]byte("properties:\n  name: tool\ncommands:\n  - name: parent\n    flags:\n      - name: verbose\n        type: bool\n        persistent: true\n"), DefaultFileMode))

	store := emptyTestStore(t)
	if cfgYAML != "" {
		store = testutil.StoreFromYAML(t, cfgYAML)
	}

	cfg.Path = "/work"
	if cfg.Name == "" {
		cfg.Name = "widget"
	}

	return New(&props.Props{FS: fs, Logger: logger.NewNoop(), Config: store}, cfg), fs
}

func TestParseFlags_ReadsEveryPositionalField(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Flags: []string{
		"name",
		"count:int",
		"tag:string:A tag:true:t:true:dflt:true",
	}})

	got := g.parseFlags()
	require.Len(t, got, 3)

	assert.Equal(t, CommandFlag{Name: "name", Type: "string"}, got[0])
	assert.Equal(t, CommandFlag{Name: "count", Type: "int"}, got[1])
	assert.Equal(t, CommandFlag{
		Name:          "tag",
		Type:          "string",
		Description:   "A tag",
		Persistent:    true,
		Shorthand:     "t",
		Required:      true,
		Default:       "dflt",
		DefaultIsCode: true,
	}, got[2])

	persistent, normal := g.categorizeFlags(got)
	require.Len(t, persistent, 1)
	assert.Equal(t, "tag", persistent[0].Name)
	assert.Len(t, normal, 2)
}

func TestConvertManifestFlagsToTemplate(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{})

	got := g.convertManifestFlagsToTemplate([]ManifestFlag{{
		Name: "out", Type: "string", Description: "Output", Persistent: true,
		Shorthand: "o", Default: "x", Required: true, Hidden: true,
	}})

	require.Len(t, got, 1)
	assert.Equal(t, templates.CommandFlag{
		Name: "out", Type: "string", Description: "Output", Persistent: true,
		Shorthand: "o", Default: "x", Required: true, Hidden: true,
	}, got[0])
}

func TestResolveAncestralFlags_CarriesTheParentsPersistentFlags(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{Parent: "parent"})

	got := g.resolveAncestralFlags()
	require.Len(t, got, 1)
	assert.Equal(t, "verbose", got[0].Name)
}

func TestResolveInput(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	script := filepath.Join(dir, "script.sh")
	require.NoError(t, os.WriteFile(script, []byte("echo hi\n"), 0o600))

	promptFile := filepath.Join(dir, "prompt.txt")
	require.NoError(t, os.WriteFile(promptFile, []byte("build a widget"), 0o600))

	tests := []struct {
		name    string
		cfg     Config
		want    string
		wantErr string
	}{
		{name: "script", cfg: Config{ScriptPath: script}, want: "echo hi\n"},
		{name: "missing script", cfg: Config{ScriptPath: filepath.Join(dir, "absent")}, wantErr: "failed to read script"},
		{name: "prompt naming a file", cfg: Config{Prompt: promptFile}, want: "build a widget"},
		{name: "literal prompt", cfg: Config{Prompt: "just do it"}, want: "just do it"},
		{name: "nothing", cfg: Config{}, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := tt.cfg
			g, _ := newPureGenerator(t, &cfg)

			got, err := g.resolveInput()
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestProcessAIGeneration_NothingToConvert(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{})
	data := &templates.CommandData{Name: "widget"}

	assert.Nil(t, g.processAIGeneration(context.Background(), data, nil))
	assert.Empty(t, data.Logic)
}

func TestProcessAIGeneration_UnlinkedProviderLeavesAPlaceholder(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{
		Prompt:     "make a widget",
		AIProvider: "gtb-generator-test-unlinked",
	})

	data := &templates.CommandData{Name: "widget", Package: "widget", PascalName: "Widget"}
	flags := []CommandFlag{{Name: "size", Type: "int", Description: "How big"}}

	assert.Nil(t, g.processAIGeneration(context.Background(), data, flags))
	assert.Contains(t, data.Logic, "AI generation failed")
}

func TestHandleAIGeneration_NoModuleFails(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/work", Name: "widget", Prompt: "x"})

	_, err := g.handleAIGeneration(context.Background(), &templates.CommandData{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get import path")
}

func TestHandleAIGeneration_CarriesTheResponseIntoTheData(t *testing.T) {
	t.Parallel()

	client := new(MockChatClient)
	client.On("Ask", mock.Anything, "make a widget", mock.Anything).Run(func(args mock.Arguments) {
		resp, ok := args.Get(2).(*verifier.AIResponse)
		require.True(t, ok)

		resp.GoCode = "package widget\n"
		resp.TestCode = "package widget_test\n"
		resp.Recommendations = []string{"add a flag"}
	}).Return(nil)

	provider := registerTestProvider("gtb-generator-test-ask-ok", client)

	g, _ := newCommandProject(t, "", &Config{Prompt: "make a widget", AIProvider: string(provider), MaxSteps: 3})
	data := &templates.CommandData{Name: "widget", Package: "widget", PascalName: "Widget"}

	got, err := g.handleAIGeneration(context.Background(), data, nil)
	require.NoError(t, err)
	assert.Same(t, client, got)
	assert.Equal(t, "package widget\n", data.FullFileContent)
	assert.Equal(t, "package widget_test\n", data.TestCode)
	assert.Equal(t, []string{"add a flag"}, data.Recommendations)
}

func TestStartAIGeneration_EmptyCodeIsStillAResponse(t *testing.T) {
	t.Parallel()

	client := new(MockChatClient)
	client.On("Ask", mock.Anything, mock.Anything, mock.Anything).Return(nil)

	provider := registerTestProvider("gtb-generator-test-ask-empty", client)

	g, _ := newCommandProject(t, "", &Config{Prompt: "x", AIProvider: string(provider)})

	got, resp, err := g.startAIGeneration(context.Background(), "example.com/tool/pkg/cmd/widget", "widget", "RunWidget", "WidgetOptions", nil)
	require.NoError(t, err)
	assert.Same(t, client, got)
	assert.Empty(t, resp.GoCode)
}

func TestStartAIGeneration_AskFailureReturnsTheClient(t *testing.T) {
	t.Parallel()

	client := new(MockChatClient)
	client.On("Ask", mock.Anything, mock.Anything, mock.Anything).Return(assert.AnError)

	provider := registerTestProvider("gtb-generator-test-ask-fail", client)

	g, _ := newCommandProject(t, "", &Config{Prompt: "x", AIProvider: string(provider)})
	data := &templates.CommandData{Name: "widget", Package: "widget", PascalName: "Widget"}

	got := g.processAIGeneration(context.Background(), data, nil)
	assert.Same(t, client, got, "the client is handed back so the caller can still repair")
	assert.Contains(t, data.Logic, "AI generation failed")
}

func TestStartAIGeneration_MissingScriptFails(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{ScriptPath: filepath.Join(t.TempDir(), "absent.sh")})

	_, _, err := g.startAIGeneration(context.Background(), "m/p", "p", "RunP", "POptions", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read script")
}

func TestStartAIGeneration_ClaudeLocalSwitchSelectsTheLocalProvider(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "ai:\n  claude:\n    local: true\n", &Config{Prompt: "x"})

	_, _, err := g.startAIGeneration(context.Background(), "m/p", "p", "RunP", "POptions", nil)
	require.Error(t, err, "claude-local is not linked into the test binary")
}

func TestRequestTimeout_NoConfigIsZero(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{})

	assert.Zero(t, g.requestTimeout())
}

func TestCheckExistingMain_WarnsWithoutForce(t *testing.T) {
	t.Parallel()

	g, fs := newCommandProject(t, "", &Config{})
	cmdDir := "/work/pkg/cmd/widget"
	require.NoError(t, afero.WriteFile(fs, filepath.Join(cmdDir, "main.go"), []byte("package widget\n"), DefaultFileMode))

	require.NoError(t, g.checkExistingMain(cmdDir))
}

func TestResolveGenerationFlags_FallsBackToTheManifest(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{Name: "parent"})

	got := g.resolveGenerationFlags()
	require.Len(t, got, 1)
	assert.Equal(t, "verbose", got[0].Name)

	g.config.Flags = []string{"name:string"}
	got = g.resolveGenerationFlags()
	require.Len(t, got, 1)
	assert.Equal(t, "name", got[0].Name)
}

func TestGenerate_RefusesOutsideAProject(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/nowhere", Name: "widget"})

	require.ErrorIs(t, g.Generate(context.Background()), ErrNotGoToolBaseProject)

	_, err := g.GenerateDryRun(context.Background())
	require.ErrorIs(t, err, ErrNotGoToolBaseProject)

	g.config.DryRun = true
	require.ErrorIs(t, g.Generate(context.Background()), ErrNotGoToolBaseProject)
}

func TestGenerate_RefusesARootCommand(t *testing.T) {
	t.Parallel()

	g, _ := newCommandProject(t, "", &Config{Name: "root"})

	err := g.Generate(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot create a command named 'root'")

	_, err = g.GenerateDryRun(context.Background())
	require.Error(t, err)
}

func TestGenerate_SkipsAProtectedCommand(t *testing.T) {
	t.Parallel()

	g, fs := newCommandProject(t, "", &Config{Name: "guarded"})
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/manifest.yaml",
		[]byte("properties:\n  name: tool\ncommands:\n  - name: guarded\n    protected: true\n"), DefaultFileMode))

	require.NoError(t, g.Generate(context.Background()))

	exists, err := afero.Exists(fs, "/work/pkg/cmd/guarded/cmd.go")
	require.NoError(t, err)
	assert.False(t, exists)

	res, err := g.GenerateDryRun(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, res)
}

func TestReshapeParentIfTransitioned_NoOps(t *testing.T) {
	t.Parallel()

	g, fs := newCommandProject(t, "", &Config{Parent: "parent", DryRun: true})
	require.NoError(t, g.reshapeParentIfTransitioned(context.Background()))

	g.config.DryRun = false
	require.NoError(t, fs.Remove("/work/.gtb/manifest.yaml"))
	require.NoError(t, g.reshapeParentIfTransitioned(context.Background()), "no manifest, nothing to reshape")
}

func TestGenerate_DryRunLeavesTheProjectUntouched(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	setupDryRunProject(t, fs, "/project")

	g := New(&props.Props{FS: fs, Logger: logger.NewNoop()}, &Config{
		Name:   "greet",
		Path:   "/project",
		Parent: "root",
		DryRun: true,
	})

	require.NoError(t, g.Generate(context.Background()))

	exists, err := afero.Exists(fs, "/project/pkg/cmd/greet/cmd.go")
	require.NoError(t, err)
	assert.False(t, exists)
}
