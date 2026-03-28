package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testData struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

func TestWriter_JSON(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, FormatJSON)

	data := testData{Name: "test-tool", Version: "1.0.0"}

	err := w.Write(data, func(w io.Writer) {
		t.Error("text function should not be called for JSON output")
	})

	require.NoError(t, err)

	var result testData
	err = json.Unmarshal(buf.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, "test-tool", result.Name)
	assert.Equal(t, "1.0.0", result.Version)
}

func TestWriter_JSONIndent(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, FormatJSON)

	data := testData{Name: "tool", Version: "2.0.0"}
	err := w.Write(data, func(w io.Writer) {})

	require.NoError(t, err)
	assert.Contains(t, buf.String(), "  \"name\"")
	assert.Contains(t, buf.String(), "  \"version\"")
}

func TestWriter_Text(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, FormatText)

	data := testData{Name: "test-tool", Version: "1.0.0"}
	textCalled := false

	err := w.Write(data, func(w io.Writer) {
		textCalled = true
		_, _ = fmt.Fprintf(w, "%s %s\n", data.Name, data.Version)
	})

	require.NoError(t, err)
	assert.True(t, textCalled)
	assert.Equal(t, "test-tool 1.0.0\n", buf.String())
}

func TestWriter_DefaultIsText(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, Format("unknown"))

	textCalled := false

	err := w.Write(nil, func(w io.Writer) {
		textCalled = true
		_, _ = fmt.Fprint(w, "hello")
	})

	require.NoError(t, err)
	assert.True(t, textCalled)
	assert.Equal(t, "hello", buf.String())
}

func TestWriter_IsJSON(t *testing.T) {
	t.Parallel()

	assert.True(t, NewWriter(nil, FormatJSON).IsJSON())
	assert.False(t, NewWriter(nil, FormatText).IsJSON())
	assert.False(t, NewWriter(nil, Format("yaml")).IsJSON())
}

// --- RenderMarkdown ---

func TestRenderMarkdown_ProducesOutput(t *testing.T) {
	t.Parallel()

	result := RenderMarkdown("# Hello\n\nThis is **bold** text.")
	assert.NotEmpty(t, result)
}

func TestRenderMarkdown_FallsBackOnEmptyContent(t *testing.T) {
	t.Parallel()

	// Empty string should produce empty (or whitespace-only) output.
	result := RenderMarkdown("")
	assert.Empty(t, result)
}

func TestRenderMarkdown_PlainTextPassThrough(t *testing.T) {
	t.Parallel()

	// Non-markdown plain text should still return non-empty output.
	result := RenderMarkdown("just some plain text")
	assert.NotEmpty(t, result)
}

// --- Writer.Render ---

func TestWriter_Render_TextMode(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, FormatText)

	err := w.Render("# Hello\n\nThis is markdown.")
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestWriter_Render_JSONMode_NoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	w := NewWriter(&buf, FormatJSON)

	err := w.Render("# Hello\n\nThis is markdown.")
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

// --- IsJSONOutput ---

func newCmdWithOutputFlag(format string) *cobra.Command {
	cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	cmd.Flags().String("output", "text", "output format")

	if format != "" {
		_ = cmd.Flags().Set("output", format)
	}

	return cmd
}

func TestIsJSONOutput_JSON(t *testing.T) {
	t.Parallel()

	cmd := newCmdWithOutputFlag("json")
	assert.True(t, IsJSONOutput(cmd))
}

func TestIsJSONOutput_Text(t *testing.T) {
	t.Parallel()

	cmd := newCmdWithOutputFlag("text")
	assert.False(t, IsJSONOutput(cmd))
}

func TestIsJSONOutput_Default(t *testing.T) {
	t.Parallel()

	// No --output flag defined at all.
	cmd := &cobra.Command{Use: "test"}
	assert.False(t, IsJSONOutput(cmd))
}

func TestIsJSONOutput_OtherValue(t *testing.T) {
	t.Parallel()

	cmd := newCmdWithOutputFlag("yaml")
	assert.False(t, IsJSONOutput(cmd))
}

// --- Emit ---

func TestEmit_JSONMode_WritesEnvelope(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newCmdWithOutputFlag("json")
	cmd.SetOut(&buf)

	resp := Response{
		Status:  StatusSuccess,
		Command: "version",
		Data:    map[string]any{"version": "1.0.0"},
	}

	err := Emit(cmd, resp)
	require.NoError(t, err)

	var got Response
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, StatusSuccess, got.Status)
	assert.Equal(t, "version", got.Command)
}

func TestEmit_JSONMode_IndentedOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newCmdWithOutputFlag("json")
	cmd.SetOut(&buf)

	err := Emit(cmd, Response{Status: StatusSuccess, Command: "test"})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "  \"status\"")
}

func TestEmit_TextMode_NoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newCmdWithOutputFlag("text")
	cmd.SetOut(&buf)

	err := Emit(cmd, Response{Status: StatusSuccess, Command: "test"})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestEmit_NoFlag_NoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&buf)

	err := Emit(cmd, Response{Status: StatusSuccess, Command: "test"})
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

// --- EmitError ---

func TestEmitError_ProducesErrorEnvelope(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newCmdWithOutputFlag("json")
	cmd.SetOut(&buf)

	err := EmitError(cmd, "update", errors.New("something failed"))
	require.NoError(t, err)

	var got Response
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	assert.Equal(t, StatusError, got.Status)
	assert.Equal(t, "update", got.Command)
	assert.Equal(t, "something failed", got.Error)
	assert.Nil(t, got.Data)
}

func TestEmitError_TextMode_NoOp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cmd := newCmdWithOutputFlag("text")
	cmd.SetOut(&buf)

	err := EmitError(cmd, "update", errors.New("something failed"))
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

// errWriter always returns an error from Write, used to exercise encode error paths.
type errWriter struct{}

func (e *errWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write error")
}

func TestEmit_WriteError_ReturnsError(t *testing.T) {
	t.Parallel()

	cmd := newCmdWithOutputFlag("json")
	cmd.SetOut(&errWriter{})

	err := Emit(cmd, Response{Status: StatusSuccess, Command: "test"})
	require.Error(t, err)
}
