package otelcore

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveSettings_OverlaysExplicitSignalValues(t *testing.T) {
	t.Parallel()

	shared := Config{
		Endpoint: "https://shared:4318",
		Headers:  map[string]string{"authorization": "Bearer shared"},
		Insecure: false,
	}
	signal := SignalConfig{
		Enabled:  true,
		Endpoint: "https://signal:4318",
		Headers:  map[string]string{"authorization": "Bearer signal"},
		Insecure: true,
	}

	got := ResolveSettings(shared, signal, SignalOverrides{
		Endpoint: true,
		Insecure: true,
	})

	assert.True(t, got.Enabled)
	assert.Equal(t, "https://signal:4318", got.Endpoint)
	assert.Equal(t, "Bearer shared", got.Headers["authorization"])
	assert.True(t, got.Insecure)
}
