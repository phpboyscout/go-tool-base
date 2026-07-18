package root_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errorhandling"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/root"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// bindTestProps builds a Props with networked/interactive features disabled so
// the pre-run path runs config loading + flag binding without prompts.
func bindTestProps(t *testing.T, fs afero.Fs) *p.Props {
	t.Helper()

	return &p.Props{
		Tool: p.Tool{
			Name:      "bindtool",
			EnvPrefix: "BINDTOOL",
			Features: p.SetFeatures(
				p.Disable(p.UpdateCmd),
				p.Disable(p.McpCmd),
				p.Disable(p.DocsCmd),
				p.Disable(p.DoctorCmd),
				p.Disable(p.TelemetryCmd),
			),
		},
		// A Charm logger (discarded) rather than NewNoop: it implements Leveller,
		// so TestBindBuiltins can observe --debug taking effect via Enabled. A
		// plain *slog.Logger from NewNoop owns its own level and ignores SetLevel.
		Logger:       logger.NewCharm(io.Discard),
		FS:           fs,
		Assets:       p.NewAssets(),
		ErrorHandler: errorhandling.New(logger.ToSlog(logger.NewNoop()), nil),
	}
}

func writeConfig(t *testing.T, fs afero.Fs, path, body string) {
	t.Helper()
	require.NoError(t, afero.WriteFile(fs, path, []byte(body), 0o644))
}

const bindCfgPath = "/etc/bindtool/config.yaml"

// TestBindBoundFlags_RootOption exercises WithBoundFlags end-to-end through the
// real PersistentPreRunE path: a changed flag overrides config; an unset flag
// does not (verification plan items 1).
func TestBindBoundFlags_RootOption(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantPort int
	}{
		{
			name:     "changed flag overrides config",
			args:     []string{"--server-port", "9090", "probe"},
			wantPort: 9090,
		},
		{
			name:     "unset flag does not override config",
			args:     []string{"probe"},
			wantPort: 8080,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setup.ResetRegistryForTesting()
			t.Cleanup(setup.ResetRegistryForTesting)

			fs := afero.NewMemMapFs()
			writeConfig(t, fs, bindCfgPath, "server:\n  port: 8080\n")
			props := bindTestProps(t, fs)

			var gotPort int

			probe := &cobra.Command{
				Use: "probe",
				RunE: func(_ *cobra.Command, _ []string) error {
					gotPort = props.Config.GetInt("server.port")

					return nil
				},
			}

			portFlags := pflag.NewFlagSet("bound", pflag.ContinueOnError)
			portFlags.Int("server-port", 0, "server port")

			rootCmd := root.NewCmdRootWithOptions(props,
				root.WithSubcommands(setup.Wrap("", probe)),
				root.WithBoundFlags(map[string]*pflag.Flag{
					"server.port": portFlags.Lookup("server-port"),
				}),
			)

			rootCmd.SetArgs(append([]string{"--config", bindCfgPath}, tc.args...))
			require.NoError(t, rootCmd.Execute())

			assert.Equal(t, tc.wantPort, gotPort)
		})
	}
}

// TestBindConventionFlags exercises WithConventionBoundFlags: --server-port maps
// to server.port automatically.
func TestBindConventionFlags(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	fs := afero.NewMemMapFs()
	writeConfig(t, fs, bindCfgPath, "server:\n  port: 8080\n")
	props := bindTestProps(t, fs)

	var gotPort int

	probe := &cobra.Command{
		Use: "probe",
		RunE: func(_ *cobra.Command, _ []string) error {
			gotPort = props.Config.GetInt("server.port")

			return nil
		},
	}

	flags := pflag.NewFlagSet("conv", pflag.ContinueOnError)
	flags.Int("server-port", 0, "server port")

	rootCmd := root.NewCmdRootWithOptions(props,
		root.WithSubcommands(setup.Wrap("", probe)),
		root.WithConventionBoundFlags(flags),
	)

	rootCmd.SetArgs([]string{"--config", bindCfgPath, "--server-port", "7000", "probe"})
	require.NoError(t, rootCmd.Execute())

	assert.Equal(t, 7000, gotPort)
}

