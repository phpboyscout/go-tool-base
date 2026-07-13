package chat

import (
	"context"
	"testing"
)

// fakeClient is a scriptable ChatClient for adapter tests that need a registered
// provider without a real SDK call.
type fakeClient struct {
	chatReply string
}

func (f *fakeClient) Add(context.Context, string, ...Media) error      { return nil }
func (f *fakeClient) Ask(context.Context, string, any, ...Media) error { return nil }
func (f *fakeClient) SetTools([]Tool) error                            { return nil }
func (f *fakeClient) Chat(context.Context, string, ...Media) (string, error) {
	return f.chatReply, nil
}
func (f *fakeClient) Usage() Usage { return Usage{} }

// registerTestProviders registers fake "fbt-ok" / "fbt-ok2" providers used by the
// fallback-chain adapter tests. The names are test-only, so they do not collide
// with the real blank-imported providers; the module registry exposes no removal,
// and re-registering is idempotent, so no cleanup is needed.
func registerTestProviders(t *testing.T) {
	t.Helper()

	factory := func(context.Context, Settings) (ChatClient, error) {
		return &fakeClient{chatReply: "ok"}, nil
	}

	RegisterProvider(Provider("fbt-ok"), factory)
	RegisterProvider(Provider("fbt-ok2"), factory)
}
