package main

import (
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/version"
	pkgRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
)

func main() {
	rootCmd, p := root.NewCmdRoot(version.Get())
	pkgRoot.Execute(rootCmd, p)
}
