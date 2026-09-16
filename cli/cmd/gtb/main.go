package main

import (
	"fmt"
	"os"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/version"
	pkgRoot "gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
)

func main() {
	rootCmd, p, err := root.NewCmdRoot(version.Get())
	if err != nil {
		fmt.Fprintln(os.Stderr, "gtb:", err)
		os.Exit(errorhandling.ExitCodeUsage)
	}

	pkgRoot.Execute(rootCmd, p)
}
