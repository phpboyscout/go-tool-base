package root

import (
	"context"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// These tests are spec 0199's T1, T2 and T9: a root owns its feature set, its
// middleware chain and its flag state, so two roots in one process share
// nothing, and every part is replaceable through an interface.

func ownedProps(t *testing.T, collector p.TelemetryCollector) *p.Props {
	t.Helper()

	props, err := p.New(
		p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable(p.InitCmd))},
		logger.NewNoop(),
		afero.NewMemMapFs(),
		p.WithCollector(collector),
		p.WithAssets(p.NewAssets()),
	)
	require.NoError(t, err)

	return props
}

// TestNewCmdRoot_TwoRootsInParallel is T1 (#27, #37): building two roots at
// once is race-free under -race and neither panics. The forge init flags used
// to bind to package variables and the middleware registry sealed on first
// build; both are per root now.
func TestNewCmdRoot_TwoRootsInParallel(t *testing.T) {
	t.Parallel()

	var wg sync.WaitGroup

	roots := make([]*setup.Command, 4)
	inputs := make([]*p.Props, len(roots))

	for i := range inputs {
		inputs[i] = ownedProps(t, p.NoopCollector{})
	}

	for i := range roots {
		wg.Add(1)

		go func() {
			defer wg.Done()

			roots[i] = NewCmdRoot(inputs[i])
		}()
	}

	wg.Wait()

	for _, r := range roots {
		require.NotNil(t, r)

		init, _, err := r.Find([]string{"init"})
		require.NoError(t, err)
		assert.NotNil(t, init.Flags().Lookup(setup.SkipKeyFlag), "each root binds init's own --skip-key")
	}
}

type countingCollector struct {
	p.NoopCollector

	mu    sync.Mutex
	names []string
}

func (c *countingCollector) TrackCommand(name string, _ int64, _ int, _ map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.names = append(c.names, name)
}

// TestNewCmdRoot_EachRootReportsToItsOwnCollector is T2: the telemetry
// middleware closes over the root's own Props. Before, the chain was built
// once per process and every root reported through the first root's Props.
func TestNewCmdRoot_EachRootReportsToItsOwnCollector(t *testing.T) {
	t.Parallel()

	first, second := &countingCollector{}, &countingCollector{}

	rootA := NewCmdRoot(ownedProps(t, first), setup.Wrap("", &cobra.Command{Use: "ping", RunE: func(*cobra.Command, []string) error { return nil }}))
	rootB := NewCmdRoot(ownedProps(t, second), setup.Wrap("", &cobra.Command{Use: "ping", RunE: func(*cobra.Command, []string) error { return nil }}))

	run := func(r *setup.Command) {
		cmd, _, err := r.Find([]string{"ping"})
		require.NoError(t, err)
		cmd.SetContext(context.Background())
		require.NoError(t, cmd.RunE(cmd, nil))
	}

	run(rootB)
	run(rootB)
	run(rootA)

	assert.Equal(t, []string{"ping"}, first.names, "the first root's collector saw its own command only")
	assert.Equal(t, []string{"ping", "ping"}, second.names, "the second root's collector saw both of its runs")
}

type recordingChain struct {
	mu       sync.Mutex
	features []p.FeatureID
}

func (r *recordingChain) Chain(feature p.FeatureID, runE setup.RunE) setup.RunE {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.features = append(r.features, feature)

	return runE
}

// TestNewCmdRoot_PartsAreReplaceable is T9 (D12): a registry of the test's own
// reaches the tree through WithRegistry, and a Chainer of the test's own
// receives every feature the root chains.
func TestNewCmdRoot_PartsAreReplaceable(t *testing.T) {
	t.Parallel()

	const plugin = p.FeatureID("plugin-probe")

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(p.FeatureDescriptor{ID: plugin, ConstName: "PluginProbe", ConstPackage: "example.com/probe", Kind: "plugin"}))
	reg.Contribute(plugin, setup.SlotSubcommand, setup.SubcommandProvider(func(*p.Props) []*cobra.Command {
		return []*cobra.Command{{Use: "probe", RunE: func(*cobra.Command, []string) error { return nil }}}
	}))

	chain := &recordingChain{}

	props := &p.Props{
		Tool:   p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable(p.InitCmd), p.Enable(plugin))},
		Logger: logger.NewNoop(),
		FS:     afero.NewMemMapFs(),
		Assets: p.NewAssets(),
	}

	root := NewCmdRootWithOptions(props, WithRegistry(reg), WithChain(chain))

	assert.True(t, props.Features.Enabled(plugin), "the root resolved the set from the test's registry")

	probe, _, err := root.Find([]string{"init", "probe"})
	require.NoError(t, err)
	assert.Equal(t, "probe", probe.Name(), "the test registry's contribution reached init")

	assert.Contains(t, chain.features, p.InitCmd, "the root chained init through the test's Chainer")
}

// TestNewCmdRoot_EnablingAnUndeclaredFeatureIsAnError is OQ2 at the
// construction path: New refuses it; a literal Props drops it and carries on.
func TestNewCmdRoot_EnablingAnUndeclaredFeatureIsAnError(t *testing.T) {
	t.Parallel()

	_, err := p.New(p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable("ghost"))}, logger.NewNoop(), afero.NewMemMapFs())
	require.ErrorIs(t, err, features.ErrUnknownFeature)

	props := &p.Props{Tool: p.Tool{Name: "owned", Features: p.SetFeatures(p.Enable("ghost"), p.Disable("phantom"))}, Logger: logger.NewNoop(), FS: afero.NewMemMapFs(), Assets: p.NewAssets()}
	require.NotNil(t, NewCmdRoot(props))
	assert.False(t, props.Features.Enabled("ghost"))
	assert.True(t, props.Features.Enabled(p.UpdateCmd), "the defaults still apply")
	assert.Equal(t, []features.State{{ID: "phantom", Enabled: false}}, props.Features.Ignored())
}
