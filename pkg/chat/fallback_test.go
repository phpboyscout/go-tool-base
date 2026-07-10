package chat

import (
	"context"
	"encoding/json"
	"net"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// fakeClient is a scriptable ChatClient for composite tests.
type fakeClient struct {
	chatErr   error
	chatReply string
	usage     Usage

	added     []string
	toolsSet  []Tool
	chatCalls int
}

func (f *fakeClient) Add(_ context.Context, prompt string, _ ...Media) error {
	f.added = append(f.added, prompt)

	return nil
}

func (f *fakeClient) Ask(_ context.Context, _ string, _ any, _ ...Media) error {
	if f.chatErr != nil {
		return f.chatErr
	}

	return nil
}

func (f *fakeClient) SetTools(tools []Tool) error {
	f.toolsSet = tools

	return nil
}

func (f *fakeClient) Chat(_ context.Context, _ string, _ ...Media) (string, error) {
	f.chatCalls++

	if f.chatErr != nil {
		return "", f.chatErr
	}

	return f.chatReply, nil
}

func (f *fakeClient) Usage() Usage { return f.usage }

// fakeStreamingClient adds StreamChat.
type fakeStreamingClient struct {
	*fakeClient
	streamErr     error
	streamReply   string
	emitBeforeErr bool
}

func (f *fakeStreamingClient) StreamChat(_ context.Context, _ string, cb StreamCallback, _ ...Media) (string, error) {
	if f.emitBeforeErr {
		if err := cb(StreamEvent{Type: EventTextDelta, Delta: "partial"}); err != nil {
			return "", err
		}
	}

	if f.streamErr != nil {
		return "", f.streamErr
	}

	if err := cb(StreamEvent{Type: EventTextDelta, Delta: f.streamReply}); err != nil {
		return "", err
	}

	return f.streamReply, nil
}

// rateLimited returns a retryable 429 whose Error() is safe to format (the
// anthropic/openai SDK error types panic on a nil Request/Response, which a
// sparse test fixture would have; genai.APIError formats cleanly).
func rateLimited() error { return &genai.APIError{Code: 429, Message: "rate limit"} }

func TestNewFallback_RequiresClient(t *testing.T) {
	t.Parallel()

	_, err := NewFallback(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one client")
}

func TestNewFallback_NilOptionsFallBackToDefaults(t *testing.T) {
	t.Parallel()

	fb, err := NewFallback([]ChatClient{&fakeClient{chatReply: "ok"}},
		WithFailoverPolicy(nil), WithFallbackLogger(nil))
	require.NoError(t, err)

	got, err := fb.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestFallback_PrimarySucceeds(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatReply: "from-primary"}
	second := &fakeClient{chatReply: "from-second"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	got, err := fb.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "from-primary", got)
	assert.Zero(t, second.chatCalls, "fallback must not be called when primary succeeds")
}

func TestFallback_AdvancesOn429(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatReply: "from-second"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	got, err := fb.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "from-second", got)
	assert.Equal(t, 1, second.chatCalls)
}

func TestFallback_AdvancesOnNetworkError(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: &net.OpError{Op: "dial", Err: errors.New("refused")}}
	second := &fakeClient{chatReply: "ok"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	got, err := fb.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestFallback_FatalAuthErrorDoesNotAdvance(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: &anthropic.Error{StatusCode: 401}}
	second := &fakeClient{chatReply: "unused"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	_, err = fb.Chat(context.Background(), "hi")
	require.Error(t, err)
	assert.Zero(t, second.chatCalls, "a fatal auth error must not trigger failover")
}

func TestFallback_AllExhausted(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatErr: rateLimited()}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	_, err = fb.Chat(context.Background(), "hi")
	require.Error(t, err)
	assert.Equal(t, 1, second.chatCalls, "every provider is tried once")
}

func TestFallback_TranscriptReplayedOnFailover(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatReply: "answer"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	require.NoError(t, fb.Add(context.Background(), "earlier user turn"))

	got, err := fb.Chat(context.Background(), "current question")
	require.NoError(t, err)
	assert.Equal(t, "answer", got)

	// The fallback received the earlier user turn replayed before its Chat.
	assert.Contains(t, second.added, "earlier user turn")
}

func TestFallback_ToolsReappliedOnFailover(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatReply: "ok"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	tool := Tool{Name: "echo", Handler: func(context.Context, json.RawMessage) (any, error) { return nil, nil }}
	require.NoError(t, fb.SetTools([]Tool{tool}))

	_, err = fb.Chat(context.Background(), "hi")
	require.NoError(t, err)

	require.Len(t, second.toolsSet, 1)
	assert.Equal(t, "echo", second.toolsSet[0].Name)
}

func TestFallback_UsageAggregated(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited(), usage: Usage{InputTokens: 10, TotalTokens: 10, Known: true}}
	second := &fakeClient{chatReply: "ok", usage: Usage{InputTokens: 5, OutputTokens: 7, TotalTokens: 12, Known: true}}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	_, err = fb.Chat(context.Background(), "hi")
	require.NoError(t, err)

	u := fb.Usage()
	assert.Equal(t, 15, u.InputTokens, "input tokens summed across primary and fallback")
	assert.Equal(t, 22, u.TotalTokens)
}

