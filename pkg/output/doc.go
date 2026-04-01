// Package output provides structured output formatting, table rendering,
// and progress indicators for CLI commands.
//
// # Output Formats
//
// Commands can produce output in multiple formats controlled by the --output
// flag. The Format type defines the supported formats: text (default), json,
// yaml, csv, tsv, and markdown.
//
// The Response type provides a standard JSON envelope for all command output,
// and the Emit/EmitError helpers handle format-aware serialisation.
//
// # Table Rendering
//
// TableWriter renders tabular data with automatic column sizing, sorting,
// and format-aware output. Use struct tags to define columns:
//
//	type Row struct {
//	    Name   string `table:"NAME,sortable"`
//	    Status string `table:"STATUS"`
//	}
//
//	tw := output.NewTableWriter(os.Stdout, output.FormatText)
//	tw.WriteRows(rows)
//
// # Progress Indicators
//
// Three components handle long-running operation feedback:
//
//   - Spin/SpinWithResult — animated spinner for indeterminate operations
//   - Progress — progress bar for operations with known total
//   - Status — multi-step status display with success/warn/fail icons
//
// All three detect interactive mode (TTY + CI check) and fall back to
// plain text logging in non-interactive environments.
//
//	// Spinner
//	output.Spin(ctx, "Checking for updates", func(ctx context.Context) error {
//	    return updater.Check(ctx)
//	})
//
//	// Progress bar
//	bar := output.NewProgress(len(files), "Processing")
//	for _, f := range files {
//	    processFile(f)
//	    bar.Increment()
//	}
//	bar.Done()
//
//	// Status display
//	s := output.NewStatus()
//	s.Update("Loading config")
//	s.Success("Config loaded")
//	s.Done()
package output
