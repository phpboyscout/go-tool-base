package root

import (
	"embed"
	"encoding/base64"
	"os"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/errorhandling"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	ver "gitlab.com/phpboyscout/go-tool-base/pkg/version"

	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/disable"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/enable"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/generate"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/keys"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/regenerate"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/remove"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/sign"
	"gitlab.com/phpboyscout/go-tool-base/internal/trustkeys"

	// Register telemetry initialiser with the setup system.
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/telemetry"
)

//go:embed all:assets
var assets embed.FS

const otelInstanceID = "1576673"

// otelAuth is injected at compile time via ldflags as a pre-encoded
// base64("<instanceID>:<token>") string. For local development, the
// OTEL_API_KEY env var can supply the raw token — init() encodes it.
// The gochecknoglobals exception for this package-level var is declared
// in .golangci.yaml (the linker can only set package-level vars).
var otelAuth string

func init() {
	if raw := os.Getenv("OTEL_API_KEY"); raw != "" {
		otelAuth = base64.StdEncoding.EncodeToString([]byte(otelInstanceID + ":" + raw))
	}

	// gtb-specific signing defaults (DefaultRequireChecksum,
	// DefaultRequireSignature, DefaultExternalKeyEmail) live in the
	// sibling signing.go file. Kept out of root.go because the
	// scaffolding generator templates from root.go and shouldn't ship
	// our signing posture into downstream tools.
}

func NewCmdRoot(v ver.Info) (*setup.Command, *props.Props) {
	l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

	p := &props.Props{
		Tool: props.Tool{
			Name:        "gtb",
			Summary:     "The gtb CLI",
			Description: "A CLI tool for managing and generating gtb projects.",
			ReleaseSource: props.ReleaseSource{
				Type:  "gitlab",
				Owner: "phpboyscout",
				Repo:  "go-tool-base",
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
			// Embedded release public key(s) for self-update signature
			// verification. Empty until a key is added under
			// internal/trustkeys/keys/*.asc — see phase2-signing-prep.md.
			Signing: props.SigningConfig{
				EmbeddedKeys: trustkeys.Keys(),
			},
		},
		Logger:  l,
		FS:      afero.NewOsFs(),
		Assets:  props.NewAssets(props.AssetMap{"root": &assets}),
		Version: v,
	}

	p.ErrorHandler = errorhandling.New(l, p.Tool.Help)

	// Create root command using the library functionality, with gtb-specific
	// subcommands registered through the new composed Register pipeline so they
	// pick up global middleware automatically.
	rootCmd := root.NewCmdRoot(p,
		generate.NewCmdGenerate(p),
		remove.NewCmdRemove(p),
		regenerate.NewCmdRegenerate(p),
		keys.NewCmdKeys(p),
		sign.NewCmdSign(p),
		enable.NewCmdEnable(p),
		disable.NewCmdDisable(p),
	)

	return rootCmd, p
}
