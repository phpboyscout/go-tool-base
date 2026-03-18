package version

import (
	"runtime/debug"

	"github.com/phpboyscout/gtb/pkg/version"
)

var (
	v = "dev"
	c = "none"
	d = "unknown"
)

func init() {
	if v != "dev" {
		return
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	if info.Main.Version != "(devel)" {
		v = info.Main.Version
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			c = setting.Value
		case "vcs.time":
			d = setting.Value
		}
	}
}

// Get returns the current version information as a pkg/version.Info.
func Get() version.Info {
	return version.NewInfo(v, c, d)
}
