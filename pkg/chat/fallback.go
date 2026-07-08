package chat

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cockroachdb/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// Compile-time interface assertions.
var (
	_ ChatClient          = (*fallbackClient)(nil)
	_ StreamingChatClient = (*streamingFallbackClient)(nil)
)

// fallbackConfig holds the options applied to a composite at construction.
type fallbackConfig struct {
	policy            FailoverPolicy
	strictToolContext bool
	onFailover        func(from, to Provider)
	log               logger.Logger
}

// FallbackOption configures a composite fallback client.
type FallbackOption func(*fallbackConfig)

// WithFailoverPolicy overrides the error-classification policy (default
// [DefaultFailoverPolicy]).
func WithFailoverPolicy(policy FailoverPolicy) FallbackOption {
	return func(c *fallbackConfig) { c.policy = policy }
}

// WithStrictToolContext makes failover fail fast once a tool call has executed
// in the current conversation, instead of advancing with a lossy text-only
// transcript replay (resolved OQ-2).
func WithStrictToolContext() FallbackOption {
	return func(c *fallbackConfig) { c.strictToolContext = true }
}

// WithOnFailover registers an observability hook invoked on each provider
// transition. It is called after the WARN log, before the new provider is used.
func WithOnFailover(fn func(from, to Provider)) FallbackOption {
	return func(c *fallbackConfig) { c.onFailover = fn }
}

// WithFallbackLogger sets the logger used for the single WARN line per failover
// transition. Defaults to a no-op logger; [NewFallbackFromConfigs] wires the
// tool's logger automatically.
func WithFallbackLogger(log logger.Logger) FallbackOption {
	return func(c *fallbackConfig) { c.log = log }
}

// fallbackClient is the composite ChatClient: it tries an ordered list of
// underlying clients, advancing to the next on a retryable failure. It is NOT
// safe for concurrent use (matching the underlying provider clients).
type fallbackClient struct {
	clients []ChatClient
	names   []Provider // parallel to clients; for logging / the onFailover hook
	policy  FailoverPolicy
	log     logger.Logger

	// transcript holds the user turns the composite has seen, replayed into a
	// fallback provider on first activation (assistant turns cannot be injected
	// via the ChatClient interface — the documented lossy behaviour). It grows
	// with conversation length, retaining each prompt's text.
	transcript []string
	tools      []Tool // instrumented (handlers flag tool invocation)
	active     int
	// readyUpTo: clients[0..readyUpTo] have had the transcript + tools applied.
	// Advancement is monotonic, so a single watermark suffices.
	readyUpTo int

	strictToolContext bool
	onFailover        func(from, to Provider)
	toolInvoked       bool
}

// NewFallback builds a composite ChatClient that tries clients in order,
// advancing to the next on a retryable failure. The first client is the
// primary; the rest are fallbacks. At least one client is required.
//
// The returned client also satisfies [StreamingChatClient] iff every supplied
// client does.
func NewFallback(clients []ChatClient, opts ...FallbackOption) (ChatClient, error) {
	base, err := newFallbackBase(clients, nil, opts...)
	if err != nil {
		return nil, err
	}

	return base.asInterface(), nil
}

// NewFallbackFromSettings constructs each provider via [New] and wraps the
// result in a composite. The first Settings value is the primary. A construction
// failure for a non-primary provider (e.g. a missing credential) is downgraded
// to a logged WARN and that provider is dropped, so one missing fallback
// credential does not break the whole client; if the primary fails to construct,
// the error is returned.
func NewFallbackFromSettings(ctx context.Context, settings []Settings, opts ...FallbackOption) (ChatClient, error) {
	if len(settings) == 0 {
		return nil, errors.New("fallback: at least one provider config is required")
	}

	cfg := fallbackOptions(opts...)

	clients, names, err := buildFallbackClients(ctx, cfg.log, settings)
	if err != nil {
		return nil, err
	}

	if len(clients) == 0 {
		return nil, errors.New("fallback: no provider could be constructed")
	}

	base, err := newFallbackBase(clients, names, opts...)
	if err != nil {
		return nil, err
	}

	return base.asInterface(), nil
}

// NewFallbackFromConfigs constructs each provider from Config values and wraps
// the result in a composite. Use [NewFallbackFromSettings] when each provider
// needs distinct construction dependencies.
func NewFallbackFromConfigs(ctx context.Context, cfgs []Config, opts ...FallbackOption) (ChatClient, error) {
	if len(cfgs) == 0 {
		return nil, errors.New("fallback: at least one provider config is required")
	}

	fallback := fallbackOptions(opts...)

	settings := make([]Settings, 0, len(cfgs))
	for _, cfg := range cfgs {
		settings = append(settings, Settings{Config: cfg, Logger: fallback.log})
	}

	return NewFallbackFromSettings(ctx, settings, opts...)
}

