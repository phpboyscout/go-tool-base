package output

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatus_PlainOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := &Status{w: &buf, interactive: false}

	s.Update("Loading configuration")
	s.Success("Configuration loaded")
	s.Update("Connecting to API")
	s.Warn("Connection slow")
	s.Update("Processing")
	s.Fail("Processing failed")
	s.Done()

	output := buf.String()

	assert.Contains(t, output, "Loading configuration...")
	assert.Contains(t, output, iconSuccess+" Configuration loaded")
	assert.Contains(t, output, "Connecting to API...")
	assert.Contains(t, output, iconWarn+" Connection slow")
	assert.Contains(t, output, "Processing...")
	assert.Contains(t, output, iconFail+" Processing failed")
}

func TestStatus_InteractiveOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := &Status{w: &buf, interactive: true}

	s.Update("Loading")
	assert.Contains(t, buf.String(), iconSpin+" Loading")

	s.Success("Loaded")
	assert.Contains(t, buf.String(), iconSuccess+" Loaded")
}

func TestStatus_DoneCleansUp(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := &Status{w: &buf, interactive: true}

	s.Update("Working")
	assert.True(t, s.active)

	s.Done()
	assert.False(t, s.active)
}

func TestStatus_SuccessResetsActive(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	s := &Status{w: &buf, interactive: true}

	s.Update("Step 1")
	assert.True(t, s.active)

	s.Success("Step 1 complete")
	assert.False(t, s.active)
}
