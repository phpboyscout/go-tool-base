package root

import (
	"embed"
	"encoding/base64"
	"os"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"

	"github.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"github.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/setup"
	ver "github.com/phpboyscout/go-tool-base/pkg/version"

	"github.com/phpboyscout/go-tool-base/internal/cmd/generate"
	"github.com/phpboyscout/go-tool-base/internal/cmd/regenerate"
	"github.com/phpboyscout/go-tool-base/internal/cmd/remove"

	// Register telemetry initialiser with the setup system.
	_ "github.com/phpboyscout/go-tool-base/pkg/setup/telemetry"
)

//go:embed all:assets
var assets embed.FS

const otelInstanceID = "1576673"

// otelAuth is injected at compile time via ldflags as a pre-encoded
// base64("<instanceID>:<token>") string. For local development, the
// OTEL_API_KEY env var can supply the raw token — init() encodes it.
//
//nolint:gochecknoglobals // compile-time injection requires package-level var
var otelAuth string

func init() {
	if raw := os.Getenv("OTEL_API_KEY"); raw != "" {
		otelAuth = base64.StdEncoding.EncodeToString([]byte(otelInstanceID + ":" + raw))
	}

	// Phase 1 of the update-checksum-verification spec: gtb is a
	// security-tooling CLI that cannot silently accept unverified
	// binaries. GoReleaser has always produced checksums.txt on every
	// release, so flipping the library default to fail-closed is
	// safe — a future release with a broken pipeline will abort the
	// update with an actionable error rather than quietly installing
	// an unverified binary.
	//
	// End users can still override via the `update.require_checksum`
	// config key or `GTB_UPDATE_REQUIRE_CHECKSUM=false` env var if
	// they need to update from a legacy release lacking the manifest.
	setup.DefaultRequireChecksum = true
}

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
				props.Enable(props.TelemetryCmd),
			),
			EnvPrefix: "GTB",
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
