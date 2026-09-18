package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/features"
)

// enabledFor resolves a tool's features against the default snapshot, the
// way New does, so a test asks the Set rather than the removed Tool.IsEnabled.
func enabledFor(t *testing.T, tool Tool) features.Set {
	t.Helper()

	set, err := features.Resolve(features.Default().Snapshot(), StatesOf(tool.Features))
	require.NoError(t, err)

	return set
}

func TestSetFeatures_DefaultsPlusOverrides(t *testing.T) {
	t.Parallel()

	set := enabledFor(t, Tool{Features: SetFeatures(Disable(UpdateCmd), Enable(AiCmd))})

	assert.False(t, set.Enabled(UpdateCmd))
	assert.True(t, set.Enabled(InitCmd))
	assert.False(t, set.Enabled(McpCmd), "mcp is declared by pkg/mcp's link, not here")
	assert.True(t, set.Enabled(DocsCmd))
	assert.True(t, set.Enabled(DoctorCmd))
	assert.True(t, set.Enabled(AiCmd))
	// ManCmd is default-off: absent from DefaultFeatures, opt-in only.
	assert.False(t, set.Enabled(ManCmd))
}

func TestManCmd_OptIn(t *testing.T) {
	t.Parallel()

	off := enabledFor(t, Tool{Features: SetFeatures()})
	assert.False(t, off.Enabled(ManCmd), "man must be disabled by default")

	on := enabledFor(t, Tool{Features: SetFeatures(Enable(ManCmd))})
	assert.True(t, on.Enabled(ManCmd), "man must enable when opted in")
}

func TestEnable_NoDuplicates(t *testing.T) {
	t.Parallel()

	f1 := Enable(UpdateCmd)(nil)
	f2 := Enable(UpdateCmd)(f1)
	count := 0
	for _, f := range f2 {
		if f.ID == UpdateCmd {
			count++
		}
	}
	assert.Equal(t, 1, count, "Enable should not duplicate entries")
}

func TestDisable_RemovesAndDisables(t *testing.T) {
	t.Parallel()

	features := []Feature{{ID: UpdateCmd, Enabled: true}}
	result := Disable(UpdateCmd)(features)

	for _, f := range result {
		if f.ID == UpdateCmd {
			assert.False(t, f.Enabled)
		}
	}
}

func TestDisable_NoDuplicates(t *testing.T) {
	t.Parallel()

	f1 := Disable(AiCmd)(nil)
	f2 := Disable(AiCmd)(f1)
	count := 0
	for _, f := range f2 {
		if f.ID == AiCmd {
			count++
		}
	}
	assert.Equal(t, 1, count, "Disable should not duplicate entries")
}

func TestIsDefaultEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		cmd     FeatureID
		enabled bool
	}{
		{UpdateCmd, true},
		{InitCmd, true},
		{McpCmd, false}, // a link: pkg/mcp declares it, this package does not
		{DocsCmd, true},
		{DoctorCmd, true},
		{AiCmd, false},
		{ConfigCmd, false},
		{ManCmd, false},
		{FeatureID("custom"), false},
	}
	for _, tt := range tests {
		t.Run(string(tt.cmd), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.enabled, enabledFor(t, Tool{}).Enabled(tt.cmd))
		})
	}
}

func TestResolvedSet_FromSlice(t *testing.T) {
	t.Parallel()

	set := enabledFor(t, Tool{Features: []Feature{{ID: UpdateCmd, Enabled: false}}})
	assert.False(t, set.Enabled(UpdateCmd))
	assert.True(t, set.Enabled(InitCmd)) // falls back to default
}

func TestResolvedSet_Fallback(t *testing.T) {
	t.Parallel()

	set := enabledFor(t, Tool{}) // no features set: all fall back to defaults
	assert.True(t, set.Enabled(UpdateCmd))
	assert.False(t, set.Enabled(AiCmd))
}

func TestGetReleaseSource(t *testing.T) {
	t.Parallel()

	tool := Tool{
		ReleaseSource: ReleaseSource{
			Type:  "github",
			Owner: "myorg",
			Repo:  "mytool",
		},
	}

	srcType, owner, repo := tool.GetReleaseSource()
	assert.Equal(t, "github", srcType)
	assert.Equal(t, "myorg", owner)
	assert.Equal(t, "mytool", repo)
}
