package props

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestFeature_WireKeyIsCmd pins the serialised key for Feature.ID to "cmd".
//
// The Go field was renamed Cmd -> ID when FeatureCmd became FeatureID, but the
// struct tag deliberately did not move: Tool.Features is persisted in generator
// manifests and tool configs, so changing the wire key would stop older
// manifests loading with no compile-time signal. This test is the guard on that
// decision — if someone "tidies" the tag to match the field, it fails here
// rather than in a downstream's regenerate run.
func TestFeature_WireKeyIsCmd(t *testing.T) {
	t.Parallel()

	t.Run("json round-trip uses cmd", func(t *testing.T) {
		t.Parallel()

		out, err := json.Marshal(Feature{ID: UpdateCmd, Enabled: true})
		require.NoError(t, err)
		assert.JSONEq(t, `{"cmd":"update","enabled":true}`, string(out))
	})

	t.Run("json written before the rename still loads", func(t *testing.T) {
		t.Parallel()

		var f Feature
		require.NoError(t, json.Unmarshal([]byte(`{"cmd":"update","enabled":true}`), &f))
		assert.Equal(t, UpdateCmd, f.ID)
		assert.True(t, f.Enabled)
	})

	t.Run("yaml written before the rename still loads", func(t *testing.T) {
		t.Parallel()

		var tool Tool
		require.NoError(t, yaml.Unmarshal([]byte("features:\n  - cmd: update\n    enabled: false\n"), &tool))
		require.Len(t, tool.Features, 1)
		assert.Equal(t, UpdateCmd, tool.Features[0].ID)
		assert.False(t, tool.Features[0].Enabled)
	})

	t.Run("an id key is not accepted, proving the tag is load-bearing", func(t *testing.T) {
		t.Parallel()

		var f Feature
		require.NoError(t, json.Unmarshal([]byte(`{"id":"update","enabled":true}`), &f))
		assert.Empty(t, f.ID, "unmarshalling an \"id\" key must not populate ID; the wire key is \"cmd\"")
	})
}
