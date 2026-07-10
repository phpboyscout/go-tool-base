package docs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"testing/fstest"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// fakeChatClient is a minimal ChatClient used to drive runAsk's success paths
// without any network or AI call. It also satisfies StreamingChatClient and
// emits the answer as a single text delta when streamed.
type fakeChatClient struct {
	answer string
}

func (f *fakeChatClient) Add(context.Context, string, ...chat.Media) error      { return nil }
func (f *fakeChatClient) Ask(context.Context, string, any, ...chat.Media) error { return nil }
func (f *fakeChatClient) SetTools([]chat.Tool) error                            { return nil }
func (f *fakeChatClient) Chat(context.Context, string, ...chat.Media) (string, error) {
	return f.answer, nil
}
func (f *fakeChatClient) Usage() chat.Usage { return chat.Usage{} }

func (f *fakeChatClient) StreamChat(_ context.Context, _ string, cb chat.StreamCallback, _ ...chat.Media) (string, error) {
	if err := cb(chat.StreamEvent{Type: chat.EventTextDelta, Delta: f.answer}); err != nil {
		return "", err
	}

	return f.answer, nil
}

// plainChatClient is a ChatClient that does NOT implement StreamingChatClient,
// forcing AskAI down the non-streaming Chat path. This lets runAsk's no-style
// branch take the didStream==false sub-branch.
type plainChatClient struct{ answer string }

func (p *plainChatClient) Add(context.Context, string, ...chat.Media) error      { return nil }
func (p *plainChatClient) Ask(context.Context, string, any, ...chat.Media) error { return nil }
func (p *plainChatClient) SetTools([]chat.Tool) error                            { return nil }
func (p *plainChatClient) Chat(context.Context, string, ...chat.Media) (string, error) {
	return p.answer, nil
}
func (p *plainChatClient) Usage() chat.Usage { return chat.Usage{} }

// registerFakeProvider registers a uniquely-named fake provider for the
// duration of the test. The chat provider registry is a process-global, so
// this test must not run in parallel; the unique name avoids collisions with
// other suites' registrations.
func registerFakeProvider(t *testing.T, name string, client chat.ChatClient) chat.Provider {
	t.Helper()

	provider := chat.Provider(name)
	chat.RegisterProvider(provider, func(context.Context, chat.Settings) (chat.ChatClient, error) {
		return client, nil
	})

	return provider
}

// captureStdout redirects os.Stdout for the duration of fn so the test does not
// pollute the test runner output with rendered answers.
func captureStdout(t *testing.T, fn func()) {
	t.Helper()

	orig := os.Stdout

	r, w, err := os.Pipe()
	require.NoError(t, err)

	os.Stdout = w

	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, r)
		close(done)
	}()

	defer func() {
		os.Stdout = orig

		_ = w.Close()
		<-done
		_ = r.Close()
	}()

	fn()
}

// findSub returns the immediate subcommand of parent whose name matches name,
// or nil when no such subcommand is registered.
func findSub(parent *cobra.Command, name string) *cobra.Command {
	for _, c := range parent.Commands() {
		if c.Name() == name {
			return c
		}
	}

	return nil
}

// TestNewCmdDocs_StructureWithoutSite asserts the docs command tree wires the
// ask subcommand and the --provider persistent flag, and that serve is absent
// when the binary lacks the static-site assets.
func TestNewCmdDocs_StructureWithoutSite(t *testing.T) {
	t.Parallel()

	// Real Assets with docs but no site: serve must not be registered.
	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})
	p := &props.Props{Assets: assets, Tool: props.Tool{Name: "gtb"}}

	cmd := NewCmdDocs(p)

	assert.Equal(t, "docs", cmd.Name())

	flag := cmd.PersistentFlags().Lookup("provider")
	require.NotNil(t, flag, "docs must expose a --provider persistent flag")
	assert.Empty(t, flag.DefValue)

	require.NotNil(t, findSub(cmd.Command, "ask"), "ask subcommand must be registered")
	assert.Nil(t, findSub(cmd.Command, "serve"), "serve must be absent without site assets")
}

