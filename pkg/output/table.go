package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/term"
	"github.com/cockroachdb/errors"
	"gopkg.in/yaml.v3"
)

// Column defines a single table column.
type Column struct {
	// Header is the display name shown in the table header row.
	Header string
	// Field is the struct field name or map key to extract the value from.
	Field string
	// Width is the fixed column width. Zero means auto-sized to content.
	Width int
	// Sortable indicates this column can be used as a sort key.
	Sortable bool
	// Formatter is an optional function to format the cell value.
	Formatter func(any) string
}

// TableWriter renders structured data as an aligned table or machine-readable format.
type TableWriter struct {
	w   io.Writer
	fmt Format
	cfg tableConfig
}

type tableConfig struct {
	columns        []Column
	sortBy         string
	sortDescending bool
	noHeader       bool
	noTruncation   bool
	maxWidth       int
}

// TableOption configures the TableWriter.
type TableOption func(*tableConfig)

// WithColumns explicitly defines the table columns. When not provided,
// columns are derived from struct tags on the row data type.
func WithColumns(cols ...Column) TableOption {
	return func(c *tableConfig) {
		c.columns = cols
	}
}

// WithSortBy sets the column to sort rows by. The column must be marked Sortable.
func WithSortBy(field string) TableOption {
	return func(c *tableConfig) {
		c.sortBy = field
	}
}

// WithSortDescending reverses the sort order.
func WithSortDescending() TableOption {
	return func(c *tableConfig) {
		c.sortDescending = true
	}
}

// WithNoHeader suppresses the header row in text table output.
func WithNoHeader() TableOption {
	return func(c *tableConfig) {
		c.noHeader = true
	}
}

// WithNoTruncation disables terminal-width truncation.
// Useful when output is piped to a file or another process.
func WithNoTruncation() TableOption {
	return func(c *tableConfig) {
		c.noTruncation = true
	}
}

// WithMaxWidth overrides automatic terminal width detection.
func WithMaxWidth(width int) TableOption {
	return func(c *tableConfig) {
		c.maxWidth = width
	}
}

// NewTableWriter creates a TableWriter that writes to the given io.Writer.
func NewTableWriter(w io.Writer, format Format, opts ...TableOption) *TableWriter {
	var cfg tableConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return &TableWriter{w: w, fmt: format, cfg: cfg}
}

const (
	columnPadding        = 3
	ellipsisMinWidth     = 3
	defaultTerminalWidth = 80
	ellipsis             = "..."
)

// truncateCell shrinks a cell to fit within w display columns. It is aware of
// multi-byte runes, wide (CJK/emoji) characters, and ANSI escape sequences, so
// it never splits a rune mid-sequence or miscounts display width. When there is
// room, an ellipsis tail is appended to signal truncation.
func truncateCell(s string, w int) string {
	if w <= 0 {
		return ""
	}

	if ansi.StringWidth(s) <= w {
		return s
	}

	tail := ellipsis
	if w <= ellipsisMinWidth {
		tail = ""
	}

	return ansi.Truncate(s, w, tail)
}

// padCell left-aligns s within w display columns, padding with spaces. Padding
// is computed from display width (not byte length) so multi-byte and wide
// content aligns correctly.
func padCell(s string, w int) string {
	gap := w - ansi.StringWidth(s)
	if gap <= 0 {
		return s
	}

	return s + strings.Repeat(" ", gap)
}

// WriteRows renders the provided slice as a table.
// The input must be a slice of structs (for tag-based columns) or []map[string]any.
// For JSON/YAML formats, the raw data is marshalled directly.
// For CSV format, columns are used as the header row.
// For text format, an aligned table with padding is produced.
func (t *TableWriter) WriteRows(rows any) error {
	switch t.fmt {
	case FormatJSON:
		return t.renderJSON(rows)
	case FormatYAML:
		return t.renderYAML(rows)
	case FormatCSV:
		return t.renderCSV(rows)
	case FormatTSV:
		return t.renderTSV(rows)
	case FormatMarkdown:
		return t.renderMarkdown(rows)
	case FormatText:
		return t.renderText(rows)
	}

	return t.renderText(rows)
}

func (t *TableWriter) resolveColumns(rows any) ([]Column, error) {
	if len(t.cfg.columns) > 0 {
		return t.cfg.columns, nil
	}

	return columnsFromStruct(rows)
}

func (t *TableWriter) renderJSON(rows any) error {
	enc := json.NewEncoder(t.w)
	enc.SetIndent("", "  ")

	return enc.Encode(rows)
}

func (t *TableWriter) renderYAML(rows any) error {
	return yaml.NewEncoder(t.w).Encode(rows)
}

func (t *TableWriter) renderTSV(rows any) error {
	return t.renderDelimited(rows, '\t')
}

