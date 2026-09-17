package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestProps_Accessors exercises every narrow-provider accessor on *Props,
// confirming each returns the exact value stored on the container.
func TestProps_Accessors(t *testing.T) {
	t.Parallel()

	p, log, cfg, assets, memFS, ver, eh, tool, col := newTestProps(t)

	assert.Equal(t, log, p.GetLogger())
	assert.Equal(t, cfg, p.GetConfig())
	assert.Equal(t, assets, p.GetAssets())
	assert.Equal(t, memFS, p.GetFS())
	assert.Equal(t, ver, p.GetVersion())
	assert.Equal(t, eh, p.GetErrorHandler())
	assert.Equal(t, tool, p.GetTool())
	assert.Equal(t, col, p.GetCollector())
}

// TestProps_SatisfiesProviders confirms a *Props value is usable through each
// narrow provider interface (compile-time checks already exist; this exercises
// the dispatch through the interface at runtime).
func TestProps_SatisfiesProviders(t *testing.T) {
	t.Parallel()

	p, _, _, _, _, _, _, _, _ := newTestProps(t)

	var (
		lp  LoggerProvider       = p
		cp  ConfigProvider       = p
		ap  AssetProvider        = p
		tmp ToolMetadataProvider = p
	)

	assert.NotNil(t, lp.GetLogger())
	assert.NotNil(t, cp.GetConfig())
	assert.NotNil(t, ap.GetAssets())
	assert.Equal(t, "demo", tmp.GetTool().Name)

	// The remaining getters no longer have a narrow interface (pruned pre-1.0),
	// but the methods stay on *Props for direct use.
	assert.NotNil(t, p.GetFS())
	assert.Equal(t, "v1.2.3", p.GetVersion().GetVersion())
	assert.NotNil(t, p.GetErrorHandler())
	assert.NotNil(t, p.GetCollector())
}
