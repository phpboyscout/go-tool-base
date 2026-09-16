package doctor

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gochat "gitlab.com/phpboyscout/go/chat"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// TestCheckChatProviders pins spec 0196 D8 and D12: doctor reports whether
// the provider the config names, and every fallback member, is one this
// binary registers, naming the module to import when not; it mirrors the
// Forge adapters check. The doctor test binary links no real provider, so
// the fakes below are what "registered" means here.
func TestCheckChatProviders(t *testing.T) {
	factory := func(context.Context, gochat.Settings) (gochat.ChatClient, error) { return nil, nil }
	gochat.RegisterProvider("dt-ok", factory)
	gochat.RegisterProvider("dt-ok2", factory)

	ai := p.Tool{Name: "t", Features: p.SetFeatures(p.Enable(p.AiCmd))}

	t.Run("skips without ai", func(t *testing.T) {
		res := checkChatProviders(context.Background(), &p.Props{Tool: p.Tool{Name: "t"}, Config: testutil.StoreFromYAML(t, "")})
		assert.Equal(t, CheckSkip, res.Status)
	})

	t.Run("passes when the configured provider is registered", func(t *testing.T) {
		res := checkChatProviders(context.Background(), &p.Props{Tool: ai, Config: testutil.StoreFromYAML(t, "ai:\n  provider: dt-ok\n")})
		assert.Equal(t, CheckPass, res.Status, res.Message)
		assert.Contains(t, res.Message, "dt-ok")
	})

	t.Run("fails naming the module when it is not", func(t *testing.T) {
		res := checkChatProviders(context.Background(), &p.Props{Tool: ai, Config: testutil.StoreFromYAML(t, "ai:\n  provider: bedrock\n")})
		assert.Equal(t, CheckFail, res.Status)
		assert.Contains(t, res.Message, "go/chat-bedrock")
	})

	t.Run("checks every fallback member", func(t *testing.T) {
		res := checkChatProviders(context.Background(), &p.Props{Tool: ai, Config: testutil.StoreFromYAML(t,
			"ai:\n  provider: dt-ok\n  fallback:\n    enabled: true\n    providers: [dt-ok, dt-ok2, claude]\n")})
		assert.Equal(t, CheckFail, res.Status)
		assert.Contains(t, res.Message, "go/chat-anthropic")
	})

	t.Run("no provider configured reports what is registered", func(t *testing.T) {
		res := checkChatProviders(context.Background(), &p.Props{Tool: ai, Config: testutil.StoreFromYAML(t, "")})
		assert.Equal(t, CheckWarn, res.Status, res.Message)
		assert.Contains(t, res.Message, "ai.provider")
	})
}

// TestDefaultChecks_ChatProvidersReplacesAPIKeys: the three-key count is gone
// (the credential resolution check reports every declared chat credential
// since #55) and the registry check is in.
func TestDefaultChecks_ChatProvidersReplacesAPIKeys(t *testing.T) {
	t.Parallel()

	var hasChat bool

	for _, c := range DefaultChecks(&p.Props{}) {
		ptr := reflect.ValueOf(c).Pointer()
		if ptr == reflect.ValueOf(checkChatProviders).Pointer() {
			hasChat = true
		}
	}

	require.True(t, hasChat, "Chat providers is a default check")
}

// TestCheckCredentialResolution_ReportsOnlyLinkedProviders: with ai enabled,
// a chat credential is reported only when one of its providers is registered
// in this binary (D12).
func TestCheckCredentialResolution_ReportsOnlyLinkedProviders(t *testing.T) {
	t.Setenv("LINKED_TEST_ANTHROPIC", "x")
	t.Setenv("LINKED_TEST_OPENAI", "x")

	cfg := "anthropic:\n  api:\n    env: LINKED_TEST_ANTHROPIC\nopenai:\n  api:\n    env: LINKED_TEST_OPENAI\n"
	ai := &p.Props{Tool: p.Tool{Name: "t", Features: p.SetFeatures(p.Enable(p.AiCmd))}, Config: testutil.StoreFromYAML(t, cfg)}

	// Nothing real is registered in this binary: neither credential belongs
	// to a linked provider.
	res := checkCredentialResolution(context.Background(), ai)
	assert.NotContains(t, res.Details, "Anthropic")
	assert.NotContains(t, res.Details, "OpenAI")

	gochat.RegisterProvider(gochat.ProviderOpenAICompatible, func(context.Context, gochat.Settings) (gochat.ChatClient, error) { return nil, nil })

	res = checkCredentialResolution(context.Background(), ai)
	assert.Contains(t, res.Details, "OpenAI", "openai-compatible shares OpenAI's root, so the key is reported")
	assert.NotContains(t, res.Details, "Anthropic")
}
