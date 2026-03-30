package telemetry

import (
	"context"
	"encoding/json"
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

	if len(data) > maxSpillFileSize {
		data = data[:maxSpillFileSize]
	}

	c.pruneSpillFiles()

	filename := filepath.Join(c.dataDir, fmt.Sprintf("telemetry-spill-%d.json", time.Now().UnixNano()))

	if err := os.WriteFile(filename, data, filePermissions); err != nil {
		c.log.Debug("failed to write spill file", "error", err)

		return
	}

	c.buffer = c.buffer[:0]
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
			_ = os.Remove(f)

			continue
		}

		if c.deliveryMode == props.DeliveryAtMostOnce {
			_ = os.Remove(f)
		}

		if err := c.backend.Send(ctx, events); err != nil {
			c.log.Debug("failed to send spill file", "error", err, "file", f)

			continue
		}

		if c.deliveryMode == props.DeliveryAtLeastOnce {
			_ = os.Remove(f)
		}
	}

	return nil
}

// deleteSpillFiles removes all spill files. Used by Drop() on consent withdrawal.
func (c *Collector) deleteSpillFiles() error {
	if c.dataDir == "" {
		return nil
	}

	files, _ := filepath.Glob(filepath.Join(c.dataDir, spillPattern))
	for _, f := range files {
		_ = os.Remove(f)
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
		_ = os.Remove(f)
	}
}
