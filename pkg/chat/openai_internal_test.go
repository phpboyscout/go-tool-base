package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunkByTokens(t *testing.T) {
	t.Parallel()

	t.Run("empty_text_returns_single_empty_string", func(t *testing.T) {
		t.Parallel()

		chunks, err := chunkByTokens("", 100, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Equal(t, []string{""}, chunks)
	})

	t.Run("zero_max_tokens_returns_empty_slice", func(t *testing.T) {
		t.Parallel()

		chunks, err := chunkByTokens("hello", 0, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Empty(t, chunks)
	})

	t.Run("negative_max_tokens_returns_empty_slice", func(t *testing.T) {
		t.Parallel()

		chunks, err := chunkByTokens("hello", -1, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Empty(t, chunks)
	})

	t.Run("short_text_returns_single_chunk", func(t *testing.T) {
		t.Parallel()

		chunks, err := chunkByTokens("hello world", 100, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Len(t, chunks, 1)
		assert.Equal(t, "hello world", chunks[0])
	})

	t.Run("long_text_produces_multiple_chunks", func(t *testing.T) {
		t.Parallel()

		// Build a prompt that exceeds the small token limit so chunking occurs.
		longPrompt := strings.Repeat("token ", 200)

		chunks, err := chunkByTokens(longPrompt, 10, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Greater(t, len(chunks), 1, "expected multiple chunks for a long prompt with small max tokens")

		// Reassembled chunks must reproduce the original text.
		reassembled := strings.Join(chunks, "")
		assert.Equal(t, longPrompt, reassembled)
	})

	t.Run("unknown_model_falls_back_to_cl100k_base", func(t *testing.T) {
		t.Parallel()

		chunks, err := chunkByTokens("hello world", 100, "unknown-model-xyz")
		require.NoError(t, err)
		assert.Len(t, chunks, 1)
		assert.Equal(t, "hello world", chunks[0])
	})

	t.Run("text_exactly_at_max_tokens_returns_single_chunk", func(t *testing.T) {
		t.Parallel()

		// "hello" is a single token in most tokenizers; use a small max to verify boundary.
		chunks, err := chunkByTokens("hello", 1, DefaultModelOpenAI)
		require.NoError(t, err)
		assert.Len(t, chunks, 1)
		assert.Equal(t, "hello", chunks[0])
	})
}
