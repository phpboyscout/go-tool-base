package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPHints_AnnotateWritesOphisKeysAdditively(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "post", Annotations: map[string]string{FeatureAnnotation: "mcp", MCPExposureAnnotation: mcpExposureValueExposed}}

	got := AnnotateMCP(&Command{Command: raw}, MCPHints{Title: "Post it", ReadOnly: new(false), OpenWorld: new(true)})

	assert.Same(t, raw, got.Command)
	assert.Equal(t, map[string]string{
		FeatureAnnotation:     "mcp",
		MCPExposureAnnotation: mcpExposureValueExposed,
		"title":               "Post it",
		"readOnlyHint":        "false",
		"openWorldHint":       "true",
	}, raw.Annotations, "existing keys survive, nil hints write nothing")
}

func TestMCPHints_AnnotateInitialisesNilMap(t *testing.T) {
	t.Parallel()

	raw := MCPReadOnly().annotate(&cobra.Command{Use: "get"})

	require.NotNil(t, raw.Annotations)
	assert.Equal(t, "true", raw.Annotations[MCPReadOnlyAnnotation])
	assert.Equal(t, "false", raw.Annotations[MCPDestructiveAnnotation])
	assert.Equal(t, "true", raw.Annotations[MCPIdempotentAnnotation])
	assert.Equal(t, "false", raw.Annotations[MCPOpenWorldAnnotation])
	assert.NotContains(t, raw.Annotations, MCPTitleAnnotation)
}

func TestMCPHintsOf_RoundTripsAndIgnoresGarbage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   MCPHints
	}{
		{"zero", MCPHints{}},
		{"read-only", MCPReadOnly()},
		{"local write", MCPLocalWrite()},
		{"open world", MCPOpenWorld()},
		{"destructive", MCPDestructive()},
		{"title only", MCPHints{Title: "Generate images"}},
		{"partial", MCPHints{Idempotent: new(false)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cmd := tc.in.annotate(&cobra.Command{Use: "x"})
			assert.Equal(t, tc.in, MCPHintsOf(cmd))
			assert.Equal(t, tc.in.IsZero(), MCPHintsOf(cmd).IsZero())
		})
	}

	garbage := &cobra.Command{Use: "x", Annotations: map[string]string{MCPReadOnlyAnnotation: "yes"}}
	assert.Nil(t, MCPHintsOf(garbage).ReadOnly, "an unparseable value is unset, not guessed")
}

func TestMCPHints_NilSafe(t *testing.T) {
	t.Parallel()

	assert.Nil(t, AnnotateMCP(nil, MCPReadOnly()))
	assert.Nil(t, MCPReadOnly().annotate(nil))
	assert.True(t, MCPHintsOf(nil).IsZero())
	assert.True(t, MCPHintsOf(&cobra.Command{}).IsZero())
}

func TestMCPHints_DoNotInherit(t *testing.T) {
	t.Parallel()

	parent := MCPReadOnly().annotate(&cobra.Command{Use: "config"})
	child := &cobra.Command{Use: "get"}
	parent.AddCommand(child)

	assert.True(t, MCPHintsOf(child).IsZero(), "a hint describes one command only")
}
