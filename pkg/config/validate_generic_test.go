package config

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type greetTestConfig struct {
	Greeting string `config:"hello.greeting" validate:"required"`
	Style    string `config:"hello.style" enum:"plain,loud"`
}

func loadTestConfig(t *testing.T, yaml string) Containable {
	t.Helper()

	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/config.yaml", []byte(yaml), 0o644))

	cfg, err := Load([]string{"/config.yaml"}, fs, false)
	require.NoError(t, err)

	return cfg
}

func TestValidateStruct_Valid(t *testing.T) {
	cfg := loadTestConfig(t, "hello:\n  greeting: Hello\n  style: loud\n")

	assert.NoError(t, ValidateStruct[greetTestConfig](cfg))
}

func TestValidateStruct_BadEnum(t *testing.T) {
	cfg := loadTestConfig(t, "hello:\n  greeting: Hello\n  style: shout\n")

	err := ValidateStruct[greetTestConfig](cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hello.style")
	assert.Contains(t, err.Error(), "not allowed")
}

func TestValidateStruct_MissingRequired(t *testing.T) {
	cfg := loadTestConfig(t, "hello:\n  style: plain\n")

	err := ValidateStruct[greetTestConfig](cfg)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "hello.greeting")
	assert.Contains(t, err.Error(), "required")
}

func TestValidateStruct_StrictMode(t *testing.T) {
	cfg := loadTestConfig(t, "hello:\n  greeting: Hello\n  style: plain\n  bogus: x\n")

	// Default: unknown keys are warnings, so the config is still valid.
	require.NoError(t, ValidateStruct[greetTestConfig](cfg))

	// Strict mode promotes unknown keys to errors.
	require.Error(t, ValidateStruct[greetTestConfig](cfg, WithStrictMode()))
}

func TestSchemaOf_CachesOptionFree(t *testing.T) {
	s1, err := SchemaOf[greetTestConfig]()
	require.NoError(t, err)

	s2, err := SchemaOf[greetTestConfig]()
	require.NoError(t, err)

	assert.Same(t, s1, s2, "option-free SchemaOf should return the cached schema")
}
