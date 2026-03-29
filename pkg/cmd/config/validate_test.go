package config_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/config"
	pkgcfg "github.com/phpboyscout/go-tool-base/pkg/config"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCmdValidate_ValidConfig(t *testing.T) {
	t.Parallel()

	result := &pkgcfg.ValidationResult{}

	m := mockcfg.NewMockContainable(t)
	m.On("Validate", mock.Anything).Return(result)

	p := &props.Props{Config: m}
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "valid")
}

func TestCmdValidate_InvalidConfig(t *testing.T) {
	t.Parallel()

	result := &pkgcfg.ValidationResult{
		Errors: []pkgcfg.ValidationError{
			{Key: "log.level", Message: "required field is missing", Hint: "add log.level to config"},
		},
	}

	m := mockcfg.NewMockContainable(t)
	m.On("Validate", mock.Anything).Return(result)

	p := &props.Props{Config: m}
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

	result := &pkgcfg.ValidationResult{
		Warnings: []pkgcfg.ValidationError{
			{Key: "unknown.key", Message: "unknown configuration key"},
		},
	}

	m := mockcfg.NewMockContainable(t)
	m.On("Validate", mock.Anything).Return(result)

	p := &props.Props{Config: m}
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
