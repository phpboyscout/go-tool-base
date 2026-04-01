// Package cmd provides shared utilities for internal CLI commands.
package cmd

import (
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/workspace"
)

// ResolveProjectPath resolves the project root from the given path.
// If path is "." (the default), it attempts workspace detection by walking
// up from CWD to find a .gtb/manifest.yaml. If detection fails, the
// original path is returned unchanged (the user may be generating a new project).
func ResolveProjectPath(p *props.Props, path string) string {
	if path != "." {
		return path
	}

	ws, err := workspace.DetectFromCWD(p.FS, []string{".gtb/manifest.yaml"})
	if err != nil {
		return path
	}

	p.Logger.Debug("Resolved workspace root", "path", ws.Root)

	return ws.Root
}
