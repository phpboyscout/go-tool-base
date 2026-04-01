package output

import (
	"fmt"
	"io"
	"os"
	"sync"
)

const (
	// progressBarWidth is the width of the progress bar in characters.
	progressBarWidth = 40
	// progressReportInterval is the percentage interval for non-interactive logging.
	progressReportInterval = 10
	// percentMax is the maximum percentage value.
	percentMax = 100
)

// Progress tracks progress of a known-total operation.
// In interactive terminals, displays an animated progress bar.
// In non-interactive environments, logs periodic status lines.
// Safe for concurrent use from multiple goroutines.
type Progress struct {
	total       int
	current     int
	description string
	w           io.Writer
	interactive bool
	lastPercent int
	mu          sync.Mutex
}

// NewProgress creates a progress indicator with the given total and description.
func NewProgress(total int, description string) *Progress {
	return &Progress{
		total:       total,
		description: description,
		w:           os.Stderr,
		interactive: IsInteractive(),
		lastPercent: -1,
	}
}

// Increment advances the progress by one unit.
func (p *Progress) Increment() {
	p.IncrementBy(1)
}

// IncrementBy advances the progress by n units.
func (p *Progress) IncrementBy(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current += n
	if p.current > p.total {
		p.current = p.total
	}

	p.render()
}

// Done marks the progress as complete and cleans up the display.
func (p *Progress) Done() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.current = p.total
	p.render()

	if p.interactive {
		_, _ = fmt.Fprintln(p.w)
	}
}

func (p *Progress) render() {
	if p.interactive {
		p.renderInteractive()
	} else {
		p.renderPlain()
	}
}

func (p *Progress) renderInteractive() {
	pct := p.percent()
	filled := progressBarWidth * pct / percentMax

	bar := make([]byte, progressBarWidth)

	for i := range bar {
		switch {
		case i < filled:
			bar[i] = '='
		case i == filled && filled < progressBarWidth:
			bar[i] = '>'
		default:
			bar[i] = ' '
		}
	}

	_, _ = fmt.Fprintf(p.w, "\r%s [%s] %d/%d (%d%%)", p.description, string(bar), p.current, p.total, pct)
}

func (p *Progress) renderPlain() {
	pct := p.percent()

	// Only log at progressReportInterval boundaries to avoid flooding
	bucket := (pct / progressReportInterval) * progressReportInterval
	if bucket > p.lastPercent {
		p.lastPercent = bucket

		_, _ = fmt.Fprintf(p.w, "%s: %d/%d (%d%%)\n", p.description, p.current, p.total, pct)
	}
}

func (p *Progress) percent() int {
	if p.total == 0 {
		return 0
	}

	return p.current * percentMax / p.total
}
