package setup_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

func TestDeclaredEnvVariables(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()
	setup.DeclareEnvVariableOn(r, "MYTOOL_OWN_KNOB")
	setup.DeclareEnvVariableOn(r, "MYTOOL_OWN_KNOB")

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	assert.Equal(t, []string{setup.AccessibleEnvVariable, "MYTOOL_OWN_KNOB"}, setup.DeclaredEnvVariables(set),
		"the framework's own name-read variable is always declared; a tool's are declared once each")
}
