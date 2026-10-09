package doctor

import (
	"context"
	"fmt"
	"slices"
	"strings"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// checkStrayEnvVariables reports the <PREFIX>_* variables that set a key the
// tool does not declare (spec 0205 D3). Every such variable is configuration,
// and a CI job's own TOOL_VERSION or TOOL_FLAGS is otherwise invisible until
// something refuses its key. Advisory: it names variables, never values, and
// never fails a run.
func checkStrayEnvVariables(_ context.Context, props *p.Props) CheckResult {
	const name = "Environment variables"

	if props.Tool.EnvPrefix == "" || props.Config == nil {
		return CheckResult{Name: name, Status: CheckSkip, Message: "no environment prefix"}
	}

	recognised := setup.NewConfigKeyRecogniser(props)
	readByName := setup.DeclaredEnvVariables(props.GetFeatures())

	var stray []string

	for _, k := range setup.EnvKeys(props.Config.View().Snapshot()) {
		if !recognised.Recognises(k.Key) && !slices.Contains(readByName, k.Variable) {
			stray = append(stray, k.Variable+" sets "+k.Key)
		}
	}

	if len(stray) == 0 {
		return CheckResult{Name: name, Status: CheckPass, Message: "every " + props.Tool.EnvPrefix + "_* variable sets a known key"}
	}

	return CheckResult{
		Name:    name,
		Status:  CheckWarn,
		Message: fmt.Sprintf("%d %s_* variable(s) set keys %s does not declare; rename or unset any that are not meant as configuration", len(stray), props.Tool.EnvPrefix, props.Tool.Name),
		Details: strings.Join(stray, "; "),
	}
}
