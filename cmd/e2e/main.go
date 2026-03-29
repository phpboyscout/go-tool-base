// Package main provides a test-only CLI binary for E2E/BDD testing.
//
// This binary enables ALL feature-flagged commands (including init) and uses
// a stub release source so that update/init scenarios can run without reaching
// external services. It is NOT shipped in releases — goreleaser only builds cmd/gtb.
package main

import (
	"embed"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/internal/version"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
)

//go:embed all:assets
var assets embed.FS

func main() {
	rootCmd, p := newTestRoot()
	root.Execute(rootCmd, p)
}

func newTestRoot() (*cobra.Command, *props.Props) {
	l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

	p := &props.Props{
		Tool: props.Tool{
			Name:        "gtb",
			Summary:     "GTB E2E test binary",
			Description: "A test-only binary with all features enabled for E2E/BDD testing.",
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "phpboyscout",
				Repo:  "gtb",
			},
			Features: props.SetFeatures(
				props.Enable(props.InitCmd),
				props.Enable(props.UpdateCmd),
				props.Enable(props.DoctorCmd),
				props.Enable(props.McpCmd),
				props.Enable(props.ConfigCmd),
				props.Disable(props.DocsCmd), // no embedded assets in test binary
			),
		},
		Logger:  l,
		FS:      afero.NewOsFs(),
		Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
		Version: version.Get(),
	}

	p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

	rootCmd := root.NewCmdRoot(p)

	return rootCmd, p
}
