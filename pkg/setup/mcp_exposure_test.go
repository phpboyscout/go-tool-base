package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptrBool(b bool) *bool { return &b }

func TestMCPExposureFromBool(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   *bool
		want MCPExposure
	}{
		{"nil is inherit", nil, MCPExposureInherit},
		{"true is exposed", ptrBool(true), MCPExposureExposed},
		{"false is excluded", ptrBool(false), MCPExposureExcluded},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, MCPExposureFromBool(tc.in))
		})
	}
}

func TestExcludeFromMCP_StampsExcluded(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "post"}
	got := ExcludeFromMCP(&Command{Command: raw})

	assert.Equal(t, mcpExposureValueExcluded, raw.Annotations[MCPExposureAnnotation])
	assert.Same(t, raw, got.Command, "helper returns the same command for chaining")
	assert.Equal(t, MCPExposureExcluded, MCPExposureOf(raw))
}

func TestIncludeInMCP_StampsExposed(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "status"}
	got := IncludeInMCP(&Command{Command: raw})

	assert.Equal(t, mcpExposureValueExposed, raw.Annotations[MCPExposureAnnotation])
	assert.Same(t, raw, got.Command)
	assert.Equal(t, MCPExposureExposed, MCPExposureOf(raw))
}

func TestExposureHelpers_InitialiseNilAnnotationMap(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "post"}
	require.Nil(t, raw.Annotations)

	ExcludeFromMCP(&Command{Command: raw})

	require.NotNil(t, raw.Annotations)
	assert.Equal(t, mcpExposureValueExcluded, raw.Annotations[MCPExposureAnnotation])
}

func TestExposureHelpers_PreserveExistingAnnotations(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "post", Annotations: map[string]string{FeatureAnnotation: "post"}}

	ExcludeFromMCP(&Command{Command: raw})

	assert.Equal(t, "post", raw.Annotations[FeatureAnnotation], "must not clobber other annotations")
	assert.Equal(t, mcpExposureValueExcluded, raw.Annotations[MCPExposureAnnotation])
}

func TestExposureHelpers_NilSafe(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, func() { ExcludeFromMCP(nil) })
	assert.NotPanics(t, func() { IncludeInMCP(nil) })
	assert.NotPanics(t, func() { ExcludeFromMCP(&Command{Command: nil}) })
}

func TestMCPExposureOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  func() *cobra.Command
		want MCPExposure
	}{
		{
			name: "exposed annotation",
			cmd:  func() *cobra.Command { return IncludeInMCP(&Command{Command: &cobra.Command{Use: "a"}}).Command },
			want: MCPExposureExposed,
		},
		{
			name: "excluded annotation",
			cmd:  func() *cobra.Command { return ExcludeFromMCP(&Command{Command: &cobra.Command{Use: "a"}}).Command },
			want: MCPExposureExcluded,
		},
		{
			name: "no annotation is inherit",
			cmd:  func() *cobra.Command { return &cobra.Command{Use: "a"} },
			want: MCPExposureInherit,
		},
		{
			name: "unrelated annotation is inherit",
			cmd: func() *cobra.Command {
				return &cobra.Command{Use: "a", Annotations: map[string]string{FeatureAnnotation: "a"}}
			},
			want: MCPExposureInherit,
		},
		{
			name: "nil command is inherit",
			cmd:  func() *cobra.Command { return nil },
			want: MCPExposureInherit,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, MCPExposureOf(tc.cmd()))
		})
	}
}

func TestIsExposedToMCP_SingleCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  func() *cobra.Command
		want bool
	}{
		{"default (no marker) is exposed", func() *cobra.Command { return &cobra.Command{Use: "a"} }, true},
		{"explicit exposed", func() *cobra.Command { return IncludeInMCP(&Command{Command: &cobra.Command{Use: "a"}}).Command }, true},
		{"explicit excluded", func() *cobra.Command { return ExcludeFromMCP(&Command{Command: &cobra.Command{Use: "a"}}).Command }, false},
		{"nil command defaults exposed", func() *cobra.Command { return nil }, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, IsExposedToMCP(tc.cmd()))
		})
	}
}

// TestIsExposedToMCP_SubtreeAndOverride builds the spec's canonical tree:
//
//	post              excluded
//	  post due        inherit  -> excluded
//	  post status     exposed  -> override
//	    post status watch inherit -> exposed (inherits the override)
//	      ... deep excluded re-gate
func TestIsExposedToMCP_SubtreeAndOverride(t *testing.T) {
	t.Parallel()

	post := ExcludeFromMCP(&Command{Command: &cobra.Command{Use: "post"}}).Command
	due := &cobra.Command{Use: "due"}
	status := IncludeInMCP(&Command{Command: &cobra.Command{Use: "status"}}).Command
	watch := &cobra.Command{Use: "watch"}
	deepGate := ExcludeFromMCP(&Command{Command: &cobra.Command{Use: "secret"}}).Command

	post.AddCommand(due, status)
	status.AddCommand(watch)
	watch.AddCommand(deepGate)

	assert.False(t, IsExposedToMCP(post), "explicitly excluded parent is withheld")
	assert.False(t, IsExposedToMCP(due), "child inherits excluded from parent")
	assert.True(t, IsExposedToMCP(status), "explicit exposed overrides excluded ancestor")
	assert.True(t, IsExposedToMCP(watch), "descendant inherits the exposed override")
	assert.False(t, IsExposedToMCP(deepGate), "a deeper explicit exclude re-gates the subtree")
}

func TestIsExposedToMCP_AllInheritChainDefaultsExposed(t *testing.T) {
	t.Parallel()

	root := &cobra.Command{Use: "root"}
	mid := &cobra.Command{Use: "mid"}
	leaf := &cobra.Command{Use: "leaf"}
	root.AddCommand(mid)
	mid.AddCommand(leaf)

	assert.True(t, IsExposedToMCP(leaf), "no explicit decision anywhere -> default exposed")
}
