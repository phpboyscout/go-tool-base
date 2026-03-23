package update

import (
	"testing"

	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/logger"
	p "github.com/phpboyscout/gtb/pkg/props"

	"github.com/stretchr/testify/assert"
)

func TestNewCmdUpdate(t *testing.T) {
	props := &p.Props{
		Tool: p.Tool{
			Name: "test-tool",
		},
		Logger:       logger.NewNoop(),
		ErrorHandler: errorhandling.New(logger.NewNoop(), nil),
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
