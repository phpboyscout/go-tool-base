package main

import (
	"github.com/phpboyscout/gtb/internal/cmd/root"
	"github.com/phpboyscout/gtb/internal/version"
	pkgRoot "github.com/phpboyscout/gtb/pkg/cmd/root"
)

func main() {
	rootCmd, p := root.NewCmdRoot(version.Get())
	pkgRoot.Execute(rootCmd, p)
}
