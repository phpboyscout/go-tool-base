package telemetry

import (
	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ResolveDataDirFromProps adapts GTB props/config into the package-owned data
// directory resolver.
func ResolveDataDirFromProps(p props.ConfigProvider) string {
	if p == nil || p.GetConfig() == nil {
		return ResolveDataDir("")
	}

	return ResolveDataDir(lastFileSource(p.GetConfig().Snapshot()))
}

// lastFileSource returns the path of the highest-precedence loaded file layer.
// Parity with what this replaced: Viper's ConfigFileUsed() reported the last
// file it loaded (see docs/reference/migration/v0.21-config-files-accessor.md),
// and Layers() is ordered lowest to highest, so the last file entry is the
// same answer.
func lastFileSource(snap *config.Snapshot) string {
	last := ""

	for _, layer := range snap.Layers() {
		if layer.Source.Kind == config.SourceFile {
			last = layer.Source.Name
		}
	}

	return last
}
