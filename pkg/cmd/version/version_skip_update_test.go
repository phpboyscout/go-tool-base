package version

import (
	"testing"

	"github.com/stretchr/testify/assert"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestNewCmdVersion_MarkedSkipUpdateCheck: version performs its own
// latest-version lookup, so it must carry the annotation exempting it from the
// pre-run update check (rename-safe replacement for the old Use-string list).
func TestNewCmdVersion_MarkedSkipUpdateCheck(t *testing.T) {
	t.Parallel()

	cmd := NewCmdVersion(&p.Props{})
	assert.True(t, setup.SkipsUpdateCheck(cmd.Command),
		"version must be stamped with MarkSkipUpdateCheck")
}
