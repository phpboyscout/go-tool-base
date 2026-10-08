package setup

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/config"
)

// EnvVariablesFor returns the environment variables that supplied key or any
// key beneath it in snap, sorted; empty when the environment supplied none.
// A refusal about a key names them, so a stray variable is not blamed on the
// config file (spec 0205 D1).
func EnvVariablesFor(snap *config.Snapshot, key string) []string {
	var names []string

	for _, layer := range snap.Layers() {
		if layer.Source.Kind != config.SourceEnv {
			continue
		}

		if slices.ContainsFunc(leafPaths(layer.Values, ""), func(path string) bool {
			return path == key || strings.HasPrefix(path, key+".")
		}) {
			names = append(names, layer.Source.Name)
		}
	}

	slices.Sort(names)

	return slices.Compact(names)
}

func leafPaths(values map[string]any, prefix string) []string {
	var paths []string

	for name, value := range values {
		path := prefix + name

		if child, ok := value.(map[string]any); ok && len(child) > 0 {
			paths = append(paths, leafPaths(child, path+".")...)

			continue
		}

		paths = append(paths, path)
	}

	return paths
}
