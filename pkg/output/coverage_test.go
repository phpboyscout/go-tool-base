package output

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- IsInteractive ---

func TestIsInteractive_CIDisablesInteractive(t *testing.T) {
	// Not parallel: mutates the CI environment variable.
	t.Setenv("CI", "true")

	assert.False(t, IsInteractive(), "CI=true must force non-interactive output")
}

func TestIsInteractive_NonTTY(t *testing.T) {
	// Not parallel: mutates the CI environment variable.
	t.Setenv("CI", "")

	// The test runner's stdout is not a TTY, so this is false here. The call
	// still exercises the term.IsTerminal branch.
	assert.False(t, IsInteractive(), "a non-TTY stdout must be non-interactive")
}

// --- Constructors ---

func TestNewProgress_Defaults(t *testing.T) {
	// Not parallel: NewProgress consults the CI env var via IsInteractive.
	t.Setenv("CI", "true")

	p := NewProgress(50, "Building")
	require.NotNil(t, p)
	assert.Equal(t, 50, p.total)
	assert.Equal(t, "Building", p.description)
	assert.Equal(t, -1, p.lastPercent)
	assert.False(t, p.interactive, "CI=true must produce a non-interactive progress")
	assert.NotNil(t, p.w)
}

func TestNewStatus_Defaults(t *testing.T) {
	// Not parallel: NewStatus consults the CI env var via IsInteractive.
	t.Setenv("CI", "true")

	s := NewStatus()
	require.NotNil(t, s)
	assert.False(t, s.interactive, "CI=true must produce a non-interactive status")
	assert.NotNil(t, s.w)
}

// --- Spin / SpinWithResult (non-interactive path) ---

func TestSpin_NonInteractive_Success(t *testing.T) {
	// Not parallel: forces non-interactive mode via the CI env var.
	t.Setenv("CI", "true")

	called := false
	err := Spin(context.Background(), "Working", func(_ context.Context) error {
		called = true

		return nil
	})

	require.NoError(t, err)
	assert.True(t, called, "fn should run on the non-interactive path")
}

func TestSpin_NonInteractive_Error(t *testing.T) {
	// Not parallel: forces non-interactive mode via the CI env var.
	t.Setenv("CI", "true")

	sentinel := assert.AnError
	err := Spin(context.Background(), "Working", func(_ context.Context) error {
		return sentinel
	})

	require.ErrorIs(t, err, sentinel)
}

func TestSpinWithResult_NonInteractive(t *testing.T) {
	// Not parallel: forces non-interactive mode via the CI env var.
	t.Setenv("CI", "true")

	got, err := SpinWithResult(context.Background(), "Fetching", func(_ context.Context) (int, error) {
		return 42, nil
	})

	require.NoError(t, err)
	assert.Equal(t, 42, got)
}

// --- spinnerModel.Init / runFn ---

func TestSpinnerModel_InitBatchesTickAndRun(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "Loading", func(_ context.Context) (string, error) {
		return "done", nil
	})

	cmd := model.Init()
	require.NotNil(t, cmd, "Init should return a batched command")
}

func TestSpinnerModel_RunFnProducesDoneMsg(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "Loading", func(_ context.Context) (string, error) {
		return "value", nil
	})

	cmd := model.runFn()
	require.NotNil(t, cmd)

	msg := cmd()
	done, ok := msg.(spinDoneMsg[string])
	require.True(t, ok, "runFn must emit a spinDoneMsg")
	assert.Equal(t, "value", done.result)
	require.NoError(t, done.err)
}

// --- Status.Update interactive reclear ---

func TestStatus_Update_InteractiveReclearsActiveLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := &Status{w: &buf, interactive: true}

	s.Update("First")
	require.True(t, s.active)

	// A second Update while active must clear the previous line first.
	s.Update("Second")

	out := buf.String()
	assert.Contains(t, out, "\r\033[K", "an active line should be cleared before redraw")
	assert.Contains(t, out, iconSpin+" Second")
}

// --- Progress.Done interactive newline ---

func TestProgress_Done_InteractiveEmitsNewline(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       4,
		description: "Compiling",
		w:           &buf,
		interactive: true,
		lastPercent: -1,
	}

	p.Done()

	out := buf.String()
	assert.Contains(t, out, "4/4")
	assert.True(t, strings.HasSuffix(out, "\n"), "interactive Done should terminate with a newline")
}

// --- truncateCell edge cases ---

func TestTruncateCell_ZeroOrNegativeWidth(t *testing.T) {
	t.Parallel()

	assert.Empty(t, truncateCell("hello", 0))
	assert.Empty(t, truncateCell("hello", -5))
}

