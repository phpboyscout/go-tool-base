package telemetry

import (
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ResolveDataDirFromProps adapts GTB props/config into the package-owned data
// directory resolver.
func ResolveDataDirFromProps(p props.ConfigProvider) string {
	if p == nil || p.GetConfig() == nil {
		return ResolveDataDir("")
	}

	files := props.ConfigFileSources(p.GetConfig().Snapshot())
	if len(files) == 0 {
		return ResolveDataDir("")
	}

	return ResolveDataDir(files[len(files)-1])
}
