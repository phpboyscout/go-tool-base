package telemetry

import "gitlab.com/phpboyscout/go-tool-base/pkg/props"

// ResolveDataDirFromProps adapts GTB props/config into the package-owned data
// directory resolver.
func ResolveDataDirFromProps(p props.ConfigProvider) string {
	if p == nil || p.GetConfig() == nil {
		return ResolveDataDir("")
	}

	return ResolveDataDir(p.GetConfig().GetViper().ConfigFileUsed())
}
