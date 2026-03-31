package config

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
)

func newTestContainer(t *testing.T, yaml string) *Container {
	t.Helper()

	l := logger.NewNoop()
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/config.yaml", []byte(yaml), 0o644)
	require.NoError(t, err)

	c, err := LoadFilesContainer(l, fs, "/config.yaml")
	require.NoError(t, err)
	require.NotNil(t, c)

	container, ok := c.(*Container)
	require.True(t, ok)

	return container
}

func TestValidate_RequiredFieldPresent(t *testing.T) {
	t.Parallel()

	c := newTestContainer(t, `
github:
  token: "abc123"
`)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	result := c.Validate(schema)
	assert.True(t, result.Valid())
	assert.Empty(t, result.Errors)
}

func TestValidate_RequiredFieldViolation(t *testing.T) {
	// Not parallel — t.Setenv modifies process environment
	t.Setenv("GITHUB_TOKEN", "")

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "missing key",
			yaml: `
log:
  level: info
`,
		},
		{
			name: "empty value",
			yaml: `
github:
  token: ""
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestContainer(t, tt.yaml)

			schema, err := NewSchema(WithStructSchema(testAppConfig{}))
			require.NoError(t, err)

			result := c.Validate(schema)
			assert.False(t, result.Valid())

			var found bool

			for _, e := range result.Errors {
				if e.Key == "github.token" {
					found = true
					assert.Contains(t, e.Message, "required")
				}
			}

			assert.True(t, found, "should have error for github.token")
		})
	}
}

func TestValidate_EnumValid(t *testing.T) {
	t.Parallel()

	c := newTestContainer(t, `
github:
  token: "abc"
log:
  level: debug
  format: json
`)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	result := c.Validate(schema)
	assert.True(t, result.Valid())
}

func TestValidate_EnumInvalid(t *testing.T) {
	t.Parallel()

	c := newTestContainer(t, `
github:
  token: "abc"
log:
  level: verbose
`)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	result := c.Validate(schema)
	assert.False(t, result.Valid())

	var found bool

	for _, e := range result.Errors {
		if e.Key == "log.level" {
			found = true
			assert.Contains(t, e.Message, "not allowed")
			assert.Contains(t, e.Hint, "debug")
		}
	}

	assert.True(t, found, "should have error for log.level enum violation")
}

func TestValidate_UnknownKey_Warning(t *testing.T) {
	t.Parallel()

	c := newTestContainer(t, `
github:
  token: "abc"
unknown_key: value
`)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	result := c.Validate(schema)
	// Non-strict: unknown keys produce warnings, not errors
	assert.True(t, result.Valid())
	assert.NotEmpty(t, result.Warnings)

	var found bool

	for _, w := range result.Warnings {
		if w.Key == "unknown_key" {
			found = true
		}
	}

	assert.True(t, found, "should warn about unknown_key")
}

func TestValidate_UnknownKey_Strict(t *testing.T) {
	t.Parallel()

	c := newTestContainer(t, `
github:
  token: "abc"
unknown_key: value
`)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}), WithStrictMode())
	require.NoError(t, err)

	result := c.Validate(schema)
	assert.False(t, result.Valid())

	var found bool

	for _, e := range result.Errors {
		if e.Key == "unknown_key" {
			found = true
			assert.Contains(t, e.Message, "unknown")
		}
	}

	assert.True(t, found, "should error on unknown_key in strict mode")
}

func TestValidate_NestedFields(t *testing.T) {
	t.Parallel()

	type nested struct {
		Database struct {
			Host string `config:"host" validate:"required"`
			Port int    `config:"port"`
		}
	}

	c := newTestContainer(t, `
database:
  host: "localhost"
  port: 5432
`)

	schema, err := NewSchema(WithStructSchema(nested{}))
	require.NoError(t, err)

	result := c.Validate(schema)
	assert.True(t, result.Valid())
}

func TestValidationResult_Error(t *testing.T) {
	t.Parallel()

	result := &ValidationResult{
		Errors: []ValidationError{
			{Key: "a.b", Message: "missing", Hint: "add it"},
			{Key: "c.d", Message: "wrong type", Hint: "use int"},
		},
	}

	errStr := result.Error()
	assert.Contains(t, errStr, "config validation failed:")
	assert.Contains(t, errStr, "a.b: missing")
	assert.Contains(t, errStr, "c.d: wrong type")
}

func TestLoadFilesContainerWithSchema_Valid(t *testing.T) {
	// Not parallel — t.Setenv modifies process environment
	t.Setenv("GITHUB_TOKEN", "")

	l := logger.NewNoop()
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/config.yaml", []byte(`
github:
  token: "secret"
log:
  level: info
`), 0o644)
	require.NoError(t, err)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	c, err := LoadFilesContainerWithSchema(l, fs, schema, "/config.yaml")
	require.NoError(t, err)
	require.NotNil(t, c)

	assert.Equal(t, "secret", c.GetString("github.token"))
}

func TestLoadFilesContainerWithSchema_Invalid(t *testing.T) {
	// Not parallel — t.Setenv modifies process environment
	t.Setenv("GITHUB_TOKEN", "")

	l := logger.NewNoop()
	fs := afero.NewMemMapFs()

	err := afero.WriteFile(fs, "/config.yaml", []byte(`
log:
  level: info
`), 0o644)
	require.NoError(t, err)

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	c, err := LoadFilesContainerWithSchema(l, fs, schema, "/config.yaml")
	require.Error(t, err)
	assert.Nil(t, c)
	assert.Contains(t, err.Error(), "github.token")
}

func TestLoadFilesContainerWithSchema_FileNotFound(t *testing.T) {
	t.Parallel()

	l := logger.NewNoop()
	fs := afero.NewMemMapFs()

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)

	c, err := LoadFilesContainerWithSchema(l, fs, schema, "/nonexistent.yaml")
	require.NoError(t, err)
	assert.Nil(t, c)
}
