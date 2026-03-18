package chat

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tiktoken-go/tokenizer"
)

func TestChunkByTokens(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		maxTokens int
		model     string
		wantErr   bool
		validate  func(t *testing.T, chunks []string, originalText string)
	}{
		{
			name:      "simple text with small chunks",
			text:      "Hello world! This is a test of the chunking functionality.",
			maxTokens: 5,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.NotEmpty(t, chunks)
				// Verify that joining chunks gives us back meaningful text
				joined := strings.Join(chunks, "")
				assert.NotEmpty(t, joined)
				// Each chunk should be reasonably sized (not empty)
				for i, chunk := range chunks {
					assert.NotEmpty(t, chunk, "chunk %d should not be empty", i)
				}
			},
		},
		{
			name:      "empty text",
			text:      "",
			maxTokens: 10,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.Equal(t, []string{""}, chunks)
			},
		},
		{
			name:      "single token",
			text:      "Hello",
			maxTokens: 1,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.NotEmpty(t, chunks)
				// Should have at least one chunk
				assert.GreaterOrEqual(t, len(chunks), 1)
				// First chunk should contain the text
				assert.NotEmpty(t, chunks[0])
			},
		},
		{
			name:      "text smaller than max tokens",
			text:      "Short text",
			maxTokens: 100,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.Len(t, chunks, 1)
				assert.Equal(t, originalText, chunks[0])
			},
		},
		{
			name:      "long text with reasonable chunk size",
			text:      strings.Repeat("This is a longer sentence that should be split into multiple chunks when processed. ", 20),
			maxTokens: 20,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.Greater(t, len(chunks), 1, "long text should produce multiple chunks")
				// Each chunk should be non-empty
				for i, chunk := range chunks {
					assert.NotEmpty(t, chunk, "chunk %d should not be empty", i)
				}
			},
		},
		{
			name:      "text with special characters",
			text:      "Hello 🌍! This contains émojis and spëcial characters: @#$%^&*()",
			maxTokens: 10,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.NotEmpty(t, chunks)
				// Verify all chunks are non-empty
				for i, chunk := range chunks {
					assert.NotEmpty(t, chunk, "chunk %d should not be empty", i)
				}
			},
		},
		{
			name:      "unknown model falls back to cl100k_base",
			text:      "Hello world",
			maxTokens: 5000,
			model:     "unknown-model-name",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.NotEmpty(t, chunks)
			},
		},
		{
			name:      "negative max tokens",
			text:      "Hello world",
			maxTokens: -1,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				// With negative maxTokens, we should get an empty result or handle it gracefully
				assert.NotNil(t, chunks)
			},
		},
		{
			name:      "very large max tokens",
			text:      "Short text",
			maxTokens: 1000000,
			model:     "gpt-4",
			wantErr:   false,
			validate: func(t *testing.T, chunks []string, originalText string) {
				assert.Len(t, chunks, 1)
				assert.Equal(t, originalText, chunks[0])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := chunkByTokens(tt.text, tt.maxTokens, tt.model)

			if tt.wantErr {
				assert.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, chunks)

			if tt.validate != nil {
				tt.validate(t, chunks, tt.text)
			}
		})
	}
}

func TestChunkByTokens_TokenCounting(t *testing.T) {
	text := "This is a test sentence that will be used to verify token counting."
	maxTokens := 5
	model := "gpt-4"

	chunks, err := chunkByTokens(text, maxTokens, model)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Verify that we can reconstruct the original meaning
	// (Note: exact reconstruction may not be possible due to tokenization)
	joined := strings.Join(chunks, "")
	assert.NotEmpty(t, joined)

	// Count tokens in each chunk to verify they don't exceed maxTokens
	enc, err := tokenizer.ForModel(tokenizer.Model(model))
	require.NoError(t, err)

	for i, chunk := range chunks {
		if chunk == "" {
			continue // Skip empty chunks
		}

		tokens, _, _ := enc.Encode(chunk)
		assert.LessOrEqual(t, len(tokens), maxTokens,
			"chunk %d has %d tokens, which exceeds maxTokens %d", i, len(tokens), maxTokens)
	}
}

func TestChunkByTokens_ConsistentResults(t *testing.T) {
	text := "This is a test to ensure consistent chunking results across multiple calls."
	maxTokens := 8
	model := "gpt-4"

	// Call the function multiple times
	chunks1, err1 := chunkByTokens(text, maxTokens, model)
	require.NoError(t, err1)

	chunks2, err2 := chunkByTokens(text, maxTokens, model)
	require.NoError(t, err2)

	chunks3, err3 := chunkByTokens(text, maxTokens, model)
	require.NoError(t, err3)

	// Results should be consistent
	assert.Equal(t, chunks1, chunks2)
	assert.Equal(t, chunks2, chunks3)
}

func TestChunkByTokens_DifferentModels(t *testing.T) {
	text := "Testing different models for tokenization."
	maxTokens := 10

	models := []string{"gpt-4", "gpt-3.5-turbo", "text-davinci-003"}

	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			chunks, err := chunkByTokens(text, maxTokens, model)
			if err != nil {
				t.Skipf("Model %s not supported by tokenizer: %v", model, err)

				return
			}

			assert.NotEmpty(t, chunks)
			// Verify all chunks are non-empty (unless the original text was empty)
			for i, chunk := range chunks {
				if text != "" {
					assert.NotEmpty(t, chunk, "chunk %d should not be empty for model %s", i, model)
				}
			}
		})
	}
}

func TestChunkByTokens_EdgeCases(t *testing.T) {
	t.Run("whitespace only", func(t *testing.T) {
		chunks, err := chunkByTokens("   \n\t  ", 5, "gpt-4")
		require.NoError(t, err)
		assert.NotEmpty(t, chunks)
	})

	t.Run("single character", func(t *testing.T) {
		chunks, err := chunkByTokens("a", 1, "gpt-4")
		require.NoError(t, err)
		assert.NotEmpty(t, chunks)
		assert.Equal(t, "a", chunks[0])
	})

	t.Run("unicode characters", func(t *testing.T) {
		chunks, err := chunkByTokens("こんにちは世界", 3, "gpt-4")
		require.NoError(t, err)
		assert.NotEmpty(t, chunks)
	})

	t.Run("mixed content", func(t *testing.T) {
		mixedText := "English text with 中文 and números 123 and symbols !@#$%"
		chunks, err := chunkByTokens(mixedText, 7, "gpt-4")
		require.NoError(t, err)
		assert.NotEmpty(t, chunks)
	})
}

// Benchmark tests to ensure the function performs reasonably.
func BenchmarkChunkByTokens(b *testing.B) {
	text := strings.Repeat("This is a benchmark test sentence that will be repeated many times to test performance. ", 100)
	maxTokens := 50
	model := "gpt-4"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := chunkByTokens(text, maxTokens, model)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkChunkByTokens_LargeText(b *testing.B) {
	text := strings.Repeat("Large text content for benchmarking the chunking function performance with substantial input data. ", 1000)
	maxTokens := 100
	model := "gpt-4"

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := chunkByTokens(text, maxTokens, model)
		if err != nil {
			b.Fatal(err)
		}
	}
}
