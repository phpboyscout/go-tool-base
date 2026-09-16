package features_test

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
)

// desc is a test descriptor: the module's Descriptor is an interface so GTB can
// carry codegen facts on its own type.
type desc struct {
	id      features.ID
	kind    features.Kind
	on      bool
	dynamic bool
	rank    int
}

func (d desc) FeatureID() features.ID     { return d.id }
func (d desc) FeatureKind() features.Kind { return d.kind }
func (d desc) DefaultOn() bool            { return d.on }
func (d desc) IsDynamic() bool            { return d.dynamic }

// ranked is desc with a Rank, the optional ordering hook.
type ranked struct{ desc }

func (r ranked) Rank() (int, bool) { return r.rank, true }

const (
	builtin features.Kind = "builtin"
	forge   features.Kind = "forge"
	slotA   features.Slot = "a"
)

func registry(t *testing.T, ds ...features.Descriptor) features.Registry {
	t.Helper()

	r := features.NewRegistry()
	for _, d := range ds {
		require.NoError(t, r.Declare(d))
	}

	return r
}

// TestNoGTBImports is the extraction guard (spec 0199 D2, D9): the core must
// leave for go/features as a move, so nothing here may import GTB or cobra.
func TestNoGTBImports(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	require.NoError(t, err)

	fset := token.NewFileSet()

	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}

		f, perr := parser.ParseFile(fset, filepath.Join(".", e.Name()), nil, parser.ImportsOnly)
		require.NoError(t, perr)

		for _, imp := range f.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			assert.False(t, strings.HasPrefix(path, "gitlab.com/phpboyscout/go-tool-base"), "%s imports GTB: %s", e.Name(), path)
			assert.False(t, strings.Contains(path, "cobra") || strings.Contains(path, "pflag"), "%s imports a CLI library: %s", e.Name(), path)
		}
	}
}

func TestDeclare_ValidationAndDuplicates(t *testing.T) {
	t.Parallel()

	r := features.NewRegistry()

	require.NoError(t, r.Declare(desc{id: "update", kind: builtin, on: true}))

	err := r.Declare(desc{id: "update", kind: builtin})
	require.ErrorIs(t, err, features.ErrDuplicateFeature)

	err = r.Declare(desc{id: "", kind: builtin})
	require.ErrorIs(t, err, features.ErrInvalidDescriptor)

	err = r.Declare(desc{id: "x", kind: ""})
	require.ErrorIs(t, err, features.ErrInvalidDescriptor)

	err = r.Declare(nil)
	require.ErrorIs(t, err, features.ErrInvalidDescriptor)
}

// TestSnapshot_IsImmutable is D1: a snapshot does not see a later declaration,
// a later snapshot does, and neither call panics.
func TestSnapshot_IsImmutable(t *testing.T) {
	t.Parallel()

	r := registry(t, desc{id: "update", kind: builtin, on: true})
	first := r.Snapshot()

	require.NoError(t, r.Declare(desc{id: "gitlab", kind: forge}))
	r.Contribute("update", slotA, "late")

	assert.Len(t, first.Descriptors(), 1)
	assert.Empty(t, first.Contributions("update", slotA))

	second := r.Snapshot()
	assert.Len(t, second.Descriptors(), 2)
	assert.Equal(t, []any{"late"}, second.Contributions("update", slotA))
}

// TestSnapshot_Order pins the 0184 total order: ranked descriptors by rank,
// then everything else by kind then ID, never by declaration sequence.
func TestSnapshot_Order(t *testing.T) {
	t.Parallel()

	r := registry(t,
		desc{id: "gitlab", kind: forge},
		ranked{desc{id: "mcp", kind: builtin, rank: 2}},
		desc{id: "bitbucket", kind: forge},
		ranked{desc{id: "update", kind: builtin, rank: 0}},
		desc{id: "custom", kind: "plugin"},
		ranked{desc{id: "init", kind: builtin, rank: 1}},
	)

	var ids []features.ID
	for _, d := range r.Snapshot().Descriptors() {
		ids = append(ids, d.FeatureID())
	}

	assert.Equal(t, []features.ID{"update", "init", "mcp", "bitbucket", "gitlab", "custom"}, ids)

	forges := r.Snapshot().OfKind(forge)
	assert.Len(t, forges, 2)
	assert.Equal(t, features.ID("bitbucket"), forges[0].FeatureID())

	d, ok := r.Snapshot().Lookup("mcp")
	require.True(t, ok)
	assert.Equal(t, builtin, d.FeatureKind())

	_, ok = r.Snapshot().Lookup("nope")
	assert.False(t, ok)
}

