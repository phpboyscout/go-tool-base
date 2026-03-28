package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestInitialise_Basic(t *testing.T) {
	fs := afero.NewMemMapFs()
	homeDir := "/home/testuser"
	t.Setenv("HOME", homeDir)

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	targetDir := "/home/testuser/.testtool"

	configPath, err := Initialise(p, InitOptions{
		Dir: targetDir,
	})
	require.NoError(t, err)

	assert.Contains(t, configPath, "config.yaml")
	exists, _ := afero.Exists(fs, configPath)
	assert.True(t, exists)
}
