package doctor

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0204 D10: the stack in the order it resolves, lowest first.
func TestCheckConfigStack_NoSources(t *testing.T) {
	t.Parallel()

	props := &p.Props{Config: testutil.FileStoreFromYAML(t, "{}\n"), Tool: p.Tool{Name: "mytool", EnvPrefix: "MYTOOL"}}

	result := checkConfigStack(context.Background(), props)
	assert.Equal(t, "Config stack", result.Name)
	assert.Equal(t, CheckPass, result.Status)
	assert.Equal(t, "5 layers, no config sources", result.Message)
	assert.Contains(t, result.Details, "1. defaults: embedded defaults")
	assert.Contains(t, result.Details, "4. env: variables under MYTOOL_")
	assert.Contains(t, result.Details, "5. flags: changed flags")
}

func sourcedStack(features []p.Feature) *p.Props {
	no := false

	return &p.Props{
		Tool: p.Tool{
			Name:     "mytool",
			Features: features,
			Config: p.ConfigSpec{
				Sources: []p.ConfigSource{{Name: "team", Kind: "consul"}, {Name: "spare", Kind: "file", Required: &no}, {Name: "down", Kind: "vault", Required: &no}},
				Layers:  []p.ConfigLayer{p.LayerDefaults, "team", "spare", "down", p.LayerFiles, p.LayerEnv, p.LayerFlags},
			},
		},
		SourceStatuses: []p.ConfigSourceStatus{
			{Slot: p.ConfigSource{Name: "team", Kind: "consul"}, State: p.ConfigSourceBuilt, Sensitive: true, Credential: "auth.env"}, //nolint:gosec // G101: names a rung, holds no secret
			{Slot: p.ConfigSource{Name: "spare", Kind: "file", Required: &no}, State: p.ConfigSourceUnconfigured},
			{Slot: p.ConfigSource{Name: "down", Kind: "vault", Required: &no}, State: p.ConfigSourceUnavailable, Err: "connection refused"},
		},
	}
}

func TestCheckConfigStack_ReportsEachSlot(t *testing.T) {
	t.Parallel()

	props := sourcedStack(p.SetFeatures(p.Enable(p.InitCmd)))
	props.Config = testutil.FileStoreFromYAML(t, "{}\n")

	result := checkConfigStack(context.Background(), props)
	assert.Equal(t, CheckWarn, result.Status, "an optional slot left out is a warning")
	assert.Equal(t, "7 layers, 3 config sources, 2 left out", result.Message)
	assert.Contains(t, result.Details, "2. team (consul): built, required, read-only, sensitive; credential from auth.env")
	assert.Contains(t, result.Details, "3. spare (file): not configured, optional; run `mytool init config spare`")
	assert.Contains(t, result.Details, "4. down (vault): unavailable, optional: connection refused")
}

func TestCheckConfigStack_WithoutInitPointsAtTheDefaults(t *testing.T) {
	t.Parallel()

	props := sourcedStack(p.SetFeatures(p.Disable(p.InitCmd)))
	props.Config = testutil.FileStoreFromYAML(t, "{}\n")

	result := checkConfigStack(context.Background(), props)
	assert.Contains(t, result.Details, "3. spare (file): not configured, optional; set config.sources.spare in the tool's defaults or the user's config file")
	assert.NotContains(t, result.Details, "init config")
}

func TestCheckConfigStack_AllBuiltPasses(t *testing.T) {
	t.Parallel()

	props := sourcedStack(p.SetFeatures(p.Enable(p.InitCmd)))
	props.Config = testutil.FileStoreFromYAML(t, "{}\n")
	props.Tool.Config.Sources = props.Tool.Config.Sources[:1]
	props.Tool.Config.Layers = []p.ConfigLayer{p.LayerDefaults, "team", p.LayerFiles, p.LayerEnv, p.LayerFlags}
	props.SourceStatuses = props.SourceStatuses[:1]

	result := checkConfigStack(context.Background(), props)
	assert.Equal(t, CheckPass, result.Status)
	assert.Equal(t, "5 layers, 1 config source", result.Message)
}

// Every line of a check's details sits under it, not just the first; the
// Config stack check's details are one line per layer.
func TestPrintReport_IndentsEveryDetailLine(t *testing.T) {
	t.Parallel()

	var out strings.Builder

	PrintReport(&out, &DoctorReport{Tool: "mytool", Checks: []CheckResult{
		{Name: "Config stack", Status: CheckPass, Message: "2 layers", Details: "1. defaults\n2. flags"},
	}})

	assert.Contains(t, out.String(), "       1. defaults\n       2. flags\n")
}
