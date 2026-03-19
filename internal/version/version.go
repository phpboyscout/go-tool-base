package version

import (
	"runtime/debug"

	pkgversion "github.com/phpboyscout/gtb/pkg/version"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			commit = setting.Value
		case "vcs.time":
			date = setting.Value
		case "vcs.modified":
			if setting.Value == "true" {
				commit += "-dirty"
			}
		}
	}

	// Only override version if it's still the default "dev"
	if version == "dev" {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		} else {
			// Fallback to the short commit hash if no tag exists
			version = commit
		}
	}
}

// Get returns the current version information as a pkgversion.Info.
func Get() pkgversion.Info {
	return pkgversion.NewInfo(version, commit, date)
}