// TestNewCmdDocs_RegistersServeWithSite asserts the serve subcommand is added
// only when the static-site assets are present.
func TestNewCmdDocs_RegistersServeWithSite(t *testing.T) {
	t.Parallel()

	assets := props.NewAssets(props.AssetMap{
		"all": fstest.MapFS{
			"assets/docs/index.md":   {Data: []byte("# Docs")},
			"assets/site/index.html": {Data: []byte("<h1>site</h1>")},
		},
	})
	p := &props.Props{Assets: assets, Tool: props.Tool{Name: "gtb"}}

	cmd := NewCmdDocs(p)

	require.NotNil(t, findSub(cmd.Command, "serve"), "serve must be registered when site assets exist")
}

// TestNewCmdDocs_RunE_MissingDocsAssets exercises the error branch taken when
// the running binary lacks the embedded documentation assets. This is the
// non-TTY path of RunE — the happy path launches a Bubble Tea program and is
// terminal-bound, so it is intentionally not covered here.
func TestNewCmdDocs_RunE_MissingDocsAssets(t *testing.T) {
	t.Parallel()

	// Empty Assets: Exists("assets/docs") returns fs.ErrNotExist.
	p := &props.Props{Assets: props.NewAssets(), Tool: props.Tool{Name: "mytool"}}

	cmd := NewCmdDocs(p)
	cmd.SetContext(context.Background())

	err := cmd.RunE(cmd.Command, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "documentation assets")

	// The hint chain must mention the tool name via missingAssetsHint.
	assert.Contains(t, errors.FlattenHints(err), "mytool")
}

// TestNewCmdDocsAsk_Structure asserts the ask command's metadata, alias,
// argument arity, and the --no-style flag.
func TestNewCmdDocsAsk_Structure(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDocsAsk(&props.Props{})

	assert.Equal(t, "ask", cmd.Name())
	assert.Contains(t, cmd.Aliases, "?")

	flag := cmd.Flags().Lookup("no-style")
	require.NotNil(t, flag, "ask must expose --no-style")
	assert.Equal(t, "false", flag.DefValue)
	assert.NotNil(t, cmd.Flags().ShorthandLookup("n"), "no-style must have -n shorthand")

	// ExactArgs(1): zero and two args are rejected.
	require.Error(t, cmd.Args(cmd, nil))
	require.Error(t, cmd.Args(cmd, []string{"a", "b"}))
	require.NoError(t, cmd.Args(cmd, []string{"one"}))
}

// TestNewCmdDocsAsk_Run_FatalOnError drives the cobra Run handler through the
// failure path: an unsupported --provider makes chat.New (inside runAsk) fail
// fast with no network. A no-op exit function lets the test observe that the
// handler was invoked without terminating the process.
func TestNewCmdDocsAsk_Run_FatalOnError(t *testing.T) {
	t.Parallel()

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})

	handler := errorhandling.New(
		logger.NewNoop(),
		nil,
		errorhandling.WithExitFunc(func(int) {}),
	)

	p := &props.Props{
		Assets:       assets,
		Logger:       logger.NewNoop(),
		ErrorHandler: handler,
	}

	cmd := NewCmdDocsAsk(p)
	// Provide the --provider flag the Run handler reads via cmd.Flags().
	cmd.Flags().String("provider", "definitely-not-a-real-provider", "")
	cmd.SetArgs([]string{"what is gtb?"})
	cmd.SetContext(context.Background())

	// Execute routes through Args validation then Run. The no-op exit means
	// the Fatal call returns instead of exiting; the test passes if no panic
	// and no process exit occurs.
	require.NoError(t, cmd.Execute())
}

// TestRunAsk_UnsupportedProvider exercises runAsk's error path directly: an
// unsupported provider override makes chat.New return an error with no network
// or AI call, which runAsk wraps as "failed to ask AI".
func TestRunAsk_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})

	p := &props.Props{
		Assets: assets,
		Logger: logger.NewNoop(),
	}

	err := runAsk(context.Background(), p, "question?", false, "definitely-not-a-real-provider")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ask AI")
}

