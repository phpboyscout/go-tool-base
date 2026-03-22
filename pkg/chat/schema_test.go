package chat

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSchemaStruct struct {
	Name  string `json:"name" jsonschema:"description=The name"`
	Count int    `json:"count" jsonschema:"description=The count"`
}

func TestGenerateSchema(t *testing.T) {
	t.Parallel()

	schema := GenerateSchema[testSchemaStruct]()
	require.NotNil(t, schema)
}

func TestGenerateSchema_SimpleTypes(t *testing.T) {
	t.Parallel()

	type simple struct {
		Value string `json:"value"`
	}

	schema := GenerateSchema[simple]()
	assert.NotNil(t, schema)
}
