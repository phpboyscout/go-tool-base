package chat

import (
	"testing"

	"github.com/invopop/jsonschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

func TestConvertToGeminiSchema_Nil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, convertToGeminiSchema(nil))
}

func TestConvertToGeminiSchema_SimpleObject(t *testing.T) {
	t.Parallel()

	schema := GenerateSchema[testSchemaStruct]()
	s, ok := schema.(*jsonschema.Schema)
	require.True(t, ok)

	gs := convertToGeminiSchema(s)
	require.NotNil(t, gs)
	assert.Equal(t, genai.TypeObject, gs.Type)
	assert.NotEmpty(t, gs.Properties)
}

func TestConvertToGeminiSchema_Array(t *testing.T) {
	t.Parallel()

	s := &jsonschema.Schema{
		Type: "array",
		Items: &jsonschema.Schema{
			Type: "string",
		},
	}

	gs := convertToGeminiSchema(s)
	require.NotNil(t, gs)
	assert.Equal(t, genai.TypeArray, gs.Type)
	require.NotNil(t, gs.Items)
	assert.Equal(t, genai.TypeString, gs.Items.Type)
}

func TestConvertToGeminiSchema_WithEnum(t *testing.T) {
	t.Parallel()

	s := &jsonschema.Schema{
		Type: "string",
		Enum: []any{"a", "b", "c"},
	}

	gs := convertToGeminiSchema(s)
	require.NotNil(t, gs)
	assert.Equal(t, []string{"a", "b", "c"}, gs.Enum)
}

func TestMapType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected genai.Type
	}{
		{"object", genai.TypeObject},
		{"array", genai.TypeArray},
		{"string", genai.TypeString},
		{"number", genai.TypeNumber},
		{"integer", genai.TypeInteger},
		{"boolean", genai.TypeBoolean},
		{"unknown", genai.TypeUnspecified},
		{"", genai.TypeUnspecified},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, mapType(tt.input))
		})
	}
}
