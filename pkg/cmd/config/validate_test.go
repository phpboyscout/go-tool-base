package config_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdValidate_ValidConfig(t *testing.T) {
	t.Parallel()

	p := newTestProps(t, "log:\n  level: info\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "valid")
}

func TestCmdValidate_InvalidConfig(t *testing.T) {
	t.Parallel()

	// The base schema requires log.level; a config without it is invalid.
	p := newTestProps(t, "feature:\n  enabled: true\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, buf.String(), "error:")
	assert.Contains(t, buf.String(), "log.level")
}

func TestCmdValidate_WarningDoesNotFail(t *testing.T) {
	t.Parallel()

	// An unknown key in a user-authored (file) layer yields a warning, not an
	// error. Provenance matters: the same key in an embedded-defaults reader
	// layer is filtered from the warnings.
	p := &props.Props{Config: testutil.FileStoreFromYAML(t, "log:\n  level: info\nunknown:\n  key: surplus\n")}
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "warning:")
}

func TestCmdValidate_NilConfig(t *testing.T) {
	t.Parallel()

	p := &props.Props{Config: nil}
	cmd := config.NewCmdValidate(p)

	err := cmd.Execute()
	assert.Error(t, err)
}
