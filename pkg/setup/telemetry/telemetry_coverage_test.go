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

	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// TestRegisteredProviders exercises the closures registered in init() by
// reading them back from the setup registry and invoking them. The init()
// body only runs at import time; calling the closures here covers the
// provider and feature-flag bodies.
//
// NOT t.Parallel(), and the sub-tests below are serial too: the registered
// telemetry closures both READ the skipTelemetry flag (InitialiserProvider)
// and WRITE it (FeatureFlag binds the same *bool via pflag.BoolVar). Running
// the read and write sub-tests concurrently would be a data race on the
// shared flag pointer — the exact pattern CLAUDE.md's "no package-level
// mocking hooks" guidance forbids under t.Parallel(). Keep them sequential.
//
// CI is pinned to empty so the flag's `isCI` default is deterministic.
func TestRegisteredProviders(t *testing.T) {
	t.Setenv("CI", "")

	props := newTestProps(t)

	t.Run("initialiser provider returns TelemetryInitialiser when not skipped", func(t *testing.T) {
		ips := setup.GetInitialisers()[p.TelemetryCmd]
		require.NotEmpty(t, ips, "telemetry initialiser provider must be registered")

		var got setup.Initialiser

		for _, ip := range ips {
			if i := ip(props); i != nil {
				got = i
			}
		}

		require.NotNil(t, got)
		assert.Equal(t, "telemetry", got.Name())
	})

	t.Run("no subcommands registered", func(t *testing.T) {
		sps := setup.GetSubcommands()[p.TelemetryCmd]
		assert.Empty(t, sps, "telemetry registers no init subcommands")
	})

	t.Run("feature flag registers --skip-telemetry with CI=false default", func(t *testing.T) {
		fps := setup.GetFeatureFlags()[p.TelemetryCmd]
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

// TestFeatureFlagDefaultsTrueUnderCI covers the isCI=true branch of the
// FeatureFlag closure: with CI=true the --skip-telemetry default flips to
// true. Serial: mutates the CI environment variable and binds the shared
// skipTelemetry pointer.
func TestFeatureFlagDefaultsTrueUnderCI(t *testing.T) {
	t.Setenv("CI", "true")

	fps := setup.GetFeatureFlags()[p.TelemetryCmd]
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

// TestInitialiserProviderSkips covers the *skipTelemetry == true branch of
// the registered initialiser provider closure: the provider must return nil
// when the flag is set. Serial: it binds and sets the shared skipTelemetry
// pointer through the registered FeatureFlag closure, then reads it back via
// the InitialiserProvider — these touch the same process-global pointer.
func TestInitialiserProviderSkips(t *testing.T) {
	t.Setenv("CI", "")

	// Bind the shared skipTelemetry pointer to a fresh command, then flip it
	// to true through the bound flag — this mutates exactly the value the
	// InitialiserProvider reads.
	fps := setup.GetFeatureFlags()[p.TelemetryCmd]
	require.NotEmpty(t, fps)

	cmd := &cobra.Command{Use: "init"}
	for _, fp := range fps {
		fp(cmd)
	}

	require.NoError(t, cmd.Flags().Set("skip-telemetry", "true"))

	props := newTestProps(t)

	ips := setup.GetInitialisers()[p.TelemetryCmd]
	require.NotEmpty(t, ips)

	for _, ip := range ips {
		assert.Nil(t, ip(props), "provider must return nil when skip-telemetry is set")
	}

	// Reset the bound pointer so subsequent serial tests see a clean default.
	require.NoError(t, cmd.Flags().Set("skip-telemetry", "false"))
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
