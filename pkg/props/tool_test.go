package props

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSetFeatures_DefaultsPlusOverrides(t *testing.T) {
	t.Parallel()

	features := SetFeatures(Disable(UpdateCmd), Enable(AiCmd))
	tool := Tool{Features: features}

	assert.False(t, tool.IsEnabled(UpdateCmd))
	assert.True(t, tool.IsEnabled(InitCmd))
	assert.True(t, tool.IsEnabled(McpCmd))
	assert.True(t, tool.IsEnabled(DocsCmd))
	assert.True(t, tool.IsEnabled(DoctorCmd))
	assert.True(t, tool.IsEnabled(AiCmd))
	// ManCmd is default-off: absent from DefaultFeatures, opt-in only.
	assert.False(t, tool.IsEnabled(ManCmd))
}

func TestManCmd_OptIn(t *testing.T) {
	t.Parallel()

	off := Tool{Features: SetFeatures()}
	assert.False(t, off.IsEnabled(ManCmd), "man must be disabled by default")

	on := Tool{Features: SetFeatures(Enable(ManCmd))}
	assert.True(t, on.IsEnabled(ManCmd), "man must enable when opted in")
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
		{McpCmd, true},
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
			assert.Equal(t, tt.enabled, isDefaultEnabled(tt.cmd))
		})
	}
}

func TestIsEnabled_FromSlice(t *testing.T) {
	t.Parallel()

	tool := Tool{Features: []Feature{{ID: UpdateCmd, Enabled: false}}}
	assert.False(t, tool.IsEnabled(UpdateCmd))
	assert.True(t, tool.IsEnabled(InitCmd)) // falls back to default
}

func TestIsEnabled_Fallback(t *testing.T) {
	t.Parallel()

	tool := Tool{} // no features set — all fall back to defaults
	assert.True(t, tool.IsEnabled(UpdateCmd))
	assert.False(t, tool.IsEnabled(AiCmd))
}

func TestIsDisabled(t *testing.T) {
	t.Parallel()

	tool := Tool{}
	assert.False(t, tool.IsDisabled(UpdateCmd))
	assert.True(t, tool.IsDisabled(AiCmd))
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