func TestContribute_UndeclaredIsRecordedAndGlobalIsAllowed(t *testing.T) {
	t.Parallel()

	r := registry(t, desc{id: "update", kind: builtin, on: true})
	r.Contribute(features.Global, slotA, "everyone")
	r.Contribute("update", slotA, 1)
	r.Contribute("update", slotA, 2)

	s := r.Snapshot()
	assert.Equal(t, []any{1, 2}, s.Contributions("update", slotA), "contributions keep registration order")
	assert.Equal(t, []any{"everyone"}, s.Contributions(features.Global, slotA))
	assert.Empty(t, s.Contributions("update", "other"))
	assert.Equal(t, []features.ID{features.Global, "update"}, s.Contributed())
}

// TestResolve is OQ2: defaults, then states in order; enabling an unknown ID is
// an error, disabling one is ignored and listed.
func TestResolve(t *testing.T) {
	t.Parallel()

	r := registry(t,
		desc{id: "update", kind: builtin, on: true},
		desc{id: "ai", kind: builtin},
		desc{id: "gitlab", kind: forge},
	)
	snap := r.Snapshot()

	set, err := features.Resolve(snap, []features.State{
		{ID: "ai", Enabled: true},
		{ID: "update", Enabled: false},
		{ID: "update", Enabled: true}, // later state wins
		{ID: "ghost", Enabled: false},
	})
	require.NoError(t, err)

	assert.True(t, set.Enabled("update"))
	assert.True(t, set.Enabled("ai"))
	assert.False(t, set.Enabled("gitlab"), "a plugin is off unless the tool says so")
	assert.False(t, set.Enabled("ghost"))
	assert.Equal(t, []features.State{{ID: "ghost", Enabled: false}}, set.Ignored())

	var enabled []features.ID
	for _, d := range set.EnabledDescriptors() {
		enabled = append(enabled, d.FeatureID())
	}
	assert.Equal(t, []features.ID{"ai", "update"}, enabled, "snapshot order, filtered")
	assert.Len(t, set.Descriptors(), 3)

	_, err = features.Resolve(snap, []features.State{{ID: "ghost", Enabled: true}})
	require.ErrorIs(t, err, features.ErrUnknownFeature)
}

func TestSet_ContributionsAreGatedAndTyped(t *testing.T) {
	t.Parallel()

	r := registry(t,
		desc{id: "update", kind: builtin, on: true},
		desc{id: "ai", kind: builtin},
	)
	r.Contribute("update", slotA, "u")
	r.Contribute("ai", slotA, "a")
	r.Contribute("update", slotA, 7)

	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	assert.Equal(t, []any{"u", 7}, set.Contributions("update", slotA))
	assert.Empty(t, set.Contributions("ai", slotA), "a disabled feature contributes nothing")

	strs, err := features.ContributionsOf[string](set, "update", slotA)
	require.Error(t, err, "7 is not a string")
	assert.Contains(t, err.Error(), "update")
	assert.Contains(t, err.Error(), string(slotA))
	assert.Equal(t, []string{"u"}, strs, "the well-typed values are still returned")
}

func TestSet_EvaluateIsStatic(t *testing.T) {
	t.Parallel()

	r := registry(t, desc{id: "update", kind: builtin, on: true})
	set, err := features.Resolve(r.Snapshot(), nil)
	require.NoError(t, err)

	dec, err := set.Evaluate(context.Background(), "update", features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonStatic}, dec)

	dec, err = set.Evaluate(context.Background(), "nope", features.EvalContext{})
	require.ErrorIs(t, err, features.ErrUnknownFeature)
	assert.Equal(t, features.ReasonError, dec.Reason)
}

func TestMutators(t *testing.T) {
	t.Parallel()

	states := features.Apply(nil, features.Enable("a"), features.Disable("a"), features.Enable("b"))
	assert.Equal(t, []features.State{{ID: "a", Enabled: false}, {ID: "b", Enabled: true}}, states)
}

func TestDefault_IsARegistry(t *testing.T) {
	t.Parallel()

	// The default registry is the init-time target; a test never writes to it,
	// only checks it answers as a Registry.
	assert.NotNil(t, features.Default().Snapshot())
}
