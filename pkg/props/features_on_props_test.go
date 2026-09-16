package props

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

// ownRegistry declares the built-ins plus one plugin on a registry of the
// test's own (spec 0199 D5).
func ownRegistry(t *testing.T, plugin FeatureID) features.Registry {
	t.Helper()

	r := features.NewRegistry()
	for _, d := range DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, r.Declare(d))
	}

	require.NoError(t, r.Declare(FeatureDescriptor{ID: plugin, ConstName: "Plugin", ConstPackage: "example.com/plugin", Kind: "plugin", Dynamic: true}))

	return r
}

func TestNew_ResolvesFeaturesFromTheSnapshotGiven(t *testing.T) {
	t.Parallel()

	r := ownRegistry(t, "plugin")

	p, err := New(Tool{Name: "t", Features: SetFeatures(Enable("plugin"))}, logger.NewNoop(), afero.NewMemMapFs(), WithFeatures(r.Snapshot()))
	require.NoError(t, err)

	assert.True(t, p.Features.Enabled("plugin"))
	assert.True(t, p.Features.Enabled(UpdateCmd), "defaults still apply")
	assert.Same(t, p.Features, p.GetFeatures())
	assert.Equal(t, p.Features, p.Flags, "Flags defaults to the set")

	d, ok := r.Snapshot().Lookup("plugin")
	require.True(t, ok)
	assert.True(t, d.IsDynamic())
}

type allOnResolver struct{}

func (allOnResolver) Resolve(s features.Snapshot, _ []features.State) (features.Set, error) {
	var states []features.State
	for _, d := range s.Descriptors() {
		states = append(states, features.State{ID: d.FeatureID(), Enabled: true})
	}

	return features.Resolve(s, states)
}

func TestNew_WithResolverReplacesThePrecedence(t *testing.T) {
	t.Parallel()

	p, err := New(Tool{Name: "t", Features: SetFeatures(Disable(UpdateCmd))}, logger.NewNoop(), afero.NewMemMapFs(), WithResolver(allOnResolver{}))
	require.NoError(t, err)

	assert.True(t, p.Features.Enabled(UpdateCmd), "the consumer's resolver wins over the manifest")
	assert.True(t, p.Features.Enabled(AiCmd))
}

type staticSet struct{ features.Set }

func TestNew_WithSetBypassesResolution(t *testing.T) {
	t.Parallel()

	base, err := features.Resolve(features.Default().Snapshot(), nil)
	require.NoError(t, err)

	own := staticSet{base}

	p, err := New(Tool{Name: "t", Features: SetFeatures(Enable("ghost"))}, logger.NewNoop(), afero.NewMemMapFs(), WithSet(own))
	require.NoError(t, err, "a supplied Set is not re-resolved, so the unknown enable is never seen")
	assert.Equal(t, own, p.Features)
}

type fixedEvaluator struct{ on bool }

func (f fixedEvaluator) Evaluate(context.Context, features.ID, features.EvalContext) (features.Decision, error) {
	return features.Decision{Enabled: f.on, Reason: features.ReasonTargeting}, nil
}

func TestNew_WithFlagsSetsTheEvaluator(t *testing.T) {
	t.Parallel()

	p, err := New(Tool{Name: "t"}, logger.NewNoop(), afero.NewMemMapFs(), WithFlags(fixedEvaluator{on: true}))
	require.NoError(t, err)

	dec, err := p.GetFlags().Evaluate(context.Background(), AiCmd, features.EvalContext{})
	require.NoError(t, err)
	assert.True(t, dec.Enabled)
	assert.Equal(t, features.ReasonTargeting, dec.Reason)
}

func TestNew_RefusesEnablingAnUndeclaredFeature(t *testing.T) {
	t.Parallel()

	_, err := New(Tool{Name: "t", Features: SetFeatures(Enable("ghost"))}, logger.NewNoop(), afero.NewMemMapFs())
	require.ErrorIs(t, err, features.ErrUnknownFeature)
}

// TestGetFeatures_LiteralPropsResolvesOnRead: a Props built by literal (the
// shape hundreds of tests use) reads a set the way New would have built one,
// dropping an unknown enable and listing an unknown disable.
func TestGetFeatures_LiteralPropsResolvesOnRead(t *testing.T) {
	t.Parallel()

	p := &Props{Tool: Tool{Name: "t", Features: SetFeatures(Enable(AiCmd), Enable("ghost"), Disable("phantom"))}}

	set := p.GetFeatures()
	assert.True(t, set.Enabled(AiCmd))
	assert.False(t, set.Enabled("ghost"))
	assert.Equal(t, []features.State{{ID: "phantom", Enabled: false}}, set.Ignored())
	assert.Nil(t, p.Features, "GetFeatures does not write the field; ApplyDefaults does")

	p.ApplyDefaults()
	assert.NotNil(t, p.Features)
	assert.Equal(t, p.Features, p.Flags)

	dec, err := p.GetFlags().Evaluate(context.Background(), AiCmd, features.EvalContext{})
	require.NoError(t, err)
	assert.Equal(t, features.Decision{Enabled: true, Reason: features.ReasonStatic}, dec)
}

func TestFeatureDescriptor_RankOnlyForBuiltins(t *testing.T) {
	t.Parallel()

	rank, ok := FeatureDescriptor{ID: UpdateCmd}.Rank()
	assert.True(t, ok)
	assert.Equal(t, 0, rank)

	_, ok = FeatureDescriptor{ID: "gitlab", Kind: KindForge}.Rank()
	assert.False(t, ok)

	rank, ok = FeatureDescriptor{ID: "gitlab", Kind: KindForge, Order: 2}.Rank()
	assert.True(t, ok)
	assert.Equal(t, len(builtinOrder)+2, rank, "an ordered plugin follows every built-in in its declared order")
}
