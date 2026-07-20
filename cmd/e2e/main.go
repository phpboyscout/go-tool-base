// Package main provides a test-only CLI binary for E2E/BDD testing of
// framework features.
//
// Why it exists: it lets us write contrived scenarios for the framework that a
// downstream tool inherits (init, doctor, config, update, chat, controls,
// signals, bootstrap) WITHOUT baking test fixtures into the real, shipped
// binary. It enables ALL feature-flagged commands. It is NOT shipped —
// goreleaser only builds ./cmd/gtb.
//
// Self-update flows are network-free under test: when GTB_E2E_RELEASE_SCENARIO
// is set, applyReleaseStub (release_stub.go) injects an in-memory stub release
// source (go/forge/test) onto the tool and pins a deterministic
// current version, so `gtb update` scenarios resolve releases from memory. When
// the env var is unset the configured GitLab source is used as normal.
//
// IMPORTANT — when NOT to use this binary: because it ENABLES InitCmd, the
// framework bootstrap REQUIRES a config file (init is how a tool's first config
// is created), so scenarios driving this binary must provide one. Conversely,
// gtb's own command-line tooling — generate, regenerate, remove, keys — is
// tested against ./cmd/gtb, which DISABLES InitCmd and runs config-free
// (allow-empty bootstrap), matching production. Driving the generator through
// this InitCmd-enabled binary wrongly demands a config file: green locally
// (ambient ~/.config/gtb config), red in CI's clean environment. Generator
// scenarios use support.GeneratorBinaryPath() (./cmd/gtb), not this binary.
//
// See docs/development/testing/index.md § "E2E test binaries".
package main

import (
	"embed"
	"encoding/base64"
	"os"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/generate"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/keys"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/regenerate"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/remove"
	"gitlab.com/phpboyscout/go-tool-base/internal/cmd/sign"
	tmplcmd "gitlab.com/phpboyscout/go-tool-base/internal/cmd/template"
	"gitlab.com/phpboyscout/go-tool-base/internal/version"
	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"

	// Register telemetry initialiser with the setup system.
	_ "gitlab.com/phpboyscout/go-tool-base/pkg/setup/telemetry"
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

func newTestRoot() (*setup.Command, *props.Props) {
	l := logger.NewCharm(os.Stderr, logger.WithTimestamp(true))

	p := &props.Props{
		Tool: props.Tool{
			Name:        "gtb",
			Summary:     "GTB E2E test binary",
			Description: "A test-only binary with all features enabled for E2E/BDD testing.",
			// Match production gtb's env prefix so the test binary does
			// not inherit raw credential env vars (ANTHROPIC_API_KEY
			// etc.) from the developer's shell via viper's AutomaticEnv.
			EnvPrefix: "GTB",
			ReleaseSource: props.ReleaseSource{
				Type:  "gitlab",
				Owner: "phpboyscout",
				Repo:  "go-tool-base",
			},
			Features: props.SetFeatures(
				props.Enable(props.InitCmd),
				props.Enable(props.UpdateCmd),
				props.Enable(props.DoctorCmd),
				props.Enable(props.McpCmd),
				props.Enable(props.ConfigCmd),
				props.Enable(props.TelemetryCmd),
				props.Enable(props.ManCmd), // hidden; enabled here for BDD coverage
				// AiCmd + github + bitbucket are not enabled by
				// default but are needed for BDD / manual testing of
				// the credential setup wizards and the chat/VCS
				// resolvers.
				props.Enable(props.AiCmd),
				props.Enable(props.FeatureCmd("github")),
				props.Enable(props.FeatureCmd("bitbucket")),
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

	p.ErrorHandler = errorhandling.New(logger.ToSlog(l), p.Tool.Help)

	// When GTB_E2E_RELEASE_SCENARIO is set, swap the configured GitLab release
	// source for an in-memory stub so `gtb update` scenarios run hermetically.
	applyReleaseStub(p)

	// Register the internal scaffolding commands so BDD scenarios
	// can exercise the real generator entry point (e.g. input
	// validation for `generate project --name`).
	rootCmd := root.NewCmdRoot(p,
		generate.NewCmdGenerate(p),
		remove.NewCmdRemove(p),
		regenerate.NewCmdRegenerate(p),
		keys.NewCmdKeys(p),
		sign.NewCmdSign(p),
		tmplcmd.NewCmdTemplate(p),
		// config-probe is a contrived fixture for the bootstrap-traversal
		// E2E (spec 2026-06-12): a subcommand with its own
		// PersistentPreRunE that reads props.Config in RunE, proving the
		// root bootstrap still runs via EnableTraverseRunHooks.
		newConfigProbeCmd(p),
	)

	// Register the hidden `block` fixture used by the SIGINT E2E scenario to
	// exercise the signal-aware execution context at the OS level.
	rootCmd.Register(newBlockCommand())

	return rootCmd, p
}
