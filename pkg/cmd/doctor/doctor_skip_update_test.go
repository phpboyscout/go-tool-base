package doctor

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestNewCmdDoctor_MarkedSkipUpdateCheck: a diagnostics run must not trigger
// the pre-run update check; the stamp covers the doctor subtree via
// SkipUpdateCheck's parent-chain walk.
func TestNewCmdDoctor_MarkedSkipUpdateCheck(t *testing.T) {
	t.Parallel()

	props := &p.Props{Logger: logger.NewNoop(), FS: afero.NewMemMapFs()}

	cmd := NewCmdDoctor(props)
	assert.True(t, setup.SkipsUpdateCheck(cmd.Command),
		"doctor must be stamped with MarkSkipUpdateCheck")
}
