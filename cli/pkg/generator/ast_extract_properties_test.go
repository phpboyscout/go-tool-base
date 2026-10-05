package generator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func applyToolFields(t *testing.T, fields map[string]string) ManifestProperties {
	t.Helper()

	var mp ManifestProperties

	for name, expr := range fields {
		applyLiteralToolField(&mp, name, parseExpr(t, expr))
	}

	return mp
}

func TestApplyLiteralToolField_RecoversTheRenderedLiteral(t *testing.T) {
	t.Parallel()

	mp := applyToolFields(t, map[string]string{
		"EnvPrefix":           `"TOOL"`,
		"UpdatePolicy":        `props.UpdatePolicyPrompt`,
		"UpdateCheckInterval": `6 * time.Hour`,
		"MCP":                 `props.MCPConfig{Mode: props.MCPDirect}`,
		"Help":                `props.SlackHelp{Channel: "#help", Team: "acme"}`,
		"Telemetry":           `props.TelemetryConfig{Endpoint: "https://t", OTelEndpoint: "https://o"}`,
		"Bootstrap":           `props.BootstrapPolicy{AutoInitialise: true, SkipConfigCheck: []string{"init"}, AuxiliaryCommands: []string{"version"}}`,
		"Signing":             `props.SigningConfig{EmbeddedKeys: trustkeys.Keys(), RequireChecksum: props.BoolPtr(true)}`,
		"Config": `props.ConfigSpec{
			Layers:  []props.ConfigLayer{props.LayerDefaults, props.ConfigLayer("team"), props.LayerFlags},
			Format:  "toml",
			Sources: []props.ConfigSource{{Name: "team", Kind: "http", Required: props.BoolPtr(false), Writable: props.BoolPtr(true)}},
		}`,
		"Unrelated": `42`,
	})

	assert.Equal(t, "TOOL", mp.EnvPrefix)
	assert.Equal(t, "prompt", mp.UpdatePolicy)
	assert.Equal(t, (6 * time.Hour).String(), mp.UpdateCheckInterval)
	assert.Equal(t, "direct", mp.MCP.Mode)
	assert.Equal(t, ManifestHelp{Type: "slack", SlackChannel: "#help", SlackTeam: "acme"}, mp.Help)
	assert.Equal(t, ManifestTelemetry{Endpoint: "https://t", OTelEndpoint: "https://o"}, mp.Telemetry)
	assert.Equal(t, ManifestBootstrap{
		AutoInitialise:    true,
		SkipConfigCheck:   []string{"init"},
		AuxiliaryCommands: []string{"version"},
	}, mp.Bootstrap)
	assert.True(t, mp.Signing.RequireChecksum)

	assert.Equal(t, []string{"defaults", "team", "flags"}, mp.Config.Layers)
	assert.Equal(t, "toml", mp.Config.Format)
	require.Len(t, mp.Config.Sources, 1)
	assert.Equal(t, "team", mp.Config.Sources[0].Name)
	assert.Equal(t, "http", mp.Config.Sources[0].Kind)
	require.NotNil(t, mp.Config.Sources[0].Required)
	assert.False(t, *mp.Config.Sources[0].Required)
	require.NotNil(t, mp.Config.Sources[0].Writable)
	assert.True(t, *mp.Config.Sources[0].Writable)
}

func TestApplyLiteralToolField_TeamsHelpAndEnabledPolicy(t *testing.T) {
	t.Parallel()

	mp := applyToolFields(t, map[string]string{
		"UpdatePolicy":        `props.UpdatePolicyEnabled`,
		"UpdateCheckInterval": `time.Duration(90)`,
		"Help":                `props.TeamsHelp{Channel: "General", Team: "Ops"}`,
	})

	assert.Equal(t, "enabled", mp.UpdatePolicy)
	assert.Equal(t, time.Duration(90).String(), mp.UpdateCheckInterval)
	assert.Equal(t, ManifestHelp{Type: "teams", TeamsChannel: "General", TeamsTeam: "Ops"}, mp.Help)
}

func TestApplyLiteralToolField_IgnoresShapesItDoesNotRender(t *testing.T) {
	t.Parallel()

	mp := applyToolFields(t, map[string]string{
		"EnvPrefix":    `prefix`,
		"UpdatePolicy": `policy`,
		"MCP":          `mcpConfig`,
		"Help":         `help`,
		"Telemetry":    `tel`,
		"Bootstrap":    `bs`,
		"Signing":      `sg`,
		"Config":       `cfg`,
	})

	assert.Equal(t, ManifestProperties{}, mp)

	mp = applyToolFields(t, map[string]string{
		"UpdatePolicy": `props.UpdatePolicyNever`,
		"MCP":          `props.MCPConfig{props.MCPDirect, Mode: props.MCPCompact}`,
		"Help":         `SlackHelp{Channel: "#x"}`,
		"Bootstrap":    `props.BootstrapPolicy{true, 1: false}`,
		"Signing":      `props.SigningConfig{true, RequireChecksum: true}`,
		"Config":       `props.ConfigSpec{true, 1: 2, Layers: layers, Sources: srcs}`,
	})

	assert.Empty(t, mp.UpdatePolicy)
	assert.Empty(t, mp.MCP.Mode)
	assert.Equal(t, ManifestHelp{}, mp.Help)
	assert.Equal(t, ManifestBootstrap{}, mp.Bootstrap)
	assert.False(t, mp.Signing.RequireChecksum)
	assert.Nil(t, mp.Config.Layers)
	assert.Nil(t, mp.Config.Sources)
}

func TestDurationFromExpr_RejectsWhatItCannotEvaluate(t *testing.T) {
	t.Parallel()

	for _, expr := range []string{
		`n * time.Hour`,
		`6 + time.Hour`,
		`6 * time.Millisecond`,
		`6 * hour`,
		`time.Duration(1, 2)`,
		`time.Duration(n)`,
		`"6h"`,
	} {
		t.Run(expr, func(t *testing.T) {
			t.Parallel()

			_, ok := durationFromExpr(parseExpr(t, expr))
			assert.False(t, ok)
		})
	}

	for expr, want := range map[string]time.Duration{
		`2 * time.Minute`: 2 * time.Minute,
		`3 * time.Second`: 3 * time.Second,
	} {
		got, ok := durationFromExpr(parseExpr(t, expr))
		require.True(t, ok, expr)
		assert.Equal(t, want, got)
	}
}

func TestLiteralValueHelpers(t *testing.T) {
	t.Parallel()

	_, ok := intLitValue(parseExpr(t, `"1"`))
	assert.False(t, ok)

	_, ok = intLitValue(parseExpr(t, `99999999999999999999`))
	assert.False(t, ok, "overflows int64")

	assert.False(t, boolPtrCallValue(parseExpr(t, `props.BoolPtr()`)))
	assert.False(t, boolPtrCallValue(parseExpr(t, `flag`)))

	assert.Nil(t, boolPtrFromCall(parseExpr(t, `flag`)))
	assert.Nil(t, boolPtrFromCall(parseExpr(t, `props.BoolPtr(maybe)`)))

	assert.Nil(t, stringSliceLitValue(parseExpr(t, `names`)))
	assert.Equal(t, []string{"a"}, stringSliceLitValue(parseExpr(t, `[]string{"a", other}`)))

	assert.Equal(t, []ManifestConfigSource{{}}, sourcesFromLiteral(parseExpr(t, `[]props.ConfigSource{src, {"positional", 1: 2}}`)), "non-literal entries are skipped, unkeyed fields ignored")
}
