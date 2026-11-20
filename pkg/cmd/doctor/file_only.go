package doctor

import (
	"context"
	"fmt"
	"strings"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// checkFileOnlyKeys lists the keys no prefixed environment variable may set and names
// any variable the root ignored on this run (spec 0214 D10). Advisory: a
// withheld variable warns and never fails the run.
func checkFileOnlyKeys(_ context.Context, props *p.Props) CheckResult {
	const name = "File-only config keys"

	patterns := props.Tool.Config.FileOnly
	if len(patterns) == 0 {
		return CheckResult{Name: name, Status: CheckSkip, Message: "none declared"}
	}

	variables := "config-layer variable"
	if prefix := props.Tool.EnvPrefix; prefix != "" {
		variables = prefix + "_ variable"
	}

	listed := "no " + variables + " sets " + strings.Join(patterns, ", ")

	if len(props.WithheldEnv) == 0 {
		return CheckResult{Name: name, Status: CheckPass, Message: listed}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckWarn,
		Message: fmt.Sprintf("%s; %d variable(s) aimed at them were ignored, so unset them or set the keys in a config file", listed, len(props.WithheldEnv)),
		Details: strings.Join(props.WithheldEnv, "; "),
	}
}
