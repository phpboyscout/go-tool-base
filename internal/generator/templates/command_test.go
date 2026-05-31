package templates

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCleanImports_Initializer(t *testing.T) {
	imps := getCleanImports(nil, true)

	// The generated Init<Name> stub takes a config.Containable parameter.
	assert.Contains(t, imps, "gitlab.com/phpboyscout/go-tool-base/pkg/config",
		"initializer main.go needs the config package")
	// It does not use viper; importing it leaves an unused import.
	assert.NotContains(t, imps, "github.com/spf13/viper",
		"initializer main.go must not import viper")
}

func TestGetCleanImports_NoInitializer(t *testing.T) {
	imps := getCleanImports(nil, false)

	// Without an initializer, main.go consumes none of config/viper.
	assert.NotContains(t, imps, "gitlab.com/phpboyscout/go-tool-base/pkg/config",
		"command without an initializer should not import config in main.go")
	assert.NotContains(t, imps, "github.com/spf13/viper")

	// Base imports are always present.
	assert.Contains(t, imps, "context")
	assert.Contains(t, imps, "gitlab.com/phpboyscout/go-tool-base/pkg/props")
	assert.Contains(t, imps, "gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling")
}

func TestCommandRegistration_ReturnsSetupCommand(t *testing.T) {
	data := CommandData{
		Package:    "hello",
		PascalName: "Hello",
		Name:       "hello",
		Short:      "say hi",
		Long:       "say hi",
		OmitRun:    true,
	}

	var buf bytes.Buffer
	require.NoError(t, CommandRegistration(data).Render(&buf))
	src := buf.String()

	// New shape: returns *setup.Command and assigns cmd directly to the
	// wrapped value, so later attachment (parent.Register / inline subcommand
	// wiring) goes through *setup.Command. The literal "hello" is implicitly
	// converted to props.FeatureCmd (a named string type) by Go.
	assert.Contains(t, src, "*setup.Command", "NewCmd<Name> must return *setup.Command")
	assert.Contains(t, src, "cmd := setup.Wrap(\"hello\", &cobra.Command{",
		"cmd must be *setup.Command from the start, not wrapped only at return")

	// The legacy shape — bare *cobra.Command return, or a stray
	// AddCommandWithMiddleware call — must not appear.
	assert.NotContains(t, src, "AddCommandWithMiddleware",
		"generator must no longer emit AddCommandWithMiddleware")
}
