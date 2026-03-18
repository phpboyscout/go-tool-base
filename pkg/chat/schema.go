package chat

import (
	"github.com/invopop/jsonschema"
)

// GenerateSchema creates a JSON schema for a given type T.
// OpenAI's structured outputs feature uses a subset of JSON schema.
// The reflector is configured with flags to ensure the generated schema
// complies with this specific subset.
func GenerateSchema[T any]() any {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties:  false,
		DoNotReference:             true,
		RequiredFromJSONSchemaTags: false,
	}

	var v T

	schema := reflector.Reflect(v)

	return schema
}
