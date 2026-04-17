package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	maxSpillFileSize = 1 << 20 // 1 MB
	maxSpillFiles    = 10
	spillPattern     = "telemetry-spill-*.json"
)

// spillToDisk writes the current buffer to a spill file and clears the buffer.
// Must be called with c.mu held.
func (c *Collector) spillToDisk() {
	if c.dataDir == "" {
		return
	}

	data, err := json.Marshal(c.buffer)
	if err != nil {
		c.log.Debug("failed to marshal spill data", "error", err)

		return
	}

	// If the serialised buffer exceeds the file size cap, split into chunks
	// rather than truncating (which would produce invalid JSON).
	chunks := splitSpillData(c.buffer, data)

	c.pruneSpillFiles()

	for i, chunk := range chunks {
		filename := filepath.Join(c.dataDir, fmt.Sprintf("telemetry-spill-%d-%d.json", time.Now().UnixNano(), i))

		if err := os.WriteFile(filename, chunk, filePermissions); err != nil {
			c.log.Debug("failed to write spill file", "error", err)

			return
		}
	}

	c.buffer = c.buffer[:0]
}

// splitSpillData splits the buffer into chunks that each fit within maxSpillFileSize.
// If the entire buffer fits, returns a single chunk. If individual events are
// larger than the cap, they are included as single-element chunks (best effort).
func splitSpillData(events []Event, fullData []byte) [][]byte {
	if len(fullData) <= maxSpillFileSize {
		return [][]byte{fullData}
	}

	var chunks [][]byte

	var batch []Event

	for _, e := range events {
		batch = append(batch, e)

		data, err := json.Marshal(batch)
		if err == nil && len(data) <= maxSpillFileSize {
			continue
		}

		chunks, batch = flushBatch(chunks, batch, e)
	}

	if len(batch) > 0 {
		if data, err := json.Marshal(batch); err == nil {
			chunks = append(chunks, data)
		}
	}

	return chunks
}

// flushBatch appends the current batch (minus the last event) to chunks and
// returns the updated chunks and a new batch starting with the overflow event.
func flushBatch(chunks [][]byte, batch []Event, overflow Event) ([][]byte, []Event) {
	if len(batch) > 1 {
		if prev, err := json.Marshal(batch[:len(batch)-1]); err == nil {
			chunks = append(chunks, prev)
		}

		return chunks, []Event{overflow}
	}

	// Single event exceeds cap — include it anyway (best effort)
	if single, err := json.Marshal(batch); err == nil {
		chunks = append(chunks, single)
	}

	return chunks, nil
}

// flushSpillFiles reads and sends all spill files, cleaning up after successful delivery.
// Delivery mode controls whether files are deleted before or after send.
func (c *Collector) flushSpillFiles(ctx context.Context) error {
	if c.dataDir == "" {
		return nil
	}

	files, err := filepath.Glob(filepath.Join(c.dataDir, spillPattern))
	if err != nil || len(files) == 0 {
		return err
	}

	sort.Strings(files)

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			c.log.Debug("failed to read spill file", "error", err, "file", f)

			continue
		}

		var events []Event
		if err := json.Unmarshal(data, &events); err != nil {
			c.log.Debug("failed to unmarshal spill file", "error", err, "file", f)
			c.removeSpillFile(f)

			continue
		}

		if c.deliveryMode == props.DeliveryAtMostOnce {
			c.removeSpillFile(f)
		}

		if err := c.backend.Send(ctx, events); err != nil {
			c.log.Debug("failed to send spill file", "error", err, "file", f)

			continue
		}

		if c.deliveryMode == props.DeliveryAtLeastOnce {
			c.removeSpillFile(f)
		}
	}

	return nil
}

// removeSpillFile deletes the spill file at path and logs at WARN if
// the removal fails — so operators can see when files are accumulating
// due to permissions, read-only filesystems, or races. Logs only the
// basename, never the full path (which would leak the data directory
// location). Closes L-3 from
// docs/development/reports/security-audit-2026-04-17.md.
func (c *Collector) removeSpillFile(path string) {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		c.log.Warn("failed to remove spill file",
			"file", filepath.Base(path),
			"error", err)
	}
}

// deleteSpillFiles removes all spill files. Used by Drop() on consent withdrawal.
func (c *Collector) deleteSpillFiles() error {
	if c.dataDir == "" {
		return nil
	}

	files, _ := filepath.Glob(filepath.Join(c.dataDir, spillPattern))
	for _, f := range files {
		c.removeSpillFile(f)
	}

	return nil
}

// pruneSpillFiles removes the oldest spill files when the count exceeds maxSpillFiles.
// Must be called with c.mu held.
func (c *Collector) pruneSpillFiles() {
	if c.dataDir == "" {
		return
	}

	files, _ := filepath.Glob(filepath.Join(c.dataDir, spillPattern))
	if len(files) < maxSpillFiles {
		return
	}

	sort.Strings(files)

	for _, f := range files[:len(files)-maxSpillFiles+1] {
		c.removeSpillFile(f)
	}
}
