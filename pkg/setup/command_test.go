package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	childFeature  = props.FeatureID("child")
	parentFeature = props.FeatureID("parent")
	grandFeature  = props.FeatureID("grand")
)

func TestSkipConfigCheck_AnnotatesAndReads(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "studio"}
	assert.False(t, SkipsConfigCheck(cmd), "unannotated command must not skip")

	got := SkipConfigCheck(cmd)
	assert.Same(t, cmd, got, "SkipConfigCheck returns the same command for chaining")
	assert.True(t, SkipsConfigCheck(cmd), "annotated command must skip")
	assert.Equal(t, "true", cmd.Annotations[SkipConfigCheckAnnotation])
}

func TestSkipConfigCheck_NilSafe(t *testing.T) {
	t.Parallel()

	assert.Nil(t, SkipConfigCheck(nil))
	assert.False(t, SkipsConfigCheck(nil))
}

func TestSkipsConfigCheck_UnrelatedAnnotations(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "studio", Annotations: map[string]string{"other": "x"}}
	assert.False(t, SkipsConfigCheck(cmd),
		"a command with unrelated annotations must not skip")
}

func TestWrap_AssignsFeatureAndEmbedsCommand(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "child"}
	c := Wrap(childFeature, raw)

	assert.Equal(t, childFeature, c.Feature)
	assert.Same(t, raw, c.Command, "Wrap must embed the *cobra.Command unchanged")
	// Method delegation via the embedded pointer still works.
	assert.Equal(t, "child", c.Use)
}

func TestWrap_StampsFeatureAnnotation(t *testing.T) {
	t.Parallel()

	raw := &cobra.Command{Use: "init"}
	_ = Wrap(props.InitCmd, raw)

	assert.Equal(t, string(props.InitCmd), raw.Annotations[FeatureAnnotation],
		"Wrap must stamp the feature onto the raw command's annotations")
}

func TestFeatureOf_IdentifiesByAnnotationNotUseString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmd  func() *cobra.Command
		want props.FeatureID
	}{
		{
			name: "wrapped init command is identified by feature",
			cmd:  func() *cobra.Command { return Wrap(props.InitCmd, &cobra.Command{Use: "init"}).Command },
			want: props.InitCmd,
		},
		{
			name: "unrelated command literally named init is NOT the init feature",
			// The fragile cmd.Use == "init" check would misfire here; FeatureOf
			// must not, because this command carries no init annotation.
			cmd:  func() *cobra.Command { return &cobra.Command{Use: "init"} },
			want: "",
		},
		{
			name: "init command with an arg suffix in Use is still identified",
			cmd:  func() *cobra.Command { return Wrap(props.InitCmd, &cobra.Command{Use: "init [dir]"}).Command },
			want: props.InitCmd,
		},
		{
			name: "nil command yields the empty feature",
			cmd:  func() *cobra.Command { return nil },
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, FeatureOf(tc.cmd()))
		})
	}
}

// chainFor resolves a registry with every named feature enabled into a chain
// with no built-ins, so only the contributions show in the order.
func chainFor(t *testing.T, r features.Registry, enabled ...props.FeatureID) *MiddlewareChain {
	t.Helper()

	states := make([]features.State, 0, len(enabled))
	for _, id := range enabled {
		require.NoError(t, r.Declare(props.FeatureDescriptor{ID: id, ConstName: "X", ConstPackage: "example.com/x", Kind: "test"}))
		states = append(states, features.State{ID: id, Enabled: true})
	}

	set, err := features.Resolve(r.Snapshot(), states)
	require.NoError(t, err)

	return NewMiddlewareChain(nil, set)
}

func TestRegister_WiresChildOwnFeatureMiddleware(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	r.Contribute(childFeature, SlotMiddleware, testMiddleware("child-mw", &order))
	// Middleware registered against the parent's feature must NOT run on
	// the child: Register wires each child with its OWN feature, not the
	// parent's. This is the regression we fixed by construction.
	r.Contribute(parentFeature, SlotMiddleware, testMiddleware("parent-mw", &order))

	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	parent.UseChain(chainFor(t, r, childFeature, parentFeature))
	child := Wrap(childFeature, &cobra.Command{
		Use: "child",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "child-runE")

			return nil
		},
	})

	parent.Register(child)

	require.NoError(t, child.RunE(child.Command, nil))
	assert.Equal(t,
		[]string{"child-mw:before", "child-runE", "child-mw:after"},
		order,
		"child must be wrapped with its own feature's middleware only")
}

// TestRegister_WrapsASubtreeBuiltBeforeItJoined is the per-root chain's
// wrinkle (spec 0199 D3): a feature package builds its subtree bottom-up
// before the root registers it, so the grandchild is wrapped when the child
// joins the root, under the grandchild's own feature, and exactly once.
func TestRegister_WrapsASubtreeBuiltBeforeItJoined(t *testing.T) {
	t.Parallel()

	var order []string

	r := features.NewRegistry()
	r.Contribute(childFeature, SlotMiddleware, testMiddleware("child-mw", &order))
	r.Contribute(grandFeature, SlotMiddleware, testMiddleware("grand-mw", &order))

	grandchild := Wrap(grandFeature, &cobra.Command{
		Use: "grand",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "grand-runE")

			return nil
		},
	})
	child := Wrap(childFeature, &cobra.Command{Use: "child"})

	// No chain yet: the subtree is assembled before it has a root.
	child.Register(grandchild)
	require.NoError(t, grandchild.RunE(grandchild.Command, nil))
	assert.Equal(t, []string{"grand-runE"}, order, "unwrapped until a root with a chain takes the subtree")

	order = nil

	root := Wrap(parentFeature, &cobra.Command{Use: "root"})
	root.UseChain(chainFor(t, r, childFeature, grandFeature, parentFeature))
	root.Register(child)

	require.NoError(t, grandchild.RunE(grandchild.Command, nil))
	assert.Equal(t,
		[]string{"grand-mw:before", "grand-runE", "grand-mw:after"},
		order,
		"grandchild RunE must be wrapped exactly once, with the grandchild's feature")

	// Registering the child again must not wrap the grandchild a second time.
	order = nil
	root.Register(child)
	require.NoError(t, grandchild.RunE(grandchild.Command, nil))
	assert.Equal(t, []string{"grand-mw:before", "grand-runE", "grand-mw:after"}, order)
}

func TestRegister_SkipsNilCommandEmbedded(t *testing.T) {
	t.Parallel()

	// Defensive: a *Command with no embedded cobra.Command shouldn't
	// crash Register; it just gets skipped.
	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	parent.Register(&Command{Feature: childFeature, Command: nil})

	assert.Empty(t, parent.Commands(), "nil embedded cobra.Command must be a no-op")
}
