package initialise

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// pathInitialiser is a fake kind's initialiser: it writes a path under its
// slot's settings, which is what a real one does after asking.
type pathInitialiser struct{ slot p.ConfigSource }

func (i pathInitialiser) Name() string                    { return i.slot.Name }
func (i pathInitialiser) IsConfigured(config.Reader) bool { return false }

func (i pathInitialiser) Configure(_ context.Context, _ *p.Props, cfg setup.Editor) error {
	return cfg.Set("config.sources."+i.slot.Name+".path", "/src/"+i.slot.Name+".yaml")
}

func sourcesProps(t *testing.T, fs afero.Fs, sources ...p.ConfigSource) *p.Props {
	t.Helper()

	reg := features.NewRegistry()
	for _, d := range p.DescriptorsIn(features.Default().Snapshot()) {
		require.NoError(t, reg.Declare(d))
	}

	require.NoError(t, reg.Declare(setup.ConfigSourceDescriptor("memfile")))
	setup.RegisterConfigSourceKindOn(reg, "memfile", nil, func(_ *p.Props, slot p.ConfigSource) setup.Initialiser {
		return pathInitialiser{slot: slot}
	})

	layers := []p.ConfigLayer{p.LayerDefaults, p.LayerFiles}
	for _, s := range sources {
		layers = append(layers, p.ConfigLayer(s.Name))
	}

	tool := p.Tool{Name: "mytool", Config: p.ConfigSpec{Sources: sources, Layers: append(layers, p.LayerEnv, p.LayerFlags)}}

	props, err := p.New(tool, logger.NewNoop(), fs, p.WithFeatures(reg.Snapshot()), p.WithAssets(p.NewAssets()))
	require.NoError(t, err)

	return props
}

// Spec 0204 D3 and R5: each declared slot is configured by
// `<tool> init config <name>`, which writes its settings to the user's file.
func TestInitConfig_WritesTheSlotsSettings(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	props := sourcesProps(t, fs,
		p.ConfigSource{Name: "team", Kind: "memfile"},
		p.ConfigSource{Name: "shared", Kind: "memfile"})

	cmd := NewCmdInit(props)
	cmd.SetArgs([]string{"config", "shared", "--dir", "/home/me/.mytool"})
	require.NoError(t, cmd.Execute())

	written, err := afero.ReadFile(fs, "/home/me/.mytool/config.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(written), "/src/shared.yaml", "the kind's initialiser is told which slot it writes")
	assert.NotContains(t, string(written), "/src/team.yaml", "and only that one")
}

func TestInitConfig_OneSubcommandPerSlot(t *testing.T) {
	t.Parallel()

	props := sourcesProps(t, afero.NewMemMapFs(),
		p.ConfigSource{Name: "team", Kind: "memfile"},
		p.ConfigSource{Name: "legacy", Kind: "etcd"})

	group, _, err := NewCmdInit(props).Find([]string{"config"})
	require.NoError(t, err)
	require.Equal(t, "config", group.Name())

	var names []string
	for _, c := range group.Commands() {
		names = append(names, c.Name())
	}

	assert.ElementsMatch(t, []string{"team", "legacy"}, names)
}

// A source the tool's own code builds has nothing for init to ask.
func TestInitConfig_AnOverrideOnlySlotSaysWhy(t *testing.T) {
	t.Parallel()

	props := sourcesProps(t, afero.NewMemMapFs(), p.ConfigSource{Name: "legacy", Kind: "etcd"})

	cmd := NewCmdInit(props)
	cmd.SetArgs([]string{"config", "legacy", "--dir", "/home/me/.mytool"})
	err := cmd.Execute()
	require.ErrorIs(t, err, setup.ErrConfigSourceNeedsOverride)
}

// A tool that declares no source has no `init config`.
func TestInitConfig_AbsentWithoutSources(t *testing.T) {
	t.Parallel()

	for _, c := range NewCmdInit(sourcesProps(t, afero.NewMemMapFs())).Commands() {
		assert.NotEqual(t, "config", c.Name())
	}
}