// buildFallbackClients constructs each provider via [New]. A non-primary
// construction failure is logged and dropped; a primary failure is fatal.
func buildFallbackClients(ctx context.Context, log logger.Logger, settings []Settings) ([]ChatClient, []Provider, error) {
	clients := make([]ChatClient, 0, len(settings))
	names := make([]Provider, 0, len(settings))

	for i, setting := range settings {
		client, err := New(ctx, setting)
		if err != nil {
			if i == 0 {
				return nil, nil, errors.Wrapf(err, "fallback: primary provider %q failed to construct", setting.Config.Provider)
			}

			logDroppedProvider(log, setting.Config)

			continue
		}

		clients = append(clients, client)
		names = append(names, setting.Config.Provider)
	}

	return clients, names, nil
}

// logDroppedProvider warns that a non-primary provider was dropped — host only,
// never the underlying error detail.
func logDroppedProvider(log logger.Logger, cfg Config) {
	log.Warn("chat fallback provider dropped",
		"provider", string(cfg.Provider),
		"endpoint_host", baseURLHost(cfg.BaseURL))
}

// NewWithFallbackSettings builds a single provider client or fallback composite
// from package-owned settings and fallback config. Per the resolved OQ-3,
// fallback.Providers[0] is the primary and overrides Settings.Config.Provider.
func NewWithFallbackSettings(ctx context.Context, settings Settings, fallback FallbackConfig, opts ...FallbackOption) (ChatClient, error) {
	if !fallback.Enabled || len(fallback.Providers) == 0 {
		return New(ctx, settings)
	}

	log := settings.logger()
	cfg := settings.Config
	warnFallbackPrimaryOverride(log, cfg, fallback.Providers[0])

	opts = append([]FallbackOption{WithFallbackLogger(log)}, opts...)

	perProviderSettings := make([]Settings, 0, len(fallback.Providers))
	for _, providerConfig := range fallbackProviderConfigs(cfg, fallback.Providers) {
		perProviderSettings = append(perProviderSettings, Settings{Config: providerConfig, Logger: log})
	}

	return NewFallbackFromSettings(ctx, perProviderSettings, opts...)
}

func warnFallbackPrimaryOverride(log logger.Logger, cfg Config, primary Provider) {
	if cfg.Provider == "" || cfg.Provider == primary {
		return
	}

	log.Warn("ai.fallback.providers[0] overrides ai.provider",
		"ai_provider", string(cfg.Provider),
		"fallback_primary", string(primary))
}

func fallbackProviderConfigs(base Config, providers []Provider) []Config {
	cfgs := make([]Config, 0, len(providers))
	for _, name := range providers {
		cfgs = append(cfgs, perProviderConfig(base, name))
	}

	return cfgs
}

// perProviderConfig clones base for a specific provider, clearing the
// provider-specific fields so each provider self-resolves them: Token and BaseURL
// fall through to that provider's own credential resolution, and Model falls
// through to that provider's default (the global ai.model is NOT applied per
// provider in a fallback chain — see the known limitation in chat.md; pass
// explicit Configs to NewFallbackFromConfigs to pin per-provider models).
//
// NOTE: this clears a denylist of fields. A new provider-specific or
// credential-bearing field added to Config must be cleared here too, or it will
// silently carry across every provider in the chain.
func perProviderConfig(base Config, provider Provider) Config {
	pc := base
	pc.Provider = provider
	pc.Token = ""
	pc.Credentials = CredentialConfig{}
	pc.Model = ""
	pc.BaseURL = ""

	return pc
}

func fallbackOptions(opts ...FallbackOption) fallbackConfig {
	var cfg fallbackConfig
	for _, o := range opts {
		o(&cfg)
	}

	if cfg.log == nil {
		cfg.log = logger.NewNoop()
	}

	return cfg
}