// TestRunAsk_NoStyleUnsupportedProvider covers the no-style branch selection in
// runAsk. The error still originates from chat.New so no AI call is made; the
// branch under test is the deltaFn assignment guarded by noStyle.
func TestRunAsk_NoStyleUnsupportedProvider(t *testing.T) {
	t.Parallel()

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})

	p := &props.Props{
		Assets: assets,
		Logger: logger.NewNoop(),
	}

	err := runAsk(context.Background(), p, "question?", true, "definitely-not-a-real-provider")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ask AI")
}

// TestRunAsk_StyledSuccess covers runAsk's styled (default) success path: a
// non-streaming fake client returns an answer which runAsk renders as markdown.
// Not parallel — registers a process-global fake provider.
func TestRunAsk_StyledSuccess(t *testing.T) {
	provider := registerFakeProvider(t, "docs-test-styled", &fakeChatClient{answer: "# Hello"})

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})
	p := &props.Props{Assets: assets, Logger: logger.NewNoop()}

	captureStdout(t, func() {
		err := runAsk(context.Background(), p, "hi?", false, string(provider))
		require.NoError(t, err)
	})
}

// TestRunAsk_NoStyleStreamedSuccess covers runAsk's no-style streamed success
// path: a streaming fake client emits the answer as a delta, so didStream is
// true and runAsk prints a trailing newline. Not parallel.
func TestRunAsk_NoStyleStreamedSuccess(t *testing.T) {
	provider := registerFakeProvider(t, "docs-test-streamed", &fakeChatClient{answer: "streamed answer"})

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})
	p := &props.Props{Assets: assets, Logger: logger.NewNoop()}

	captureStdout(t, func() {
		err := runAsk(context.Background(), p, "hi?", true, string(provider))
		require.NoError(t, err)
	})
}

// TestRunAsk_NoStyleNonStreamedSuccess covers runAsk's no-style path when the
// client does not stream: AskAI returns the full answer via Chat, didStream is
// false, and runAsk prints the answer followed by a newline. Not parallel.
func TestRunAsk_NoStyleNonStreamedSuccess(t *testing.T) {
	provider := registerFakeProvider(t, "docs-test-plain", &plainChatClient{answer: "plain answer"})

	assets := props.NewAssets(props.AssetMap{
		"docs": fstest.MapFS{"assets/docs/index.md": {Data: []byte("# Docs")}},
	})
	p := &props.Props{Assets: assets, Logger: logger.NewNoop()}

	captureStdout(t, func() {
		err := runAsk(context.Background(), p, "hi?", true, string(provider))
		require.NoError(t, err)
	})
}

// TestLogToProps_AllLevels asserts logToProps forwards each docs log level to a
// record emitted at the matching slog level. Fatal has no slog equivalent and
// is emitted at error level (logging must never exit the process here).
func TestLogToProps_AllLevels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		level     logger.Level
		wantLevel slog.Level
	}{
		{"debug", logger.DebugLevel, slog.LevelDebug},
		{"info", logger.InfoLevel, slog.LevelInfo},
		{"warn", logger.WarnLevel, slog.LevelWarn},
		{"error", logger.ErrorLevel, slog.LevelError},
		{"fatal", logger.FatalLevel, slog.LevelError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			capture := logger.NewCaptureHandler()
			p := &props.Props{Logger: slog.New(capture)}
			logToProps(p, "msg", tc.level)

			records := capture.Records()
			require.Len(t, records, 1)
			assert.Equal(t, "msg", records[0].Message)
			assert.Equal(t, tc.wantLevel, records[0].Level)
		})
	}
}

// TestLogToProps_UnknownLevelNoOp asserts an unrecognised level forwards
// nothing.
func TestLogToProps_UnknownLevelNoOp(t *testing.T) {
	t.Parallel()

	capture := logger.NewCaptureHandler()
	p := &props.Props{Logger: slog.New(capture)}

	logToProps(p, "msg", logger.Level(999))

	assert.Empty(t, capture.Records())
}
