// Package main provides a test-only CLI binary for E2E/BDD testing.
//
// This binary enables ALL feature-flagged commands (including init) and uses
// a stub release source so that update/init scenarios can run without reaching
// external services. It is NOT shipped in releases — goreleaser only builds cmd/gtb.
package main

import (
	"embed"
	"encoding/base64"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/internal/cmd/generate"
	"github.com/phpboyscout/go-tool-base/internal/cmd/regenerate"
	"github.com/phpboyscout/go-tool-base/internal/cmd/remove"
	"github.com/phpboyscout/go-tool-base/internal/version"
	"github.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"

	// Register telemetry initialiser with the setup system.
	_ "github.com/phpboyscout/go-tool-base/pkg/setup/telemetry"
)

//go:embed all:assets
var assets embed.FS

const otelInstanceID = "1576673"

//nolint:gochecknoglobals // compile-time injection requires package-level var
var otelAuth string

func init() {
	if raw := os.Getenv("OTEL_API_KEY"); raw != "" {
		otelAuth = base64.StdEncoding.EncodeToString([]byte(otelInstanceID + ":" + raw))
	}
}

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
				props.Enable(props.TelemetryCmd),
				props.Disable(props.DocsCmd), // no embedded assets in test binary
			),
			Telemetry: props.TelemetryConfig{
				OTelEndpoint: "https://otlp-gateway-prod-gb-south-1.grafana.net/otlp",
				OTelHeaders: map[string]string{
					"Authorization": "Basic " + otelAuth,
				},
			},
		},
		Logger:  l,
		FS:      afero.NewOsFs(),
		Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
		Version: version.Get(),
	}

	p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

	rootCmd := root.NewCmdRoot(p)

	// Register the internal scaffolding commands so BDD scenarios
	// can exercise the real generator entry point (e.g. input
	// validation for `generate project --name`).
	rootCmd.AddCommand(generate.NewCmdGenerate(p))
	rootCmd.AddCommand(remove.NewCmdRemove(p))
	rootCmd.AddCommand(regenerate.NewCmdRegenerate(p))

	return rootCmd, p
}
