package props_test

import (
	"fmt"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func ExampleSetFeatures() {
	features := props.SetFeatures(
		props.Disable(props.InitCmd),
		props.Enable(props.AiCmd),
		props.Enable(props.TelemetryCmd),
	)

	for _, f := range features {
		if f.Enabled {
			fmt.Println("enabled:", f.ID)
		}
	}
}

func ExampleNew_features() {
	tool := props.Tool{
		Name: "mytool",
		Features: props.SetFeatures(
			props.Enable(props.AiCmd),
		),
	}

	p, err := props.New(tool, logger.NewNoop(), afero.NewMemMapFs())
	if err != nil {
		panic(err)
	}

	fmt.Println("AI:", p.Features.Enabled(props.AiCmd))
	fmt.Println("Init:", p.Features.Enabled(props.InitCmd))
	// Output:
	// AI: true
	// Init: true
}