func TestTruncateCell_FitsExactly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "abc", truncateCell("abc", 3))
}

// --- extractRows / extractField error and edge paths ---

func TestExtractRows_NonSliceErrors(t *testing.T) {
	t.Parallel()

	_, err := extractRows([]Column{{Header: "X", Field: "X"}}, 123)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be a slice")
}

func TestExtractField_MissingStructField(t *testing.T) {
	t.Parallel()

	type s struct{ Name string }

	v := reflect.ValueOf(s{Name: "x"})
	assert.Empty(t, extractField(v, "Nonexistent"))
}

func TestExtractField_MissingMapKey(t *testing.T) {
	t.Parallel()

	m := map[string]any{"a": 1}
	v := reflect.ValueOf(m)
	assert.Empty(t, extractField(v, "missing"))
}

func TestExtractField_NonStructNonMap(t *testing.T) {
	t.Parallel()

	v := reflect.ValueOf(42)
	assert.Empty(t, extractField(v, "anything"))
}

func TestExtractField_PointerToStruct(t *testing.T) {
	t.Parallel()

	type s struct{ Name string }

	ptr := &s{Name: "value"}
	// A direct pointer Value: extractField unwraps the single pointer layer to
	// reach the struct.
	v := reflect.ValueOf(ptr)
	assert.Equal(t, "value", extractField(v, "Name"))
}

// --- writeRow: zero-width column is skipped, extra cells break out ---

func TestWriteRow_ZeroWidthColumnSkipped(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText)

	// Width 0 for the first column means it is skipped entirely.
	tw.writeRow([]string{"skip", "keep"}, []int{0, 4})

	out := strings.TrimRight(buf.String(), "\n")
	assert.NotContains(t, out, "skip")
	assert.Contains(t, out, "keep")
}

func TestWriteRow_MoreCellsThanWidthsBreaks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText)

	// Only one width is provided; the extra "second" cell must be dropped.
	tw.writeRow([]string{"first", "second"}, []int{5})

	out := buf.String()
	assert.Contains(t, out, "first")
	assert.NotContains(t, out, "second")
}

// --- writeMarkdownRow: more cells than widths breaks ---

func TestWriteMarkdownRow_MoreCellsThanWidthsBreaks(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatMarkdown)

	tw.writeMarkdownRow([]string{"a", "b", "c"}, []int{1})

	out := buf.String()
	assert.Contains(t, out, "a")
	assert.NotContains(t, out, "c")
}

// --- sort error propagation across delimited / markdown renderers ---

func TestRenderDelimited_SortErrorPropagates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	// STATUS is not sortable, so sorting by it must surface an error.
	tw := NewTableWriter(&buf, FormatCSV, WithSortBy("STATUS"))

	err := tw.WriteRows(testRows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not sortable")
}

func TestRenderMarkdown_SortErrorPropagates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatMarkdown, WithSortBy("STATUS"))

	err := tw.WriteRows(testRows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not sortable")
}

func TestRenderDelimited_UnknownSortColumn(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatTSV, WithSortBy("NOPE"))

	err := tw.WriteRows(testRows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown sort column")
}

// --- sortRows non-numeric descending branch ---

func TestSortRows_StringDescending(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(),
		WithSortBy("NAME"), WithSortDescending())

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)
	// Descending alphabetical: worker, gateway, api
	assert.Contains(t, lines[1], "worker")
	assert.Contains(t, lines[2], "gateway")
	assert.Contains(t, lines[3], "api")
}

// --- columnsFromStruct: non-struct element type ---

func TestColumnsFromStruct_NonStructErrors(t *testing.T) {
	t.Parallel()

	_, err := columnsFromStruct([]int{1, 2, 3})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "slice of structs")
}

// --- WriteRows resolveColumns error propagates (no columns, bad type) ---

func TestWriteRows_ResolveColumnsError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText)

	err := tw.WriteRows([]int{1})
	require.Error(t, err)
}

// --- a tea.Cmd batch sanity: ensure Init's cmd is callable without panic ---

func TestSpinnerModel_InitCmdInvokable(t *testing.T) {
	t.Parallel()

	model := newSpinnerModel(context.Background(), "x", func(_ context.Context) (struct{}, error) {
		return struct{}{}, nil
	})

	cmd := model.Init()
	require.NotNil(t, cmd)

	// Invoking the batch command should not panic and should yield a message.
	msg := cmd()
	assert.NotNil(t, msg)
}
