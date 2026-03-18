package update

import (
	"io"
	"testing"

	"github.com/phpboyscout/gtb/pkg/errorhandling"
	p "github.com/phpboyscout/gtb/pkg/props"

	"github.com/charmbracelet/log"
	"github.com/stretchr/testify/assert"
)

func TestNewCmdUpdate(t *testing.T) {
	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger:       log.New(io.Discard),
		ErrorHandler: errorhandling.New(log.New(io.Discard), nil),
	}

	cmd := NewCmdUpdate(props)
	assert.NotNil(t, cmd)
	assert.Equal(t, "update", cmd.Use)

	// Check flags
	force, err := cmd.Flags().GetBool("force")
	assert.NoError(t, err)
	assert.False(t, force)

	version, err := cmd.Flags().GetString("version")
	assert.NoError(t, err)
	assert.Equal(t, "", version)
}
