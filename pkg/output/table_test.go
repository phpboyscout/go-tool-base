package output

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type serviceRow struct {
	Name   string `json:"name"   table:"NAME,sortable" yaml:"name"`
	Status string `json:"status" table:"STATUS"         yaml:"status"`
	Port   int    `json:"port"   table:"PORT,sortable"  yaml:"port"`
	Uptime string `json:"uptime" table:"UPTIME"         yaml:"uptime"`
}

var testRows = []serviceRow{
	{Name: "api", Status: "running", Port: 8080, Uptime: "3d2h"},
	{Name: "worker", Status: "stopped", Port: 0, Uptime: "0s"},
	{Name: "gateway", Status: "running", Port: 443, Uptime: "7d"},
}

func TestTableWriter_TextFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation())

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.GreaterOrEqual(t, len(lines), 4, "should have header + 3 data rows")

	// Header row should contain column names
	assert.Contains(t, lines[0], "NAME")
	assert.Contains(t, lines[0], "STATUS")
	assert.Contains(t, lines[0], "PORT")
	assert.Contains(t, lines[0], "UPTIME")

	// Data rows should contain values
	assert.Contains(t, lines[1], "api")
	assert.Contains(t, lines[2], "worker")
	assert.Contains(t, lines[3], "gateway")
}

func TestTableWriter_JSONFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatJSON)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	var result []serviceRow
	require.NoError(t, json.Unmarshal(buf.Bytes(), &result))
	assert.Len(t, result, 3)
	assert.Equal(t, "api", result[0].Name)
}

func TestTableWriter_YAMLFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatYAML)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	var result []serviceRow
	require.NoError(t, yaml.Unmarshal(buf.Bytes(), &result))
	assert.Len(t, result, 3)
	assert.Equal(t, "api", result[0].Name)
	assert.Equal(t, 8080, result[0].Port)
}

func TestTableWriter_CSVFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatCSV)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	r := csv.NewReader(&buf)
	records, err := r.ReadAll()
	require.NoError(t, err)
	require.Len(t, records, 4, "header + 3 data rows")

	assert.Equal(t, []string{"NAME", "STATUS", "PORT", "UPTIME"}, records[0])
	assert.Equal(t, "api", records[1][0])
	assert.Equal(t, "8080", records[1][2])
}

func TestTableWriter_TSVFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatTSV)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4, "header + 3 data rows")

	// Header row
	headers := strings.Split(lines[0], "\t")
	assert.Equal(t, []string{"NAME", "STATUS", "PORT", "UPTIME"}, headers)

	// Data row
	fields := strings.Split(lines[1], "\t")
	assert.Equal(t, "api", fields[0])
	assert.Equal(t, "running", fields[1])
	assert.Equal(t, "8080", fields[2])

	// No commas in output (not CSV)
	assert.NotContains(t, buf.String(), ",")
}

func TestTableWriter_MarkdownFormat(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatMarkdown)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 5, "header + separator + 3 data rows")

	// Header row: | NAME ... | STATUS ... | PORT ... | UPTIME ... |
	assert.True(t, strings.HasPrefix(lines[0], "| NAME"), "header should start with pipe-delimited NAME")
	assert.True(t, strings.HasSuffix(lines[0], "|"), "header should end with pipe")
	assert.Contains(t, lines[0], "STATUS")
	assert.Contains(t, lines[0], "PORT")

	// Separator row: | ---- | ------ | ---- | ------ |
	assert.True(t, strings.HasPrefix(lines[1], "|"))
	assert.Contains(t, lines[1], "---")
	assert.True(t, strings.HasSuffix(lines[1], "|"))

	// Data rows
	assert.Contains(t, lines[2], "api")
	assert.Contains(t, lines[2], "running")
	assert.True(t, strings.HasPrefix(lines[2], "|"))
	assert.True(t, strings.HasSuffix(lines[2], "|"))
}

func TestTableWriter_MarkdownFormat_NoHeader(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatMarkdown, WithNoHeader())

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 3, "should have 3 data rows, no header or separator")
	assert.NotContains(t, lines[0], "NAME")
	assert.NotContains(t, buf.String(), "---")
}

func TestTableWriter_MarkdownFormat_ColumnAlignment(t *testing.T) {
	t.Parallel()

	type alignRow struct {
		Short string `json:"short" table:"A,sortable"`
		Long  string `json:"long"  table:"LONGHEADER"`
	}

	rows := []alignRow{
		{Short: "x", Long: "y"},
		{Short: "abc", Long: "defghi"},
	}

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatMarkdown)

	err := tw.WriteRows(rows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)

	// All rows should have the same length (padded consistently)
	assert.Len(t, lines[1], len(lines[0]), "header and separator should be same width")
	assert.Len(t, lines[2], len(lines[0]), "header and data rows should be same width")
	assert.Len(t, lines[3], len(lines[0]), "all rows should be same width")
}

func TestTableWriter_SortBy(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(), WithSortBy("NAME"))

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)

	// Sorted alphabetically by NAME: api, gateway, worker
	assert.Contains(t, lines[1], "api")
	assert.Contains(t, lines[2], "gateway")
	assert.Contains(t, lines[3], "worker")
}

