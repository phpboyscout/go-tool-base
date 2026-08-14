package chat

import (
	"context"
	"testing"

	gochat "gitlab.com/phpboyscout/go/chat"
)

// fakeClient is a scriptable ChatClient for adapter tests that need a registered
// provider without a real SDK call.
type fakeClient struct {
	chatReply string
}

func (f *fakeClient) Add(context.Context, string, ...gochat.Media) error      { return nil }
func (f *fakeClient) Ask(context.Context, string, any, ...gochat.Media) error { return nil }
func (f *fakeClient) SetTools([]gochat.Tool) error                            { return nil }
func (f *fakeClient) Chat(context.Context, string, ...gochat.Media) (string, error) {
	return f.chatReply, nil
}
func (f *fakeClient) Usage() gochat.Usage { return gochat.Usage{} }

// History satisfies gochat.ChatClient, which gained the method in chat v0.10.0.
// A fake with no conversation reports none, and Known is false — this stands in
// for a provider rather than owning a transcript.
func (f *fakeClient) History() gochat.History { return gochat.History{} }

// registerTestProviders registers fake "fbt-ok" / "fbt-ok2" providers used by the
// fallback-chain adapter tests. The names are test-only, so they do not collide
// with the real blank-imported providers; the module registry exposes no removal,
// and re-registering is idempotent, so no cleanup is needed.
func registerTestProviders(t *testing.T) {
	t.Helper()

	factory := func(context.Context, gochat.Settings) (gochat.ChatClient, error) {
		return &fakeClient{chatReply: "ok"}, nil
	}

	gochat.RegisterProvider(gochat.Provider("fbt-ok"), factory)
	gochat.RegisterProvider(gochat.Provider("fbt-ok2"), factory)
}
