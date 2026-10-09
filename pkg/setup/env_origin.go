package setup

import (
	"slices"
	"strings"

	"gitlab.com/phpboyscout/go/config"
)

// EnvKey is one configuration key an environment variable set.
type EnvKey struct {
	Variable string
	Key      string
}

// EnvKeys lists every key the environment supplied in snap, as the store
// mapped it, sorted by variable.
func EnvKeys(snap *config.Snapshot) []EnvKey {
	var keys []EnvKey

	for _, layer := range snap.Layers() {
		if layer.Source.Kind != config.SourceEnv {
			continue
		}

		for _, path := range leafPaths(layer.Values, "") {
			keys = append(keys, EnvKey{Variable: layer.Source.Name, Key: path})
		}
	}

	slices.SortFunc(keys, func(a, b EnvKey) int { return strings.Compare(a.Variable, b.Variable) })

	return keys
}

// EnvVariablesFor returns the environment variables that supplied key or any
// key beneath it in snap, sorted; empty when the environment supplied none.
// A refusal about a key names them, so a stray variable is not blamed on the
// config file (spec 0205 D1).
func EnvVariablesFor(snap *config.Snapshot, key string) []string {
	var names []string

	for _, k := range EnvKeys(snap) {
		if k.Key == key || strings.HasPrefix(k.Key, key+".") {
			names = append(names, k.Variable)
		}
	}

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
