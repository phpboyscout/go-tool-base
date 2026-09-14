package detach

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestNewCmdDetach_Structure(t *testing.T) {
	t.Parallel()

	cmd := NewCmdDetach(&props.Props{}).Command
	assert.Equal(t, "detach", cmd.Use)

	got := map[string]bool{}
	for _, c := range NewCmdDetach(&props.Props{}).Commands() {
		got[c.Name()] = true
	}

	assert.True(t, got["command"], "expected 'detach command' subcommand")
}

func TestNewCmdDetachCommand_Flags(t *testing.T) {
	t.Parallel()

	cmd := newCmdDetachCommand(&props.Props{}).Command
	assert.Equal(t, "command <module>", cmd.Use)
	assert.NotNil(t, cmd.Flags().Lookup("path"), "must have a --path flag")
}
