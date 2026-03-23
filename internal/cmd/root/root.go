package root

import (
	"embed"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/gtb/pkg/cmd/root"
	"github.com/phpboyscout/gtb/pkg/errorhandling"
	"github.com/phpboyscout/gtb/pkg/logger"
	"github.com/phpboyscout/gtb/pkg/props"
	ver "github.com/phpboyscout/gtb/pkg/version"

	"github.com/phpboyscout/gtb/internal/cmd/generate"
	"github.com/phpboyscout/gtb/internal/cmd/regenerate"
	"github.com/phpboyscout/gtb/internal/cmd/remove"
)

//go:embed all:assets
var assets embed.FS

func NewCmdRoot(v ver.Info) (*cobra.Command, *props.Props) {
	l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

	p := &props.Props{
		Tool: props.Tool{
			Name:        "gtb",
			Summary:     "The gtb CLI",
			Description: "A CLI tool for managing and generating gtb projects.",
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "phpboyscout",
				Repo:  "gtb",
			},
			Features: props.SetFeatures(
				props.Disable(props.InitCmd),
				props.Enable(props.AiCmd),
			),
		},
		Logger:  l,
		FS:      afero.NewOsFs(),
		Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
		Version: v,
	}

	p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

	// Create root command using the library functionality
	rootCmd := root.NewCmdRoot(p)

	// Add the generate command
	rootCmd.AddCommand(generate.NewCmdGenerate(p))

	// Add the remove command
	rootCmd.AddCommand(remove.NewCmdRemove(p))

	// Add the regenerate command
	rootCmd.AddCommand(regenerate.NewCmdRegenerate(p))

	return rootCmd, p
}
