package chat

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"
)

// failingUnmarshaler is a json.Unmarshaler whose UnmarshalJSON always fails,
// exercising the error branch of assignText's json.Unmarshaler case.
type failingUnmarshaler struct{}

func (failingUnmarshaler) UnmarshalJSON([]byte) error {
	return assert.AnError
}

// capturingUnmarshaler records the bytes it is given and succeeds.
type capturingUnmarshaler struct {
	captured string
}

func (c *capturingUnmarshaler) UnmarshalJSON(data []byte) error {
	c.captured = string(data)

	return nil
}

func TestAssignText(t *testing.T) {
	t.Parallel()

	t.Run("string_target", func(t *testing.T) {
		t.Parallel()

		var out string
		require.NoError(t, assignText("hello", &out))
		assert.Equal(t, "hello", out)
	})

	t.Run("json_unmarshaler_target_success", func(t *testing.T) {
		t.Parallel()

		var cu capturingUnmarshaler
		require.NoError(t, assignText(`{"k":1}`, &cu))
		assert.Equal(t, `{"k":1}`, cu.captured)
	})

	t.Run("json_unmarshaler_target_error", func(t *testing.T) {
		t.Parallel()

		err := assignText("anything", failingUnmarshaler{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal Claude text response")
	})

	t.Run("default_struct_target_success", func(t *testing.T) {
		t.Parallel()

		var out struct {
			Answer string `json:"answer"`
		}
		require.NoError(t, assignText(`{"answer":"42"}`, &out))
		assert.Equal(t, "42", out.Answer)
	})

	t.Run("default_struct_target_invalid_json", func(t *testing.T) {
		t.Parallel()

		var out struct {
			Answer string `json:"answer"`
		}
		err := assignText("not json", &out)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires a *string or json.Unmarshaler target")
	})
}

func TestBaseURLHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"empty", "", ""},
		{"https_with_path", "https://api.anthropic.com/v1", "api.anthropic.com"},
		{"https_with_port", "https://host.example:8443/path", "host.example"},
		{"unparseable", "https://%zz", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, baseURLHost(tc.baseURL))
		})
	}
}

// TestOpenAISaveRestore_RoundTrip exercises the OpenAI Save/Restore message
// marshalling paths directly on the concrete type.
func TestOpenAISaveRestore_RoundTrip(t *testing.T) {
	t.Parallel()

	orig := &OpenAI{}
	orig.cfg.Model = "gpt-4o"
	orig.cfg.SystemPrompt = "be concise"

	snap, err := orig.Save()
	require.NoError(t, err)
	assert.Equal(t, ProviderOpenAI, snap.Provider)
	assert.Equal(t, "gpt-4o", snap.Model)

	dst := &OpenAI{}
	require.NoError(t, dst.Restore(snap))
	assert.Equal(t, "be concise", dst.cfg.SystemPrompt)
}

func TestOpenAIRestore_ProviderMismatch(t *testing.T) {
	t.Parallel()

	dst := &OpenAI{}
	err := dst.Restore(&Snapshot{Provider: ProviderGemini, Messages: json.RawMessage(`[]`)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider mismatch")
}

func TestOpenAIRestore_AcceptsCompatibleProvider(t *testing.T) {
	t.Parallel()

	dst := &OpenAI{}
	err := dst.Restore(&Snapshot{Provider: ProviderOpenAICompatible, Messages: json.RawMessage(`[]`)})
	require.NoError(t, err)
}

func TestOpenAIRestore_BadMessagesJSON(t *testing.T) {
	t.Parallel()

	dst := &OpenAI{}
	err := dst.Restore(&Snapshot{Provider: ProviderOpenAI, Messages: json.RawMessage(`{bad`)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling OpenAI messages")
}

// TestGeminiSaveRestore_RoundTrip exercises Gemini Save/Restore which were
// previously uncovered.
func TestGeminiSaveRestore_RoundTrip(t *testing.T) {
	t.Parallel()

	orig := &Gemini{model: "gemini-2.0-flash"}
	orig.cfg.SystemPrompt = "be helpful"

	snap, err := orig.Save()
	require.NoError(t, err)
	assert.Equal(t, ProviderGemini, snap.Provider)
	assert.Equal(t, "gemini-2.0-flash", snap.Model)

	dst := &Gemini{config: &genai.GenerateContentConfig{}}
	require.NoError(t, dst.Restore(snap))
	assert.Equal(t, "be helpful", dst.cfg.SystemPrompt)
	require.NotNil(t, dst.config.SystemInstruction)
}

func TestGeminiRestore_ProviderMismatch(t *testing.T) {
	t.Parallel()

	dst := &Gemini{}
	err := dst.Restore(&Snapshot{Provider: ProviderOpenAI, Messages: json.RawMessage(`[]`)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider mismatch")
}

func TestGeminiRestore_BadMessagesJSON(t *testing.T) {
	t.Parallel()

	dst := &Gemini{}
	err := dst.Restore(&Snapshot{Provider: ProviderGemini, Messages: json.RawMessage(`{bad`)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling Gemini history")
}
