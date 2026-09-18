package setup_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestProtocolStdoutMarker: a command whose stdout carries a wire protocol is
// stamped once and recognised across its subtree by a parent walk, never by
// its name, so the root can withhold interactive UI from it without knowing
// which feature it belongs to (spec 0202 D5).
func TestProtocolStdoutMarker(t *testing.T) {
	t.Parallel()

	server := &cobra.Command{Use: "mcp"}
	assert.Same(t, server, setup.MarkProtocolStdout(server), "returns the command for chaining")

	start := &cobra.Command{Use: "start"}
	server.AddCommand(start)

	unmarked := &cobra.Command{Use: "mcp"}
	other := &cobra.Command{Use: "status"}

	assert.Equal(t, "true", server.Annotations[setup.ProtocolStdoutAnnotation])
	assert.True(t, setup.IsProtocolStdout(server), "the stamped command")
	assert.True(t, setup.IsProtocolStdout(start), "a descendant, by the parent walk")
	assert.False(t, setup.IsProtocolStdout(unmarked), "the same name without the stamp")
	assert.False(t, setup.IsProtocolStdout(other), "an unrelated command")
	assert.False(t, setup.IsProtocolStdout(nil), "nil is not a protocol command")
	assert.Nil(t, setup.MarkProtocolStdout(nil), "nil passes through")
}
