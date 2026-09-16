package telemetry

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	setupmocks "gitlab.com/phpboyscout/go-tool-base/mocks/pkg/setup"

	"gitlab.com/phpboyscout/go-tool-base/pkg/features"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestRegisteredProviders exercises the closures registered in init() by
// reading them back from the setup registry and invoking them. The init()
// body only runs at import time; calling the closures here covers the
// provider and feature-flag bodies.
//
// TestRegisteredProviders drives the init-time registrations through a
// snapshot of the default registry: the flag is bound on a command of the
// test's own and read back through the provider, so nothing is process state
// (spec 0199 D3). CI is pinned so the flag's default is deterministic.
func TestRegisteredProviders(t *testing.T) {
	t.Setenv("CI", "")

	props := newTestProps(t)
	snapshot := features.Default().Snapshot()

	t.Run("initialiser provider returns TelemetryInitialiser when not skipped", func(t *testing.T) {
		ips := setup.InitialisersIn(snapshot)[p.TelemetryCmd]
		require.NotEmpty(t, ips, "telemetry initialiser provider must be registered")

		var got setup.Initialiser

		for _, ip := range ips {
			if i := ip(props, nil); i != nil {
				got = i
			}
		}

		require.NotNil(t, got)
		assert.Equal(t, "telemetry", got.Name())
	})

	t.Run("no subcommands registered", func(t *testing.T) {
		sps := setup.SubcommandsIn(snapshot)[p.TelemetryCmd]
		assert.Empty(t, sps, "telemetry registers no init subcommands")
	})

	t.Run("feature flag registers --skip-telemetry with CI=false default", func(t *testing.T) {
		fps := setup.FeatureFlagsIn(snapshot)[p.TelemetryCmd]
		require.NotEmpty(t, fps, "telemetry feature-flag provider must be registered")

		cmd := &cobra.Command{Use: "init"}
		for _, fp := range fps {
			fp(cmd)
		}

		flag := cmd.Flags().Lookup("skip-telemetry")
		require.NotNil(t, flag, "feature flag must register the --skip-telemetry flag")
		assert.Equal(t, "false", flag.DefValue,
			"default must be false when CI is not set")
	})
}

// TestFeatureFlagDefaultsTrueUnderCI covers the CI branch of the FeatureFlag
// closure: with CI=true the --skip-telemetry default flips to true. Serial:
// mutates the CI environment variable.
func TestFeatureFlagDefaultsTrueUnderCI(t *testing.T) {
	t.Setenv("CI", "true")

	fps := setup.FeatureFlagsIn(features.Default().Snapshot())[p.TelemetryCmd]
	require.NotEmpty(t, fps)

	cmd := &cobra.Command{Use: "init"}
	for _, fp := range fps {
		fp(cmd)
	}

	flag := cmd.Flags().Lookup("skip-telemetry")
	require.NotNil(t, flag)
	assert.Equal(t, "true", flag.DefValue,
		"default must be true under CI=true")
}

// TestInitialiserProviderSkips covers the skip branch of the registered
// initialiser provider: bound on a command of this test's own, set, and read
// back through the provider from those flags.
func TestInitialiserProviderSkips(t *testing.T) {
	t.Setenv("CI", "")

	snapshot := features.Default().Snapshot()

	fps := setup.FeatureFlagsIn(snapshot)[p.TelemetryCmd]
	require.NotEmpty(t, fps)

	cmd := &cobra.Command{Use: "init"}
	for _, fp := range fps {
		fp(cmd)
	}

	require.NoError(t, cmd.Flags().Set("skip-telemetry", "true"))

	props := newTestProps(t)

	ips := setup.InitialisersIn(snapshot)[p.TelemetryCmd]
	require.NotEmpty(t, ips)

	for _, ip := range ips {
		assert.Nil(t, ip(props, cmd.Flags()), "provider must return nil when skip-telemetry is set")
	}
}

// TestConsentForm_NamesTheTool: the question is asked in the tool's name,
// and the answer lands on the bound value.
func TestConsentForm_NamesTheTool(t *testing.T) {
	t.Parallel()

	props := newTestProps(t)
	out := &bytes.Buffer{}
	props.IO = p.StdIO{Stdin: formtest.Answers("y"), Stdout: out, Stderr: out, AccessibleMode: true}

	var optIn bool

	require.NoError(t, setup.RunForm(t.Context(), props, ConsentForm(props, &optIn)))
	assert.True(t, optIn)
	assert.Contains(t, out.String(), "Anonymous usage telemetry")
}

// TestConfigure_FormRunError covers Configure's error branch: with nobody at
// the terminal the form refuses to run, the error is wrapped and returned,
// and telemetry.enabled is NOT set.
func TestConfigure_FormRunError(t *testing.T) {
	t.Parallel()

	mock := setupmocks.NewMockEditor(t)
	// Set must never be called on the error path.

	props := newTestProps(t)
	props.IO = p.StdIO{Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard}
	init := NewTelemetryInitialiser(props)

	err := init.Configure(t.Context(), props, mock)
	require.ErrorIs(t, err, setup.ErrNonInteractive)
	assert.Contains(t, err.Error(), "telemetry consent form")
}
