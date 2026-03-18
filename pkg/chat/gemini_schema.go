package chat

import (
	"github.com/invopop/jsonschema"
	"google.golang.org/genai"
)

// convertToGeminiSchema converts a jsonschema.Schema to a genai.Schema.
func convertToGeminiSchema(s *jsonschema.Schema) *genai.Schema {
	if s == nil {
		return nil
	}

	gs := &genai.Schema{
		Description: s.Description,
	}

	gs.Type = mapType(s.Type)
	if gs.Type == genai.TypeObject && s.Properties != nil {
		gs.Properties = make(map[string]*genai.Schema)
		for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
			gs.Properties[pair.Key] = convertToGeminiSchema(pair.Value)
		}

		gs.Required = s.Required
	} else if gs.Type == genai.TypeArray && s.Items != nil {
		gs.Items = convertToGeminiSchema(s.Items)
	}

	if len(s.Enum) > 0 {
		for _, e := range s.Enum {
			if str, ok := e.(string); ok {
				gs.Enum = append(gs.Enum, str)
			}
		}
	}

	return gs
}

func mapType(t string) genai.Type {
	switch t {
	case "object":
		return genai.TypeObject
	case "array":
		return genai.TypeArray
	case "string":
		return genai.TypeString
	case "number":
		return genai.TypeNumber
	case "integer":
		return genai.TypeInteger
	case "boolean":
		return genai.TypeBoolean
	default:
		return genai.TypeUnspecified
	}
}