func newFallbackBase(clients []ChatClient, names []Provider, opts ...FallbackOption) (*fallbackClient, error) {
	if len(clients) == 0 {
		return nil, errors.New("fallback: at least one client is required")
	}

	cfg := fallbackOptions(opts...)

	// Default after applying options, so WithFailoverPolicy(nil)/WithFallbackLogger(nil)
	// also resolve to the defaults (one source of truth per default).
	if cfg.policy == nil {
		cfg.policy = DefaultFailoverPolicy
	}

	if cfg.log == nil {
		cfg.log = logger.NewNoop()
	}

	return &fallbackClient{
		clients:           clients,
		names:             names,
		policy:            cfg.policy,
		log:               cfg.log,
		strictToolContext: cfg.strictToolContext,
		onFailover:        cfg.onFailover,
	}, nil
}

// asInterface returns the streaming wrapper when every client streams, else the
// base — so the composite advertises StreamingChatClient exactly when it can
// honour it.
func (f *fallbackClient) asInterface() ChatClient {
	if f.allStreaming() {
		f.log.Info("chat fallback initialised", "tier", "streaming", "providers", len(f.clients))

		return &streamingFallbackClient{f}
	}

	f.log.Info("chat fallback initialised", "tier", "non-streaming", "providers", len(f.clients))

	return f
}

func (f *fallbackClient) allStreaming() bool {
	for _, c := range f.clients {
		if _, ok := c.(StreamingChatClient); !ok {
			return false
		}
	}

	return true
}

// name returns a label for the client at idx — its configured provider name
// when known, else a positional fallback.
func (f *fallbackClient) name(idx int) Provider {
	if idx < len(f.names) && f.names[idx] != "" {
		return f.names[idx]
	}

	return Provider(fmt.Sprintf("provider-%d", idx))
}

// Add forwards a user turn to the active client and records it in the transcript.
func (f *fallbackClient) Add(ctx context.Context, prompt string, media ...Media) error {
	if err := f.attempt(ctx, func(c ChatClient) error { return c.Add(ctx, prompt, media...) }); err != nil {
		return err
	}

	f.transcript = append(f.transcript, prompt)

	return nil
}

// Ask forwards a structured question to the active client and records the user
// turn.
func (f *fallbackClient) Ask(ctx context.Context, question string, target any, media ...Media) error {
	if err := f.attempt(ctx, func(c ChatClient) error { return c.Ask(ctx, question, target, media...) }); err != nil {
		return err
	}

	f.transcript = append(f.transcript, question)

	return nil
}

// Chat forwards a prompt to the active client and records the user prompt in the
// transcript (only user turns are replayed on failover).
func (f *fallbackClient) Chat(ctx context.Context, prompt string, media ...Media) (string, error) {
	var reply string

	err := f.attempt(ctx, func(c ChatClient) error {
		r, e := c.Chat(ctx, prompt, media...)
		if e != nil {
			return e
		}

		reply = r

		return nil
	})
	if err != nil {
		return "", err
	}

	f.transcript = append(f.transcript, prompt)

	return reply, nil
}

// SetTools instruments the tools (so the composite can detect a tool
// invocation) and applies them to the active client. They are re-applied to any
// client the composite later fails over to.
func (f *fallbackClient) SetTools(tools []Tool) error {
	wrapped := f.instrumentTools(tools)
	if err := f.clients[f.active].SetTools(wrapped); err != nil {
		return err
	}

	f.tools = wrapped

	return nil
}

// Usage returns the summed token usage across every underlying client the
// composite has driven (undriven clients report a zero usage), so a caller sees
// real combined spend across a failover (resolved OQ-4).
func (f *fallbackClient) Usage() Usage {
	var total Usage
	for _, c := range f.clients {
		total = total.Add(c.Usage())
	}

	return total
}

// attempt runs fn against the active client, advancing through the remaining
// clients on a retryable error until one succeeds or all are exhausted.
func (f *fallbackClient) attempt(ctx context.Context, fn func(ChatClient) error) error {
	var errs []error

	for {
		err := fn(f.clients[f.active])
		if err == nil {
			return nil
		}

		errs = append(errs, err)

		if ferr := f.advanceOrFail(ctx, err, errs); ferr != nil {
			return ferr
		}
	}
}

// advanceOrFail is the shared failover tail for the buffered and streaming
// loops: it decides whether to fail over after a failed attempt and, when it
// does, performs the transition (logging + transcript/tool replay). It returns
// nil to retry on the new active client, or a terminal error to return.
func (f *fallbackClient) advanceOrFail(ctx context.Context, err error, errs []error) error {
	next, ferr := f.shouldAdvance(ctx, err, errs)
	if ferr != nil {
		return ferr
	}

	if ferr := f.failoverTo(ctx, next, err); ferr != nil {
		return errors.Join(append(errs, ferr)...)
	}

	return nil
}

