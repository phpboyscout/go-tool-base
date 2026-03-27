package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testAppConfig struct {
	Github struct {
		Token string `config:"github.token" validate:"required"`
	}
	Log struct {
		Level  string `config:"log.level" enum:"debug,info,warn,error" default:"info"`
		Format string `config:"log.format" enum:"json,logfmt,text" default:"text"`
	}
	Server struct {
		Port int    `config:"server.port" default:"8080"`
		Host string `config:"server.host"`
	}
}

func TestNewSchema_WithStructSchema(t *testing.T) {
	t.Parallel()

	schema, err := NewSchema(WithStructSchema(testAppConfig{}))
	require.NoError(t, err)
	require.NotNil(t, schema)

	fields := schema.Fields()

	// github.token
	f, ok := fields["github.token"]
	require.True(t, ok)
	assert.Equal(t, "string", f.Type)
	assert.True(t, f.Required)

	// log.level with enum
	f, ok = fields["log.level"]
	require.True(t, ok)
	assert.Equal(t, "string", f.Type)
	assert.False(t, f.Required)
	assert.Equal(t, []any{"debug", "info", "warn", "error"}, f.Enum)
	assert.Equal(t, "info", f.Default)

	// server.port
	f, ok = fields["server.port"]
	require.True(t, ok)
	assert.Equal(t, "int", f.Type)
	assert.Equal(t, "8080", f.Default)
}

func TestNewSchema_NoFields(t *testing.T) {
	t.Parallel()

	_, err := NewSchema()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no fields defined")
}

func TestNewSchema_StrictMode(t *testing.T) {
	t.Parallel()

	schema, err := NewSchema(
		WithStructSchema(testAppConfig{}),
		WithStrictMode(),
	)
	require.NoError(t, err)
	assert.True(t, schema.strict)
}

func TestWithStructSchema_NestedStruct(t *testing.T) {
	t.Parallel()

	type nested struct {
		Database struct {
			Host string `config:"host" validate:"required"`
			Port int    `config:"port"`
		}
	}

	schema, err := NewSchema(WithStructSchema(nested{}))
	require.NoError(t, err)

	fields := schema.Fields()
	_, ok := fields["database.host"]
	assert.True(t, ok, "should have database.host key")

	_, ok = fields["database.port"]
	assert.True(t, ok, "should have database.port key")
}

func TestWithStructSchema_SkipDash(t *testing.T) {
	t.Parallel()

	type skipFields struct {
		Name    string `config:"name"`
		Ignored string `config:"-"`
	}

	schema, err := NewSchema(WithStructSchema(skipFields{}))
	require.NoError(t, err)

	fields := schema.Fields()
	assert.Len(t, fields, 1)
	_, ok := fields["name"]
	assert.True(t, ok)
}