func TestFallback_CallerCancelledIsFatal(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatReply: "unused"}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = fb.Chat(ctx, "hi")
	require.Error(t, err)
	assert.Zero(t, second.chatCalls, "a cancelled caller context must not trigger failover")
}

func TestFallback_StrictToolContextFailsFastAfterToolUse(t *testing.T) {
	t.Parallel()

	// Primary's tool handler runs (flagging tool use), then the primary errors.
	primary := &fakeClient{chatReply: ""}
	second := &fakeClient{chatReply: "second"}

	fb, err := NewFallback([]ChatClient{primary, second}, WithStrictToolContext())
	require.NoError(t, err)

	tool := Tool{Name: "t", Handler: func(context.Context, json.RawMessage) (any, error) { return nil, nil }}
	require.NoError(t, fb.SetTools([]Tool{tool}))

	// Simulate the primary having executed the tool, then failing.
	_, _ = primary.toolsSet[0].Handler(context.Background(), nil) // flips toolInvoked
	primary.chatErr = rateLimited()

	_, err = fb.Chat(context.Background(), "hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool call has executed")
	assert.Zero(t, second.chatCalls, "strict tool context must fail fast, not fail over")
}

func TestFallback_AskAdvancesOn429(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{} // Ask succeeds (chatErr nil)

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	var target struct{ X int }
	require.NoError(t, fb.Ask(context.Background(), "q", &target))
}

func TestFallback_OptionsApplied(t *testing.T) {
	t.Parallel()

	var transitions [][2]Provider

	buf := logger.NewBuffer()
	primary := &fakeClient{chatErr: rateLimited()}
	second := &fakeClient{chatReply: "ok"}

	fb, err := NewFallback([]ChatClient{primary, second},
		WithFailoverPolicy(DefaultFailoverPolicy),
		WithFallbackLogger(logger.ToSlog(buf)),
		WithOnFailover(func(from, to Provider) { transitions = append(transitions, [2]Provider{from, to}) }),
	)
	require.NoError(t, err)

	_, err = fb.Chat(context.Background(), "hi")
	require.NoError(t, err)

	require.Len(t, transitions, 1, "onFailover hook fires once per transition")
	assert.True(t, buf.Contains("chat provider failover"), "a WARN line is logged per transition")
}

func TestFallback_CustomPolicyCanForceFatal(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()} // would normally advance
	second := &fakeClient{chatReply: "unused"}

	fb, err := NewFallback([]ChatClient{primary, second},
		WithFailoverPolicy(policyFunc(func(error) FailoverDecision { return FailoverFatal })),
	)
	require.NoError(t, err)

	_, err = fb.Chat(context.Background(), "hi")
	require.Error(t, err)
	assert.Zero(t, second.chatCalls, "a custom always-fatal policy suppresses failover")
}

// policyFunc adapts a function to the FailoverPolicy interface.
type policyFunc func(error) FailoverDecision

func (p policyFunc) Classify(err error) FailoverDecision { return p(err) }

func TestFallback_ReplayErrorIsSurfaced(t *testing.T) {
	t.Parallel()

	primary := &fakeClient{chatErr: rateLimited()}
	second := &addFailsClient{}

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	require.NoError(t, fb.Add(context.Background(), "prior turn"))

	_, err = fb.Chat(context.Background(), "hi")
	require.Error(t, err, "a transcript-replay failure into the fallback is surfaced")
	assert.Contains(t, err.Error(), "replaying transcript")
}

// addFailsClient errors on Add (used to exercise the replay-failure path).
type addFailsClient struct{ fakeClient }

func (a *addFailsClient) Add(context.Context, string, ...Media) error {
	return errors.New("add boom")
}

func TestFallback_NameUsesConfiguredProvider(t *testing.T) {
	t.Parallel()

	base, err := newFallbackBase(
		[]ChatClient{&fakeClient{}, &fakeClient{}},
		[]Provider{ProviderClaude, ProviderOpenAI},
	)
	require.NoError(t, err)

	assert.Equal(t, ProviderClaude, base.name(0))
	assert.Equal(t, ProviderOpenAI, base.name(1))
	assert.Equal(t, Provider("provider-2"), base.name(2), "out-of-range falls back to a positional label")
}

