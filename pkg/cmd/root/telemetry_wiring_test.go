package root

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/telemetry"
)

func TestBuildTelemetryCollector_FeatureDisabled(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, false)
	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.False(t, c.Enabled(), "telemetry feature disabled -> noop collector")
}

func TestBuildTelemetryCollector_EnabledFeatureButConfigOff(t *testing.T) {
	t.Parallel()

	// Feature enabled but telemetry.enabled is false in config -> noop.
	props := telemetryProps(t, p.TelemetryConfig{}, true)
	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.False(t, c.Enabled())
}

func TestBuildTelemetryCollector_ForceEnabledHTTPBackend(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		ForceEnabled: true,
		Endpoint:     "https://telemetry.example.com/v1/events",
	}, true)

	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.True(t, c.Enabled())
	assert.Contains(t, c.BackendInfo(), "http")
}

func TestBuildTelemetryCollector_EnvOverrideEnablesLocalFile(t *testing.T) {
	// Not parallel: sets TELEMETRY_ENABLED / TELEMETRY_LOCAL env vars.
	t.Setenv("TELEMETRY_ENABLED", "true")
	t.Setenv("TELEMETRY_LOCAL", "true")
	t.Setenv("HOME", t.TempDir())

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	c := buildTelemetryCollector(context.Background(), props)
	require.NotNil(t, c)
	assert.True(t, c.Enabled())
	assert.Contains(t, c.BackendInfo(), "file")
}

func TestSelectTelemetryBackend_CustomBackend(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Backend: func(*p.Props) any { return telemetry.NewNoopBackend() },
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Equal(t, "custom", info)
}

func TestSelectTelemetryBackend_InvalidCustomBackend(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Backend: func(*p.Props) any { return "not a backend" },
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "noop")
}

func TestSelectTelemetryBackend_LocalOnly(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{LocalOnly: true}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "file")
}

func TestSelectTelemetryBackend_OTel(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		OTelEndpoint: "https://collector.example.com:4318",
		OTelInsecure: true,
		OTelHeaders:  map[string]string{"x-token": "abc"},
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "otlp")
}

func TestSelectTelemetryBackend_HTTP(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{
		Endpoint: "https://telemetry.example.com/events",
	}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "http")
}

func TestSelectTelemetryBackend_DefaultNoop(t *testing.T) {
	t.Parallel()

	props := telemetryProps(t, p.TelemetryConfig{}, true)

	b, info := selectTelemetryBackend(context.Background(), props, telemetry.Config{}, t.TempDir())
	require.NotNil(t, b)
	assert.Contains(t, info, "noop")
}

// TestPromptTelemetryConsent_ApplyCreatesFile is covered end to end in
// pkg/cmd/telemetry (setTelemetryEnabled); the root prompt shares the same
// Apply path. Here the no-config-dir guard is pinned: with HOME unresolvable
// the prompt's persistence degrades to a debug log rather than writing to a
// bogus relative path.
func TestPromptTelemetryConsent_NoConfigDirDoesNotWrite(t *testing.T) {
	// Not parallel: clears HOME so os.UserHomeDir fails.
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // windows fallback

	props := consentProps(t, "", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}

func TestPromptTelemetryConsent_FeatureDisabled(t *testing.T) {
	t.Parallel()

	props := consentProps(t, "", false)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}

func TestPromptTelemetryConsent_ForceEnabled(t *testing.T) {
	t.Parallel()

	props := consentProps(t, "", true)
	props.Tool.Telemetry.ForceEnabled = true
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}

func TestPromptTelemetryConsent_EnvSet(t *testing.T) {
	// Not parallel: sets TELEMETRY_ENABLED.
	t.Setenv("TELEMETRY_ENABLED", "true")

	props := consentProps(t, "", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}

func TestPromptTelemetryConsent_AlreadyConfigured(t *testing.T) {
	t.Parallel()

	props := consentProps(t, "telemetry:\n  enabled: true\n", true)
	require.True(t, props.Config.View().IsSet("telemetry.enabled"))
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}

func TestPromptTelemetryConsent_CIFlagSkips(t *testing.T) {
	t.Parallel()

	// The --ci flag reaches config through the flags layer, so the CI skip is
	// driven by the config key.
	props := consentProps(t, "ci: true\n", true)
	assert.NotPanics(t, func() {
		promptTelemetryConsent(t.Context(), props)
	})
}
