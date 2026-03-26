// Package output provides structured output formatting for CLI commands.
// It supports text (default) and JSON output modes, allowing commands to
// produce machine-readable output for CI/CD pipelines and scripting.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/term"
	"github.com/cockroachdb/errors"
	"github.com/spf13/cobra"
)

// Format represents the output format for commands.
type Format string

const (
	// FormatText is the default human-readable output format.
	FormatText Format = "text"
	// FormatJSON produces machine-readable JSON output.
	FormatJSON Format = "json"
	// FormatYAML produces machine-readable YAML output.
	FormatYAML Format = "yaml"
	// FormatCSV produces comma-separated values output.
	FormatCSV Format = "csv"
	// FormatMarkdown produces a pipe-delimited markdown table with header separators.
	FormatMarkdown Format = "markdown"
	// FormatTSV produces tab-separated values output for shell pipelines.
	FormatTSV Format = "tsv"
)

// Status constants for the Response envelope.
const (
	StatusSuccess = "success"
	StatusError   = "error"
	StatusWarning = "warning"
)

// Response is the standard JSON envelope for all command output.
type Response struct {
	Status  string `json:"status"`
	Command string `json:"command"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Writer handles formatted output based on the configured format.
type Writer struct {
	format Format
	w      io.Writer
}

// NewWriter creates an output writer for the given format.
func NewWriter(w io.Writer, format Format) *Writer {
	return &Writer{format: format, w: w}
}

// Write outputs data in the configured format.
// For JSON format, data is marshalled to indented JSON.
// For text format, the textFunc is called to produce human-readable output.
func (o *Writer) Write(data any, textFunc func(io.Writer)) error {
	if o.format == FormatJSON {
		enc := json.NewEncoder(o.w)
		enc.SetIndent("", "  ")

		return enc.Encode(data)
	}

	textFunc(o.w)

	return nil
}

// IsJSON returns true if the writer is configured for JSON output.
func (o *Writer) IsJSON() bool {
	return o.format == FormatJSON
}

// RenderMarkdown renders markdown content to styled ANSI terminal output via glamour.
// It detects the terminal width automatically, falling back to 80 columns.
// If glamour fails for any reason, the original content is returned unchanged.
func RenderMarkdown(content string) string {
	width := 80
	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		width = w
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return content
	}

	out, err := r.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimSpace(out)
}

// Render writes markdown to the Writer using glamour styling in text mode.
// In JSON mode it is a no-op — callers should use Write for JSON output.
func (o *Writer) Render(markdown string) error {
	if o.format == FormatJSON {
		return nil
	}

	_, err := fmt.Fprint(o.w, RenderMarkdown(markdown))

	return err
}

// IsJSONOutput returns true if the --output flag on the command is set to "json".
func IsJSONOutput(cmd *cobra.Command) bool {
	val, err := cmd.Flags().GetString("output")
	if err != nil {
		return false
	}

	return val == string(FormatJSON)
}

// Emit writes a Response to cmd.OutOrStdout() when --output is "json".
// It is a no-op when the output format is text or the flag is absent.
func Emit(cmd *cobra.Command, resp Response) error {
	if !IsJSONOutput(cmd) {
		return nil
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")

	if err := enc.Encode(resp); err != nil {
		return errors.Wrap(err, "failed to encode JSON response")
	}

	return nil
}

// EmitError builds an error Response envelope and emits it via Emit.
func EmitError(cmd *cobra.Command, commandName string, err error) error {
	return Emit(cmd, Response{
		Status:  StatusError,
		Command: commandName,
		Error:   err.Error(),
	})
}