// --- config-driven construction (NewFallbackFromConfigs / NewWithFallback) ---

// these tests register global providers, so they are NOT parallel.

func registerTestProviders(t *testing.T) {
	t.Helper()

	RegisterProvider(Provider("fbt-ok"), func(context.Context, Settings) (ChatClient, error) {
		return &fakeClient{chatReply: "ok"}, nil
	})
	RegisterProvider(Provider("fbt-ok2"), func(context.Context, Settings) (ChatClient, error) {
		return &fakeClient{chatReply: "ok2"}, nil
	})
	RegisterProvider(Provider("fbt-bad"), func(context.Context, Settings) (ChatClient, error) {
		return nil, errors.New("no credentials")
	})
}

func TestNewFallbackFromConfigs_DropsMissingFallbackCred(t *testing.T) {
	registerTestProviders(t)

	// Non-primary construction failure → dropped, composite still built.
	fb, err := NewFallbackFromConfigs(
		context.Background(),
		[]Config{{Provider: "fbt-ok"}, {Provider: "fbt-bad"}},
		WithFallbackLogger(logger.ToSlog(logger.NewNoop())),
	)
	require.NoError(t, err)
	require.NotNil(t, fb)

	got, err := fb.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

func TestNewFallbackFromConfigs_PrimaryFailureIsFatal(t *testing.T) {
	registerTestProviders(t)

	_, err := NewFallbackFromConfigs(
		context.Background(),
		[]Config{{Provider: "fbt-bad"}, {Provider: "fbt-ok"}},
		WithFallbackLogger(logger.ToSlog(logger.NewNoop())),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary")
}

func TestNewFallbackFromConfigs_Empty(t *testing.T) {
	t.Parallel()

	_, err := NewFallbackFromConfigs(context.Background(), nil)
	require.Error(t, err)
}

func TestNewWithFallback_DisabledIsSingleProvider(t *testing.T) {
	registerTestProviders(t)

	// No fallback config → behaves as New for the given provider.
	client, err := NewWithFallbackSettings(
		context.Background(),
		Settings{Config: Config{Provider: "fbt-ok"}, Logger: logger.ToSlog(logger.NewNoop())},
		FallbackConfig{},
	)
	require.NoError(t, err)

	got, err := client.Chat(context.Background(), "hi")
	require.NoError(t, err)
	assert.Equal(t, "ok", got)
}

// --- streaming ---

func streamingFake(reply string) *fakeStreamingClient {
	return &fakeStreamingClient{fakeClient: &fakeClient{}, streamReply: reply}
}

func TestFallback_NotStreamingWhenAnyClientIsnt(t *testing.T) {
	t.Parallel()

	fb, err := NewFallback([]ChatClient{streamingFake("a"), &fakeClient{}})
	require.NoError(t, err)

	_, ok := fb.(StreamingChatClient)
	assert.False(t, ok, "composite must not advertise streaming when a client cannot stream")
}

func TestFallback_IsStreamingWhenAllClientsAre(t *testing.T) {
	t.Parallel()

	fb, err := NewFallback([]ChatClient{streamingFake("a"), streamingFake("b")})
	require.NoError(t, err)

	_, ok := fb.(StreamingChatClient)
	assert.True(t, ok)
}

func TestFallback_StreamFailoverBeforeFirstDelta(t *testing.T) {
	t.Parallel()

	primary := streamingFake("")
	primary.streamErr = rateLimited()
	second := streamingFake("second-answer")

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	sc := fb.(StreamingChatClient)

	var deltas []string
	got, err := sc.StreamChat(context.Background(), "hi", func(ev StreamEvent) error {
		if ev.Type == EventTextDelta {
			deltas = append(deltas, ev.Delta)
		}

		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "second-answer", got)
	assert.Equal(t, []string{"second-answer"}, deltas, "only the fallback's deltas reach the caller")
}

func TestFallback_StreamNoFailoverAfterFirstDelta(t *testing.T) {
	t.Parallel()

	primary := streamingFake("")
	primary.emitBeforeErr = true // emits a delta, THEN errors
	primary.streamErr = rateLimited()
	second := streamingFake("unused")

	fb, err := NewFallback([]ChatClient{primary, second})
	require.NoError(t, err)

	sc := fb.(StreamingChatClient)

	_, err = sc.StreamChat(context.Background(), "hi", func(StreamEvent) error { return nil })
	require.Error(t, err, "an error after the first delta is terminal — no failover")
	assert.Zero(t, second.chatCalls)
}