func (t *TableWriter) renderCSV(rows any) error {
	return t.renderDelimited(rows, ',')
}

func (t *TableWriter) renderDelimited(rows any, delimiter rune) error {
	cols, err := t.resolveColumns(rows)
	if err != nil {
		return err
	}

	extracted, err := extractRows(cols, rows)
	if err != nil {
		return err
	}

	if t.cfg.sortBy != "" {
		if err := t.sortRows(cols, extracted); err != nil {
			return err
		}
	}

	w := csv.NewWriter(t.w)
	w.Comma = delimiter

	// Header row
	headers := make([]string, len(cols))
	for i, col := range cols {
		headers[i] = col.Header
	}

	if err := w.Write(headers); err != nil {
		return err
	}

	for _, row := range extracted {
		if err := w.Write(row); err != nil {
			return err
		}
	}

	w.Flush()

	return w.Error()
}

func (t *TableWriter) renderText(rows any) error {
	cols, err := t.resolveColumns(rows)
	if err != nil {
		return err
	}

	extracted, err := extractRows(cols, rows)
	if err != nil {
		return err
	}

	if t.cfg.sortBy != "" {
		if err := t.sortRows(cols, extracted); err != nil {
			return err
		}
	}

	// Calculate column widths
	widths := calculateWidths(cols, extracted)

	// Apply truncation
	if !t.cfg.noTruncation {
		termWidth := t.terminalWidth()
		widths = truncateWidths(widths, termWidth)
	}

	// Render header
	if !t.cfg.noHeader {
		t.writeRow(headerValues(cols), widths)
	}

	// Render data rows
	for _, row := range extracted {
		t.writeRow(row, widths)
	}

	return nil
}

func (t *TableWriter) renderMarkdown(rows any) error {
	cols, err := t.resolveColumns(rows)
	if err != nil {
		return err
	}

	extracted, err := extractRows(cols, rows)
	if err != nil {
		return err
	}

	if t.cfg.sortBy != "" {
		if err := t.sortRows(cols, extracted); err != nil {
			return err
		}
	}

	// Escape cell content up front so a literal "|" cannot open a new column
	// and newlines cannot open a new row. Escaping before width calculation
	// keeps the column widths consistent with what is actually rendered.
	headers := escapeMarkdownRow(headerValues(cols))

	escaped := make([][]string, len(extracted))
	for i, row := range extracted {
		escaped[i] = escapeMarkdownRow(row)
	}

	// Include the escaped header row in the width calculation so escaping a
	// header (rare, but possible) never desynchronises column widths.
	widthRows := escaped
	if !t.cfg.noHeader {
		widthRows = append([][]string{headers}, escaped...)
	}

	widths := calculateWidths(cols, widthRows)

	// Header row
	if !t.cfg.noHeader {
		t.writeMarkdownRow(headers, widths)
		t.writeMarkdownSeparator(widths)
	}

	// Data rows
	for _, row := range escaped {
		t.writeMarkdownRow(row, widths)
	}

	return nil
}

// escapeMarkdownRow returns a copy of cells with each value escaped via
// [escapeMarkdownCell].
func escapeMarkdownRow(cells []string) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = escapeMarkdownCell(c)
	}

	return out
}

func (t *TableWriter) writeMarkdownRow(cells []string, widths []int) {
	var sb strings.Builder

	sb.WriteString("|")

	for i, cell := range cells {
		if i >= len(widths) {
			break
		}

		w := widths[i]

		// Cells are pre-escaped by writeMarkdown before width calculation,
		// so a literal "|" or newline can never break out of the cell here.
		display := padCell(truncateCell(cell, w), w)

		fmt.Fprintf(&sb, " %s |", display)
	}

	_, _ = fmt.Fprintln(t.w, sb.String())
}

// escapeMarkdownCell neutralises characters that would otherwise break out of
// a GitHub-flavoured Markdown table cell: a literal pipe ("|") would start a
// new column and any newline (CR or LF) would start a new row. Pipes are
// backslash-escaped (the canonical GFM escape) and newlines are folded to the
// "<br>" line-break tag so multi-line content stays inside its cell.
func escapeMarkdownCell(s string) string {
	if !strings.ContainsAny(s, "|\r\n") {
		return s
	}

	r := strings.NewReplacer(
		"|", "\\|",
		"\r\n", "<br>",
		"\r", "<br>",
		"\n", "<br>",
	)

	return r.Replace(s)
}

func (t *TableWriter) writeMarkdownSeparator(widths []int) {
	var sb strings.Builder

	sb.WriteString("|")

	for _, w := range widths {
		sb.WriteString(" ")
		sb.WriteString(strings.Repeat("-", w))
		sb.WriteString(" |")
	}

	_, _ = fmt.Fprintln(t.w, sb.String())
}