// shouldAdvance decides, after a failed attempt, whether to advance — returning
// the next client index — or to stop, returning a non-nil terminal error.
func (f *fallbackClient) shouldAdvance(ctx context.Context, err error, errs []error) (int, error) {
	// Caller cancellation is always terminal, regardless of policy.
	if ctx.Err() != nil {
		return 0, errors.Join(errs...)
	}

	if f.policy.Classify(err) == FailoverFatal {
		return 0, err
	}

	if f.strictToolContext && f.toolInvoked {
		return 0, errors.WithHint(
			errors.Wrap(err, "fallback refused: a tool call has executed in this conversation"),
			"remove WithStrictToolContext to allow lossy text-only failover after tool use",
		)
	}

	next := f.active + 1
	if next >= len(f.clients) {
		return 0, errors.Join(errs...) // every provider exhausted
	}

	return next, nil
}

// failoverTo logs the transition, advances the active index, fires the hook, and
// replays prior context (transcript + tools) into the newly-active client.
func (f *fallbackClient) failoverTo(ctx context.Context, idx int, cause error) error {
	from, to := f.name(f.active), f.name(idx)

	f.log.Warn("chat provider failover",
		"from", string(from),
		"to", string(to),
		"reason", failoverReason(cause))

	f.active = idx

	if f.onFailover != nil {
		f.onFailover(from, to)
	}

	return f.ensureReady(ctx, idx)
}

// ensureReady replays the transcript and re-applies tools into the client at idx
// the first time it becomes active. Advancement is monotonic, so the watermark
// makes subsequent activations no-ops.
func (f *fallbackClient) ensureReady(ctx context.Context, idx int) error {
	if idx <= f.readyUpTo {
		return nil
	}

	c := f.clients[idx]

	if len(f.tools) > 0 {
		if err := c.SetTools(f.tools); err != nil {
			return errors.Wrapf(err, "fallback: re-applying tools to %s", f.name(idx))
		}
	}

	for _, turn := range f.transcript {
		if err := c.Add(ctx, turn); err != nil {
			return errors.Wrapf(err, "fallback: replaying transcript into %s", f.name(idx))
		}
	}

	f.readyUpTo = idx

	return nil
}

// instrumentTools wraps each tool handler so the composite learns when a tool
// has actually executed (used by WithStrictToolContext).
func (f *fallbackClient) instrumentTools(tools []Tool) []Tool {
	out := make([]Tool, len(tools))

	for i, t := range tools {
		orig := t.Handler
		t.Handler = func(ctx context.Context, args json.RawMessage) (any, error) {
			f.toolInvoked = true

			if orig == nil {
				return nil, nil
			}

			return orig(ctx, args)
		}
		out[i] = t
	}

	return out
}

// failoverReason maps a triggering error to the coarse class logged as the WARN
// reason — never the raw message.
func failoverReason(err error) string {
	if _, ok := providerHTTPStatus(err); ok {
		return "status"
	}

	return "network"
}

// streamingFallbackClient is the composite returned when every underlying client
// is a StreamingChatClient. It adds StreamChat with the
// failover-only-before-the-first-visible-event rule.
type streamingFallbackClient struct {
	*fallbackClient
}

// StreamChat streams from the active client, failing over to the next client on
// a retryable error — but only before the first externally-visible event
// (EventTextDelta / EventToolCallStart) has reached the caller's callback. Once
// a delta has been emitted it cannot be un-emitted, so a later error is terminal.
func (s *streamingFallbackClient) StreamChat(ctx context.Context, prompt string, callback StreamCallback, media ...Media) (string, error) {
	var errs []error

	for {
		sc, ok := s.clients[s.active].(StreamingChatClient)
		if !ok {
			return "", errors.Newf("fallback: active provider %s does not support streaming", s.name(s.active))
		}

		emitted := false
		wrapped := func(ev StreamEvent) error {
			if ev.Type == EventTextDelta || ev.Type == EventToolCallStart {
				emitted = true
			}

			return callback(ev)
		}

		reply, err := sc.StreamChat(ctx, prompt, wrapped, media...)
		if err == nil {
			s.transcript = append(s.transcript, prompt)

			return reply, nil
		}

		errs = append(errs, err)

		// A visible event already reached the caller — we cannot replay onto
		// another provider without re-emitting, so the error is terminal.
		if emitted {
			return "", err
		}

		if ferr := s.advanceOrFail(ctx, err, errs); ferr != nil {
			return "", ferr
		}
	}
}
