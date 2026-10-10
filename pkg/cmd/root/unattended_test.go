package root

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"
	forgetest "gitlab.com/phpboyscout/go/forge/test"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	p "gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// Spec 0208: a run nobody can answer (CI, or a stdin that is not a terminal)
// is never stopped by a question on its start path.

const policyNotEnforced = "not enforced without a terminal"

func TestUnattendedReason(t *testing.T) {
	t.Setenv("CI", "")

	tests := []struct {
		name string
		yaml string
		io   p.IO
		want string
	}{
		{name: "a terminal", yaml: "{}\n", io: promptIO(""), want: ""},
		{name: "no terminal", yaml: "{}\n", io: nonInteractiveIO(), want: "no terminal"},
		{name: "CI at a terminal", yaml: "ci: true\n", io: promptIO(""), want: "CI environment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			props := &p.Props{IO: tt.io}

			assert.Equal(t, tt.want, unattendedReason(props, testutil.StoreFromYAML(t, tt.yaml).View()))
		})
	}
}

func outdatedProps(t *testing.T, policy p.UpdatePolicy, io p.IO) *p.Props {
	t.Helper()

	props := newUpdateProps(t, "v1.0.0", forgetest.New(forgetest.WithRelease("v2.0.0")))
	props.Tool.UpdatePolicy = policy
	props.IO = io

	return props
}

func checkRan(props *p.Props) bool {
	_, stamped := setup.TimeSinceLast(props.FS, props.Tool.Name, setup.CheckedKey)

	return stamped
}

func loggedAt(t *testing.T, props *p.Props, level logger.Level, substr string) bool {
	t.Helper()

	buf, ok := props.Logger.(interface {
		ContainsLevel(logger.Level, string) bool
	})
	require.True(t, ok, "test props must use logger.NewBuffer")

	return buf.ContainsLevel(level, substr)
}

func TestCheckForUpdates_UnattendedEnabledPolicyStarts(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyEnabled, nonInteractiveIO())

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	require.NoError(t, result.Error, "nobody can answer, so nothing blocks")
	assert.False(t, result.ShouldExit)
	assert.False(t, checkRan(props), "an unattended run makes no release probe")
	assert.True(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced), "the enabled policy says it was not enforced")
}

func TestCheckForUpdates_AttendedEnabledPolicyBlocksOnDecline(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyEnabled, promptIO("n"))

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	require.Error(t, result.Error, "a person who declines a required update is still blocked")
	assert.True(t, checkRan(props))
	assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
}

func TestCheckForUpdates_UnattendedPromptPolicyNeverUpdates(t *testing.T) {
	t.Setenv("CI", "")

	props := outdatedProps(t, p.UpdatePolicyPrompt, nonInteractiveIO())

	result := checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

	require.NoError(t, result.Error)
	assert.False(t, result.HasUpdated)
	assert.False(t, result.ShouldExit)
	assert.False(t, checkRan(props))
	assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced), "only the enabled policy warns")
}

func TestCheckForUpdates_UnattendedSuppressesTheCachedReminder(t *testing.T) {
	t.Setenv("CI", "")

	for _, tt := range []struct {
		name   string
		io     p.IO
		remind bool
	}{
		{name: "unattended", io: nonInteractiveIO(), remind: false},
		{name: "attended", io: promptIO("n"), remind: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			props := outdatedProps(t, p.UpdatePolicyDisabled, tt.io)
			require.NoError(t, setup.SetCheckedVersion(props.FS, props.Tool.Name, "v2.0.0"))

			checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

			assert.Equal(t, tt.remind, loggedAt(t, props, logger.WarnLevel, "a newer"))
		})
	}
}

func TestCheckForUpdates_PolicyWarningOnlyWhenTheTerminalIsTheReason(t *testing.T) {
	t.Run("CI skips without the warning", func(t *testing.T) {
		t.Setenv("CI", "true")

		props := outdatedProps(t, p.UpdatePolicyEnabled, nonInteractiveIO())

		checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

		assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
	})

	t.Run("a fresh throttle skips without the warning", func(t *testing.T) {
		t.Setenv("CI", "")

		props := outdatedProps(t, p.UpdatePolicyEnabled, nonInteractiveIO())
		props.Tool.UpdateCheckInterval = 24 * time.Hour
		require.NoError(t, setup.SetTimeSinceLast(props.FS, props.Tool.Name, setup.CheckedKey))

		checkForUpdates(context.Background(), mkUpdateCmd(t), props, newRootState())

		assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
	})

	t.Run("an exempt command skips without the warning", func(t *testing.T) {
		t.Setenv("CI", "")

		props := outdatedProps(t, p.UpdatePolicyEnabled, nonInteractiveIO())
		cmd := setup.MarkSkipUpdateCheck(mkUpdateCmd(t))

		checkForUpdates(context.Background(), cmd, props, newRootState())

		assert.False(t, loggedAt(t, props, logger.WarnLevel, policyNotEnforced))
	})
}

func TestPreRun_UnattendedMissingConfig(t *testing.T) {
	t.Setenv("CI", "")

	for _, tt := range []struct {
		name    string
		io      p.IO
		starts  bool
		logLine string
	}{
		{name: "unattended starts on the defaults", io: nonInteractiveIO(), starts: true, logLine: "no config file"},
		{name: "attended still needs init", io: promptIO(""), starts: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			props := noConfigProps(t, "unattended-tool")
			props.IO = tt.io
			props.Logger = logger.NewBuffer()

			var ran bool
			child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error {
				ran = true

				return nil
			}}

			err := execChild(t, props, child)

			if !tt.starts {
				require.ErrorIs(t, err, ErrNoConfigFile)
				assert.NotEmpty(t, errors.GetAllHints(err), "a person is told to run init")
				assert.False(t, ran)

				return
			}

			require.NoError(t, err)
			assert.True(t, ran)
			require.NotNil(t, props.Config)
			assert.Equal(t, "info", props.Config.View().GetString("log.level"), "the embedded defaults are loaded")
			assert.True(t, loggedAt(t, props, logger.InfoLevel, tt.logLine))
			assert.False(t, exists(t, props.FS, autoInitConfigPath(props.FS, "unattended-tool")), "nothing is written")
		})
	}
}

func TestPreRun_UnattendedMissingConfigUnderTheCIFlag(t *testing.T) {
	t.Setenv("CI", "")

	props := noConfigProps(t, "unattended-ci-tool")
	props.IO = promptIO("")

	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error { return nil }}

	rootCmd := NewCmdRoot(props, setup.Wrap("", child))
	rootCmd.SetArgs([]string{"child", "--ci"})

	require.NoError(t, rootCmd.ExecuteContext(context.Background()), "--ci at a terminal is unattended too")
}

func TestPreRun_UnattendedAutoInitialiseStillWrites(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("HOME", t.TempDir())

	props := noConfigProps(t, "unattended-autoinit-tool")
	props.IO = nonInteractiveIO()
	props.Tool.Bootstrap = p.BootstrapPolicy{AutoInitialise: true}

	child := &cobra.Command{Use: "child", RunE: func(_ *cobra.Command, _ []string) error { return nil }}

	require.NoError(t, execChild(t, props, child))
	assert.True(t, exists(t, props.FS, autoInitConfigPath(props.FS, "unattended-autoinit-tool")),
		"a tool that auto-initialises keeps doing so unattended")
}
