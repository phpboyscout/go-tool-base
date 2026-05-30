package templates

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
