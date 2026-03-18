package verifier

// AIResponse represents the structured response from the AI for code generation/fixing.
type AIResponse struct {
	GoCode          string   `json:"go_code" jsonschema:"description=The converted/fixed Go code (FULL FILE CONTENT)"`
	TestCode        string   `json:"test_code" jsonschema:"description=Unit tests for the converted code (FULL FILE CONTENT)"`
	Recommendations []string `json:"recommendations" jsonschema:"description=Recommendations for the converted code"`
}
