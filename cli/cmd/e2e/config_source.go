package main

import (
	"context"
	"os"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// configSourceEnv, when set, declares a required config source slot named
// team, of the e2e-file kind, and names the file its init writes as the
// slot's path. Unset, the binary declares no source, so no other scenario
// sees one (spec 0204 D3, D6).
const configSourceEnv = "GTB_E2E_CONFIG_SOURCE"

// The e2e-file kind reads the YAML file its slot names: a stand-in for a
// remote source, with no network.
func init() {
	setup.RegisterConfigSourceKind("e2e-file",
		func(_ context.Context, settings config.Reader, _ setup.ConfigBootstrap) (config.Backend, error) {
			return config.NewCodecBackend(config.OS(), settings.GetString("path"), config.YAMLCodec{}), nil
		},
		func(_ *props.Props, slot props.ConfigSource) setup.Initialiser { return e2eFileInitialiser{slot: slot} })
}

type e2eFileInitialiser struct{ slot props.ConfigSource }

func (i e2eFileInitialiser) Name() string { return i.slot.Name }

func (i e2eFileInitialiser) IsConfigured(cfg config.Reader) bool {
	return cfg.GetString("config.sources."+i.slot.Name+".path") != ""
}

func (i e2eFileInitialiser) Configure(_ context.Context, _ *props.Props, cfg setup.Editor) error {
	return cfg.Set("config.sources."+i.slot.Name+".path", os.Getenv(configSourceEnv))
}

// declareConfigSource gives the tool its team slot when the scenario asks.
func declareConfigSource(tool *props.Tool) {
	if os.Getenv(configSourceEnv) == "" {
		return
	}

	tool.Config = props.ConfigSpec{
		Sources: []props.ConfigSource{{Name: "team", Kind: "e2e-file"}},
		Layers: []props.ConfigLayer{
			props.LayerDefaults, props.LayerFiles, "team", props.LayerProject, props.LayerEnv, props.LayerFlags,
		},
	}
}
