package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProgress_PlainOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       100,
		description: "Processing",
		w:           &buf,
		interactive: false,
		lastPercent: -1,
	}

	// Increment to 10% — should log
	p.IncrementBy(10)
	assert.Contains(t, buf.String(), "10/100")

	// Increment to 15% — should NOT log (not at next 10% boundary)
	p.IncrementBy(5)
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	assert.Len(t, lines, 1, "should only log at 10%% intervals")

	// Increment to 20% — should log
	p.IncrementBy(5)
	assert.Contains(t, buf.String(), "20/100")
}

func TestProgress_Done(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       10,
		description: "Done test",
		w:           &buf,
		interactive: false,
		lastPercent: -1,
	}

	p.Done()

	assert.Contains(t, buf.String(), "10/10")
	assert.Contains(t, buf.String(), "100%")
}

func TestProgress_ZeroTotal(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       0,
		description: "Empty",
		w:           &buf,
		interactive: false,
		lastPercent: -1,
	}

	// Should not panic
	p.Increment()
	p.Done()
}

func TestProgress_ExceedsTotal(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       5,
		description: "Overflow",
		w:           &buf,
		interactive: false,
		lastPercent: -1,
	}

	p.IncrementBy(10)

	assert.Equal(t, 5, p.current, "current should be capped at total")
}

func TestProgress_InteractiveRendering(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	p := &Progress{
		total:       10,
		description: "Building",
		w:           &buf,
		interactive: true,
		lastPercent: -1,
	}

	p.IncrementBy(5)

	output := buf.String()
	assert.Contains(t, output, "Building")
	assert.Contains(t, output, "5/10")
	assert.Contains(t, output, "50%")
}
