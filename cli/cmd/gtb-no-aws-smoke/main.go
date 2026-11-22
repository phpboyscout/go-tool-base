// Command gtb-no-aws-smoke is a compile-time fixture that proves
// the gtb root does not pull the AWS SDK into the linked binary
// transitively.
//
// It is the same gtb binary minus chat-bedrock in providers.go, the one
// import that carries AWS code now that gtb no longer signs (sigillum
// does). A regulated downstream that needs FIPS-only or air-gapped builds
// follows this pattern: omit that blank import, keep everything else.
//
// smoke_test.go is the runtime arm: the root registers no signing
// backend, so nothing reaches the KMS backend through it.
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
