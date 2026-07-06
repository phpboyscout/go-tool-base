package setup

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

const (
	childFeature  = props.FeatureCmd("child")
	parentFeature = props.FeatureCmd("parent")
	grandFeature  = props.FeatureCmd("grand")
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
		want props.FeatureCmd
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

func TestRegister_WiresChildOwnFeatureMiddleware(t *testing.T) {
	resetRegistry(t)

	var order []string

	RegisterMiddleware(childFeature, testMiddleware("child-mw", &order))
	// Middleware registered against the parent's feature must NOT run on
	// the child — Register wires each child with its OWN feature, not the
	// parent's. This is the regression we fixed by construction.
	RegisterMiddleware(parentFeature, testMiddleware("parent-mw", &order))

	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
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
		"child must run only its own feature's middleware")
}

func TestRegister_AddsChildToCobraTree(t *testing.T) {
	resetRegistry(t)

	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	child := Wrap(childFeature, &cobra.Command{Use: "child"})

	parent.Register(child)

	cmds := parent.Commands()
	require.Len(t, cmds, 1)
	assert.Equal(t, "child", cmds[0].Use)
	assert.Same(t, child.Command, cmds[0])
}

func TestRegister_MultipleChildren(t *testing.T) {
	resetRegistry(t)

	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	a := Wrap(childFeature, &cobra.Command{Use: "a"})
	b := Wrap(grandFeature, &cobra.Command{Use: "b"})

	parent.Register(a, b)

	assert.Len(t, parent.Commands(), 2)
}

func TestRegister_NilRunE_StillAttaches(t *testing.T) {
	resetRegistry(t)

	// A command-group with no RunE (just children) must register cleanly.
	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	group := Wrap(childFeature, &cobra.Command{Use: "group"}) // RunE == nil

	parent.Register(group)

	assert.Len(t, parent.Commands(), 1)
	assert.Nil(t, group.RunE, "no RunE -> nothing to wrap, no change")
}

func TestRegister_DoesNotRewrapDescendants(t *testing.T) {
	resetRegistry(t)

	var order []string

	RegisterMiddleware(childFeature, testMiddleware("child-mw", &order))
	RegisterMiddleware(grandFeature, testMiddleware("grand-mw", &order))

	// Build the tree bottom-up: the grandchild is registered to its
	// parent (the child) first, then the child is registered to the root.
	// After the root's Register, the grandchild's RunE must still carry
	// only its own (grand) middleware — not the child's — proving
	// Register does not re-wrap descendants.
	grandchild := Wrap(grandFeature, &cobra.Command{
		Use: "grand",
		RunE: func(_ *cobra.Command, _ []string) error {
			order = append(order, "grand-runE")

			return nil
		},
	})
	child := Wrap(childFeature, &cobra.Command{Use: "child"})

	child.Register(grandchild)

	root := Wrap(parentFeature, &cobra.Command{Use: "root"})
	root.Register(child)

	require.NoError(t, grandchild.RunE(grandchild.Command, nil))
	assert.Equal(t,
		[]string{"grand-mw:before", "grand-runE", "grand-mw:after"},
		order,
		"grandchild RunE must be wrapped exactly once, with the grandchild's feature")
}

func TestRegister_SkipsNilCommandEmbedded(t *testing.T) {
	resetRegistry(t)

	// Defensive: a *Command with no embedded cobra.Command shouldn't
	// crash Register; it just gets skipped.
	parent := Wrap(parentFeature, &cobra.Command{Use: "parent"})
	parent.Register(&Command{Feature: childFeature, Command: nil})

	assert.Empty(t, parent.Commands(), "nil embedded cobra.Command must be a no-op")
}
