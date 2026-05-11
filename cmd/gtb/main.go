package main

import (
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/internal/version"
	pkgRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
)

func main() {
	rootCmd, p := root.NewCmdRoot(version.Get())
	pkgRoot.Execute(rootCmd, p)
}
