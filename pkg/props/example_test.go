package props_test

import (
	"fmt"

	"github.com/phpboyscout/go-tool-base/pkg/props"
)

func ExampleSetFeatures() {
	features := props.SetFeatures(
		props.Disable(props.InitCmd),
		props.Enable(props.AiCmd),
		props.Enable(props.TelemetryCmd),
	)

	for _, f := range features {
		if f.Enabled {
			fmt.Println("enabled:", f.Cmd)
		}
	}
}

func ExampleTool_IsEnabled() {
	tool := props.Tool{
		Name: "mytool",
		Features: props.SetFeatures(
			props.Enable(props.AiCmd),
		),
	}

	fmt.Println("AI:", tool.IsEnabled(props.AiCmd))
	fmt.Println("Init:", tool.IsEnabled(props.InitCmd))
	// Output:
	// AI: true
	// Init: true
}
