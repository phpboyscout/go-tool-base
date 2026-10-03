package doctor

import (
	"context"
	"fmt"
	"strings"

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// checkConfigStack reports the configuration stack in the order it resolves,
// lowest first, and how each declared source fared (spec 0204 D10). A
// required source that failed never reaches here: the root refused to start,
// which is itself the diagnosis.
func checkConfigStack(_ context.Context, props *p.Props) CheckResult {
	const name = "Config stack"

	spec := props.Tool.ResolvedConfigSpec()
	statuses := map[string]p.ConfigSourceStatus{}

	for _, s := range props.SourceStatuses {
		statuses[s.Slot.Name] = s
	}

	lines := make([]string, 0, len(spec.Layers))
	leftOut := 0

	for i, layer := range spec.Layers {
		line := describeBuiltinLayer(props, layer)

		if src, ok := declaredSource(spec, layer); ok {
			var missing bool

			line, missing = describeSource(props, src, statuses)
			if missing {
				leftOut++
			}
		}

		lines = append(lines, fmt.Sprintf("%d. %s", i+1, line))
	}

	result := CheckResult{Name: name, Status: CheckPass, Message: stackSummary(len(spec.Layers), len(spec.Sources), leftOut), Details: strings.Join(lines, "\n")}
	if leftOut > 0 {
		result.Status = CheckWarn
	}

	return result
}

func stackSummary(layers, sources, leftOut int) string {
	switch {
	case sources == 0:
		return fmt.Sprintf("%d layers, no config sources", layers)
	case leftOut > 0:
		return fmt.Sprintf("%d layers, %d config sources, %d left out", layers, sources, leftOut)
	case sources == 1:
		return fmt.Sprintf("%d layers, 1 config source", layers)
	default:
		return fmt.Sprintf("%d layers, %d config sources", layers, sources)
	}
}

func declaredSource(spec p.ConfigSpec, layer p.ConfigLayer) (p.ConfigSource, bool) {
	for _, s := range spec.Sources {
		if p.ConfigLayer(s.Name) == layer {
			return s, true
		}
	}

	return p.ConfigSource{}, false
}

func describeBuiltinLayer(props *p.Props, layer p.ConfigLayer) string {
	switch layer {
	case p.LayerDefaults:
		return "defaults: embedded defaults"
	case p.LayerFiles:
		files := "none yet"

		if props.Config != nil {
			if found := p.ConfigFileSources(props.Config.Snapshot()); len(found) > 0 {
				files = strings.Join(found, ", ")
			}
		}

		return "files: " + files
	case p.LayerProject:
		return "project: a discovered ." + strings.ToLower(props.Tool.Name) + ".* in the working tree, trust-filtered"
	case p.LayerEnv:
		if props.Tool.EnvPrefix == "" {
			return "env: none (the tool has no environment prefix)"
		}

		return "env: variables under " + props.Tool.EnvPrefix + "_"
	case p.LayerFlags:
		return "flags: changed flags"
	default:
		return string(layer)
	}
}

// describeSource is a slot's line, and whether the slot was left out.
func describeSource(props *p.Props, src p.ConfigSource, statuses map[string]p.ConfigSourceStatus) (string, bool) {
	head := fmt.Sprintf("%s (%s): ", src.Name, src.Kind)
	need := map[bool]string{true: "required", false: "optional"}[src.IsRequired()]

	status, ok := statuses[src.Name]
	if !ok {
		return head + "not built in this run, " + need, false
	}

	switch status.State {
	case p.ConfigSourceUnconfigured:
		return head + "not configured, " + need + "; " + configureHint(props, src), true
	case p.ConfigSourceUnavailable:
		return head + "unavailable, " + need + ": " + status.Err, true
	default:
		parts := []string{"built", need, map[bool]string{true: "writable", false: "read-only"}[status.Writable]}
		if status.Sensitive {
			parts = append(parts, "sensitive")
		}

		line := head + strings.Join(parts, ", ")
		if status.Credential != "" {
			line += "; credential from " + status.Credential
		}

		return line, false
	}
}

func configureHint(props *p.Props, src p.ConfigSource) string {
	if props.GetFeatures().Enabled(p.InitCmd) {
		return fmt.Sprintf("run `%s init config %s`", props.Tool.Name, src.Name)
	}

	return fmt.Sprintf("set config.sources.%s in the tool's defaults or the user's config file", src.Name)
}
