package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	gochat "gitlab.com/phpboyscout/go/chat"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/chat"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

// TestEveryForgeIsLinked is the registration guard for the binary that ships
// every adapter: a profile whose provider no linked adapter registers builds
// and passes every other test, then fails the first time a user runs the
// wizard. It lived in pkg/setup/forge while the framework linked the adapters
// (spec 0194 D3 moved them here).
func TestEveryForgeIsLinked(t *testing.T) {
	t.Parallel()

	var states []features.State
	for _, d := range forge.Displays() {
		states = append(states, features.State{ID: d.ID, Enabled: true})
	}

	set, err := features.Resolve(features.Default().Snapshot(), states)
	require.NoError(t, err)

	assert.Empty(t, forge.Unlinked(set), "gtb enables every forge; providers.go must link every adapter")
}

// TestTheToolBridgeIsLinked: gtb hands tools to the configured provider (AI
// docs generation, the verifier), and claude-local and codex-local refuse
// them unless the binary links the tool bridge (chat-anthropic and
// chat-openai v0.17.0).
func TestTheToolBridgeIsLinked(t *testing.T) {
	t.Parallel()

	_, ok := gochat.RegisteredToolServer()
	assert.True(t, ok, "providers.go must blank-import %s", chat.ToolServerModule)
}
