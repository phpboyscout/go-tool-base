package transportcfg_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/internal/transportcfg"
)

// demoConfig is a stand-in for a go/transit resilience config: a couple of
// numeric fields resolved from a config subsection.
type demoConfig struct {
	Rate  int `mapstructure:"rate"`
	Burst int `mapstructure:"burst"`
}

func demoDefaults() demoConfig { return demoConfig{Rate: 10, Burst: 20} }

type demoOverrides struct{ Rate, Burst bool }

// demoMerge overlays only the fields the caller marked as set, mirroring the
// transit MergeXConfig contract.
func demoMerge(base, overlay demoConfig, o demoOverrides) demoConfig {
	if o.Rate {
		base.Rate = overlay.Rate
	}

	if o.Burst {
		base.Burst = overlay.Burst
	}

	return base
}

func demoOverridesFrom(base string) func(isSet func(string) bool) demoOverrides {
	return func(isSet func(string) bool) demoOverrides {
		return demoOverrides{
			Rate:  isSet(base + ".rate"),
			Burst: isSet(base + ".burst"),
		}
	}
}

func TestResolveSection_NilReaderYieldsDefaults(t *testing.T) {
	t.Parallel()

	got := transportcfg.ResolveSection(nil, "svc.limit", demoDefaults, demoMerge, demoOverridesFrom("svc.limit"))
	assert.Equal(t, demoDefaults(), got)
}

func TestResolveSection_AbsentSectionYieldsDefaults(t *testing.T) {
	t.Parallel()

	cfg := testutil.ViewFromYAML(t, "other:\n  key: value\n")

	got := transportcfg.ResolveSection(cfg, "svc.limit", demoDefaults, demoMerge, demoOverridesFrom("svc.limit"))
	assert.Equal(t, demoDefaults(), got)
}

func TestResolveSection_MergesOnlySetFieldsOverDefaults(t *testing.T) {
	t.Parallel()

	// Only "rate" is set, so "burst" must keep its default.
	cfg := testutil.ViewFromYAML(t, "svc:\n  limit:\n    rate: 99\n")

	got := transportcfg.ResolveSection(cfg, "svc.limit", demoDefaults, demoMerge, demoOverridesFrom("svc.limit"))
	assert.Equal(t, demoConfig{Rate: 99, Burst: 20}, got)
}
