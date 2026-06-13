package output

import (
	"bytes"
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/cockroachdb/errors"
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

// ctrlCMsg is the deterministic key event for a Ctrl-C interrupt. Driving the
// model's Update directly avoids raising a real OS signal and keeps the test
// race-free (no live Bubble Tea program / TTY required).
var ctrlCMsg = tea.KeyPressMsg{Mod: tea.ModCtrl, Code: 'c'}

func TestSpinnerModel_CtrlCReturnsCanceled(t *testing.T) {
	t.Parallel()

	// A spinner wrapping a long-running fn (the fn never completes here).
	model := newSpinnerModel(context.Background(), "Working", func(ctx context.Context) (string, error) {
		<-ctx.Done()

		return "should-not-be-used", ctx.Err()
	})

	// The wrapped context must not be cancelled before the interrupt.
	require.NoError(t, model.ctx.Err())

	updated, cmd := model.Update(ctrlCMsg)
	require.NotNil(t, cmd, "ctrl+c should issue a quit command")

	final, ok := updated.(spinnerModel[string])
	require.True(t, ok)

	// Interruption is surfaced as context.Canceled, not a false success.
	require.Error(t, final.err)
	require.ErrorIs(t, final.err, context.Canceled)
	assert.Empty(t, final.result, "result must not be treated as completed on interrupt")
	assert.True(t, final.done)

	// The fn's context was cancelled so the in-flight work can unwind.
	require.ErrorIs(t, final.ctx.Err(), context.Canceled)

	// The spinner line is cleared once done.
	assert.Empty(t, final.View().Content)
}

func TestSpinnerModel_DoneMsgRecordsResult(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "Working", func(_ context.Context) (string, error) {
		return "ok", nil
	})

	updated, cmd := model.Update(spinDoneMsg[string]{result: "ok", err: nil})
	require.NotNil(t, cmd)

	final, ok := updated.(spinnerModel[string])
	require.True(t, ok)

	require.NoError(t, final.err)
	assert.Equal(t, "ok", final.result)
	assert.True(t, final.done)
}

func TestSpinnerModel_DoneMsgPreservesError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("boom")

	model := newSpinnerModel(context.Background(), "Working", func(_ context.Context) (string, error) {
		return "", sentinel
	})

	updated, _ := model.Update(spinDoneMsg[string]{err: sentinel})

	final, ok := updated.(spinnerModel[string])
	require.True(t, ok)

	require.ErrorIs(t, final.err, sentinel)
}

func TestSpinnerModel_ViewWhileActive(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "Loading", func(_ context.Context) (struct{}, error) {
		return struct{}{}, nil
	})

	// Before completion the view shows the spinner and message.
	assert.Contains(t, model.View().Content, "Loading")

	// A non-key, non-done message drives the spinner tick without finishing.
	updated, _ := model.Update(struct{ unrelated bool }{})
	final, ok := updated.(spinnerModel[struct{}])
	require.True(t, ok)
	assert.False(t, final.done)
}

func TestSpinnerModel_IgnoresNonCtrlCKeys(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "Loading", func(_ context.Context) (struct{}, error) {
		return struct{}{}, nil
	})

	updated, _ := model.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	final, ok := updated.(spinnerModel[struct{}])
	require.True(t, ok)

	assert.False(t, final.done, "an unrelated key must not stop the spinner")
	require.NoError(t, final.ctx.Err(), "an unrelated key must not cancel the context")
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