func TestTableWriter_SortDescending(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(), WithSortBy("PORT"), WithSortDescending())

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 4)

	// Sorted by PORT descending: 8080, 443, 0
	assert.Contains(t, lines[1], "8080")
	assert.Contains(t, lines[2], "443")
	assert.Contains(t, lines[3], "0")
}

func TestTableWriter_SortNonSortable(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(), WithSortBy("STATUS"))

	err := tw.WriteRows(testRows)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not sortable")
}

func TestTableWriter_NoHeader(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(), WithNoHeader())

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 3, "should have 3 data rows and no header")
	assert.NotContains(t, lines[0], "NAME")
}

func TestTableWriter_Truncation(t *testing.T) {
	t.Parallel()

	type longRow struct {
		Name  string `json:"name"  table:"NAME,sortable"`
		Value string `json:"value" table:"VALUE"`
	}

	rows := []longRow{
		{Name: "short", Value: "this is a very long value that should be truncated when the terminal is narrow"},
	}

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithMaxWidth(30))

	err := tw.WriteRows(rows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for _, line := range lines {
		assert.LessOrEqual(t, len(line), 30, "line should not exceed max width: %q", line)
	}
}

func TestTableWriter_NoTruncation(t *testing.T) {
	t.Parallel()

	type longRow struct {
		Name  string `json:"name"  table:"NAME,sortable"`
		Value string `json:"value" table:"VALUE"`
	}

	longValue := strings.Repeat("x", 200)
	rows := []longRow{
		{Name: "test", Value: longValue},
	}

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation())

	err := tw.WriteRows(rows)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), longValue)
}

func TestTableWriter_MaxWidth(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithMaxWidth(40))

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	for _, line := range lines {
		assert.LessOrEqual(t, len(line), 40, "line should not exceed max width: %q", line)
	}
}

func TestTableWriter_EmptyRows(t *testing.T) {
	t.Parallel()

	t.Run("TextHeaderOnly", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		tw := NewTableWriter(&buf, FormatText, WithNoTruncation())

		err := tw.WriteRows([]serviceRow{})
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		require.Len(t, lines, 1)
		assert.Contains(t, lines[0], "NAME")
	})

	t.Run("JSONEmptyArray", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		tw := NewTableWriter(&buf, FormatJSON)

		err := tw.WriteRows([]serviceRow{})
		require.NoError(t, err)
		assert.Contains(t, buf.String(), "[]")
	})
}

func TestTableWriter_StructTags(t *testing.T) {
	t.Parallel()

	cols, err := columnsFromStruct([]serviceRow{})
	require.NoError(t, err)
	require.Len(t, cols, 4)

	assert.Equal(t, "NAME", cols[0].Header)
	assert.Equal(t, "Name", cols[0].Field)
	assert.True(t, cols[0].Sortable)

	assert.Equal(t, "STATUS", cols[1].Header)
	assert.False(t, cols[1].Sortable)

	assert.Equal(t, "PORT", cols[2].Header)
	assert.True(t, cols[2].Sortable)
}

func TestTableWriter_ExplicitColumns(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(),
		WithColumns(
			Column{Header: "SERVICE", Field: "Name", Sortable: true},
			Column{Header: "PORT", Field: "Port"},
		),
	)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Contains(t, lines[0], "SERVICE")
	assert.Contains(t, lines[0], "PORT")
	assert.NotContains(t, lines[0], "STATUS")
	assert.NotContains(t, lines[0], "UPTIME")
}

func TestTableWriter_CustomFormatter(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(),
		WithColumns(
			Column{Header: "NAME", Field: "Name"},
			Column{Header: "PORT", Field: "Port", Formatter: func(v any) string {
				return fmt.Sprintf(":%v", v)
			}},
		),
	)

	err := tw.WriteRows(testRows)
	require.NoError(t, err)

	assert.Contains(t, buf.String(), ":8080")
	assert.Contains(t, buf.String(), ":0")
}

func TestTableWriter_MapSlice(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"name": "alpha", "count": 10},
		{"name": "beta", "count": 20},
	}

	var buf bytes.Buffer
	tw := NewTableWriter(&buf, FormatText, WithNoTruncation(),
		WithColumns(
			Column{Header: "NAME", Field: "name"},
			Column{Header: "COUNT", Field: "count", Sortable: true},
		),
	)

	err := tw.WriteRows(rows)
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[0], "NAME")
	assert.Contains(t, lines[1], "alpha")
	assert.Contains(t, lines[2], "beta")
}

func TestColumnsFromStruct_NoTags(t *testing.T) {
	t.Parallel()

	type noTags struct {
		Name string
	}

	_, err := columnsFromStruct([]noTags{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no table tags found")
}

func TestColumnsFromStruct_SkipDash(t *testing.T) {
	t.Parallel()

	type withDash struct {
		Name    string `table:"NAME"`
		Ignored string `table:"-"`
		Status  string `table:"STATUS"`
	}

	cols, err := columnsFromStruct([]withDash{})
	require.NoError(t, err)
	require.Len(t, cols, 2)
	assert.Equal(t, "NAME", cols[0].Header)
	assert.Equal(t, "STATUS", cols[1].Header)
}
