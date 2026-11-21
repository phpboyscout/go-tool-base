package generator

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCommandPackageName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"deploy":     "deploy",
		"my-cmd":     "my_cmd",
		"import":     "importcmd",
		"type":       "typecmd",
		"go":         "gocmd",
		"importcmd":  "importcmd",
		"my-import":  "my_import",
		"snake_case": "snake_case",
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, want, commandPackageName(name))
		})
	}
}