func (t *TableWriter) writeRow(cells []string, widths []int) {
	var sb strings.Builder

	for i, cell := range cells {
		if i >= len(widths) {
			break
		}

		w := widths[i]
		if w <= 0 {
			continue
		}

		display := truncateCell(cell, w)

		if i < len(cells)-1 {
			sb.WriteString(padCell(display, w+columnPadding))
		} else {
			sb.WriteString(display)
		}
	}

	_, _ = fmt.Fprintln(t.w, strings.TrimRight(sb.String(), " "))
}

func (t *TableWriter) terminalWidth() int {
	if t.cfg.maxWidth > 0 {
		return t.cfg.maxWidth
	}

	if w, _, err := term.GetSize(os.Stdout.Fd()); err == nil && w > 0 {
		return w
	}

	return defaultTerminalWidth
}

func (t *TableWriter) sortRows(cols []Column, rows [][]string) error {
	colIdx := -1

	for i, col := range cols {
		if col.Header == t.cfg.sortBy {
			if !col.Sortable {
				return errors.Newf("column %q is not sortable", t.cfg.sortBy)
			}

			colIdx = i

			break
		}
	}

	if colIdx < 0 {
		return errors.Newf("unknown sort column: %q", t.cfg.sortBy)
	}

	desc := t.cfg.sortDescending

	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i][colIdx], rows[j][colIdx]

		// Attempt numeric comparison
		na, errA := strconv.ParseFloat(a, 64)
		nb, errB := strconv.ParseFloat(b, 64)

		if errA == nil && errB == nil {
			if desc {
				return na > nb
			}

			return na < nb
		}

		if desc {
			return a > b
		}

		return a < b
	})

	return nil
}

// columnsFromStruct derives columns from struct tags on the element type.
func columnsFromStruct(v any) ([]Column, error) {
	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Slice {
		t = t.Elem()
	}

	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil, errors.New("WriteRows requires a slice of structs or explicit WithColumns")
	}

	var cols []Column

	for _, f := range reflect.VisibleFields(t) {
		tag := f.Tag.Get("table")
		if tag == "" || tag == "-" {
			continue
		}

		parts := strings.Split(tag, ",")
		col := Column{
			Header:   parts[0],
			Field:    f.Name,
			Sortable: slices.Contains(parts[1:], "sortable"),
		}
		cols = append(cols, col)
	}

	if len(cols) == 0 {
		return nil, errors.New("no table tags found on struct")
	}

	return cols, nil
}

// extractRows converts the input slice into string cell values.
func extractRows(cols []Column, rows any) ([][]string, error) {
	rv := reflect.ValueOf(rows)
	if rv.Kind() != reflect.Slice {
		return nil, errors.New("rows must be a slice")
	}

	result := make([][]string, 0, rv.Len())

	for i := range rv.Len() {
		row := rv.Index(i)
		cells := make([]string, len(cols))

		for j, col := range cols {
			val := extractField(row, col.Field)
			if col.Formatter != nil {
				cells[j] = col.Formatter(val)
			} else {
				cells[j] = fmt.Sprintf("%v", val)
			}
		}

		result = append(result, cells)
	}

	return result, nil
}

// extractField gets a field value from a struct or map.
func extractField(v reflect.Value, field string) any {
	if v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		f := v.FieldByName(field)
		if f.IsValid() {
			return f.Interface()
		}
	case reflect.Map:
		f := v.MapIndex(reflect.ValueOf(field))
		if f.IsValid() {
			return f.Interface()
		}
	case reflect.Invalid, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128,
		reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Pointer,
		reflect.Slice, reflect.String, reflect.UnsafePointer:
		// Not a struct or map — cannot extract a named field.
	}

	return ""
}

func headerValues(cols []Column) []string {
	h := make([]string, len(cols))
	for i, col := range cols {
		h[i] = col.Header
	}

	return h
}

func calculateWidths(cols []Column, rows [][]string) []int {
	widths := make([]int, len(cols))

	for i, col := range cols {
		if col.Width > 0 {
			widths[i] = col.Width

			continue
		}

		widths[i] = ansi.StringWidth(col.Header)

		for _, row := range rows {
			if i < len(row) {
				if cw := ansi.StringWidth(row[i]); cw > widths[i] {
					widths[i] = cw
				}
			}
		}
	}

	return widths
}

func truncateWidths(widths []int, maxWidth int) []int {
	total := 0
	for i, w := range widths {
		total += w
		if i < len(widths)-1 {
			total += columnPadding
		}
	}

	if total <= maxWidth {
		return widths
	}

	// Truncate from the last column backwards
	result := make([]int, len(widths))
	copy(result, widths)

	for i := len(result) - 1; i >= 0 && total > maxWidth; i-- {
		excess := total - maxWidth
		reduction := min(excess, result[i]-1)

		if reduction > 0 {
			result[i] -= reduction
			total -= reduction
		}
	}

	return result
}
