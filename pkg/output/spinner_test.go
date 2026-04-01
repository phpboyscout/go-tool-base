package output

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpinPlain_Success(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	_, err := spinPlain(context.Background(), &buf, "Loading", func(_ context.Context) (struct{}, error) {
		return struct{}{}, nil
	})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Loading...")
	assert.Contains(t, buf.String(), "Loading... done")
}

func TestSpinPlain_Error(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	_, err := spinPlain(context.Background(), &buf, "Loading", func(_ context.Context) (struct{}, error) {
		return struct{}{}, assert.AnError
	})

	require.Error(t, err)
	assert.Contains(t, buf.String(), "Loading...")
	assert.Contains(t, buf.String(), "Loading... failed")
}

func TestSpinPlain_WithResult(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	result, err := spinPlain(context.Background(), &buf, "Fetching", func(_ context.Context) (string, error) {
		return "hello", nil
	})

	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestSpinPlain_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var buf bytes.Buffer

	_, err := spinPlain(ctx, &buf, "Waiting", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, ctx.Err()
	})

	require.Error(t, err)
	assert.Contains(t, buf.String(), "Waiting... failed")
}