// TestBindPrecedence proves flag > env > file for the same key (verification
// plan item 2).
func TestBindPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		setEnv   bool
		setFlag  bool
		wantPort int
	}{
		{name: "file only", wantPort: 8080},
		{name: "env beats file", setEnv: true, wantPort: 9000},
		{name: "flag beats env and file", setEnv: true, setFlag: true, wantPort: 9090},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setup.ResetRegistryForTesting()
			t.Cleanup(setup.ResetRegistryForTesting)

			if tc.setEnv {
				t.Setenv("BINDTOOL_SERVER_PORT", "9000")
			}

			fs := afero.NewMemMapFs()
			writeConfig(t, fs, bindCfgPath, "server:\n  port: 8080\n")
			props := bindTestProps(t, fs)

			var gotPort int

			probe := &cobra.Command{
				Use: "probe",
				RunE: func(_ *cobra.Command, _ []string) error {
					gotPort = props.Config.GetInt("server.port")

					return nil
				},
			}

			portFlags := pflag.NewFlagSet("bound", pflag.ContinueOnError)
			portFlags.Int("server-port", 0, "server port")

			rootCmd := root.NewCmdRootWithOptions(props,
				root.WithSubcommands(setup.Wrap("", probe)),
				root.WithBoundFlags(map[string]*pflag.Flag{
					"server.port": portFlags.Lookup("server-port"),
				}),
			)

			args := []string{"--config", bindCfgPath}
			if tc.setFlag {
				args = append(args, "--server-port", "9090")
			}

			args = append(args, "probe")
			rootCmd.SetArgs(args)
			require.NoError(t, rootCmd.Execute())

			assert.Equal(t, tc.wantPort, gotPort)
		})
	}
}

// TestBindBuiltins confirms --debug still sets the log level and --ci is visible
// in config after binding (verification plan item 3).
func TestBindBuiltins(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	fs := afero.NewMemMapFs()
	writeConfig(t, fs, bindCfgPath, "server:\n  port: 8080\n")

	props := bindTestProps(t, fs)

	var (
		gotCI           bool
		gotDebugEnabled bool
	)

	probe := &cobra.Command{
		Use: "probe",
		RunE: func(_ *cobra.Command, _ []string) error {
			gotCI = props.Config.GetBool("ci")
			gotDebugEnabled = props.Logger.Enabled(context.Background(), slog.LevelDebug)

			return nil
		},
	}

	rootCmd := root.NewCmdRootWithOptions(props, root.WithSubcommands(setup.Wrap("", probe)))
	rootCmd.SetArgs([]string{"--config", bindCfgPath, "--ci", "--debug", "probe"})
	require.NoError(t, rootCmd.Execute())

	assert.True(t, gotCI, "--ci should be visible in config after binding")
	assert.True(t, gotDebugEnabled, "--debug should still enable debug logging")
}

// TestBindPerCommandFlags proves a command's own local flag overrides config for
// that command's RunE via the hyphen-to-dot convention (verification plan item
// 4 / D5).
func TestBindPerCommandFlags(t *testing.T) {
	setup.ResetRegistryForTesting()
	t.Cleanup(setup.ResetRegistryForTesting)

	fs := afero.NewMemMapFs()
	writeConfig(t, fs, bindCfgPath, "server:\n  port: 8080\n")
	props := bindTestProps(t, fs)

	var gotPort int

	serve := &cobra.Command{
		Use: "serve",
		RunE: func(_ *cobra.Command, _ []string) error {
			gotPort = props.Config.GetInt("server.port")

			return nil
		},
	}
	// The command's own local flag, named by convention.
	serve.Flags().Int("server-port", 0, "server port for serve")

	rootCmd := root.NewCmdRootWithOptions(props, root.WithSubcommands(setup.Wrap("", serve)))
	rootCmd.SetArgs([]string{"--config", bindCfgPath, "serve", "--server-port", "6060"})
	require.NoError(t, rootCmd.Execute())

	assert.Equal(t, 6060, gotPort)
}

func TestConventionKey(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "server.port", root.ConventionKey("server-port"))
	assert.Equal(t, "log.level", root.ConventionKey("log-level"))
	assert.Equal(t, "plain", root.ConventionKey("plain"))
}
