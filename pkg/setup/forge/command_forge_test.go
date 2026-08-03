package forge

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

// The generic `init <forge>` builder is what spec 0185 D1 replaced the
// per-forge builders with, so its wording is derived from the profile rather
// than written per command. These tests pin that derivation: a new forge gets
// its command by existing, and the help it produces has to read correctly
// without anyone editing a builder.

func TestNewCmdInitForge_DerivesItsSurfaceFromTheProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		profile       Profile
		wantUse       string
		wantShort     string
		wantInLong    []string
		wantNotInLong []string
	}{
		{
			name:      "GitLab offers SSH and login",
			profile:   gitLabProfile,
			wantUse:   "gitlab",
			wantShort: "Configure GitLab authentication and SSH keys",
			wantInLong: []string{
				"Configure the GitLab token",
				"generate or select",
				"SSH key for Git operations",
			},
			// GitLab supports device-flow login, so the manual-entry caveat
			// must not appear.
			wantNotInLong: []string{"does not support an interactive browser login"},
		},
		{
			name:      "Gitea offers SSH but no login",
			profile:   giteaProfile,
			wantUse:   "gitea",
			wantShort: "Configure Gitea authentication and SSH keys",
			wantInLong: []string{
				"Configure the Gitea token",
				"SSH key for Git operations",
				"Gitea does not support an interactive browser login",
				"entered by hand",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := &props.Props{
				FS:     afero.NewMemMapFs(),
				Logger: logger.NewNoop(),
				Tool:   props.Tool{Name: "testtool"},
			}

			cmd := NewCmdInitForge(p, tt.profile)
			require.NotNil(t, cmd)

			assert.Equal(t, tt.wantUse, cmd.Use)
			assert.Equal(t, tt.wantShort, cmd.Short)
			require.NotNil(t, cmd.Flags().Lookup("dir"), "the --dir flag is how the wizard is pointed at a config")

			for _, want := range tt.wantInLong {
				assert.Containsf(t, cmd.Long, want, "long help should mention %q", want)
			}

			for _, notWant := range tt.wantNotInLong {
				assert.NotContainsf(t, cmd.Long, notWant, "long help should not mention %q", notWant)
			}
		})
	}
}

// TestNewCmdInitForge_HostlessProfileRendersNoEmptyInterpolation guards the
// same hazard as D3 from the command layer: Gitea has no default host, and a
// profile without one must not leave a dangling fragment in its help.
func TestNewCmdInitForge_HostlessProfileRendersNoEmptyInterpolation(t *testing.T) {
	t.Parallel()

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	cmd := NewCmdInitForge(p, giteaProfile)

	require.Empty(t, giteaProfile.Host, "fixture assumption: Gitea carries no default host")
	assert.NotContains(t, cmd.Long, "{host}")
	assert.NotContains(t, cmd.Short, "{host}")
	assert.NotContains(t, cmd.Long, "https://.")
}

func TestSSHShortSuffix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, " and SSH keys", sshShortSuffix(Profile{OffersSSH: true}))
	assert.Empty(t, sshShortSuffix(Profile{OffersSSH: false}))
}

// TestNewCmdInitForge_RunE_WrapsTheFailureWithTheForgeLabel drives the RunE
// path far enough to fail (no TTY, no resolvable credential) and pins that the
// error names the forge — the generic builder must not report every forge's
// failure under one label.
func TestNewCmdInitForge_RunE_WrapsTheFailureWithTheForgeLabel(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
		Assets: props.NewAssets(),
	}

	cmd := NewCmdInitForge(p, gitLabProfile)
	cmd.SetContext(t.Context())
	require.NoError(t, cmd.Flags().Set("dir", "/cfgdir"))

	err := cmd.RunE(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to configure GitLab")
}

// TestRunForgeInitCmd_SurfacesAConfigEditorFailure covers the early return
// before the wizard runs: an unopenable config directory must fail rather than
// proceed against a nil editor.
func TestRunForgeInitCmd_SurfacesAConfigEditorFailure(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("CI", "")

	p := &props.Props{
		FS:     afero.NewReadOnlyFs(afero.NewMemMapFs()),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
		Assets: props.NewAssets(),
	}

	err := RunForgeInitCmd(t.Context(), p, "/cfgdir", giteaProfile)
	require.Error(t, err)
}

// TestRegisteredForgeProviders_HonourTheirSkipFlag exercises the closures
// registerSingleTokenForge installs. They are the reason a forge contributes an
// initialiser, a subcommand and a skip flag by being registered, so leaving
// them uninvoked would mean the registration wiring itself was never run.
func TestRegisteredForgeProviders_HonourTheirSkipFlag(t *testing.T) {
	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}

	tests := []struct {
		name    string
		feature props.FeatureID
		skip    *bool
		flag    string
	}{
		{"gitlab", GitlabFeature, &skipGitlab, "skip-gitlab"},
		{"gitea", GiteaFeature, &skipGitea, "skip-gitea"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Not parallel: these mutate the package-level skip flags the
			// registered closures read.
			initialisers := setup.GetInitialisers()[tt.feature]
			require.NotEmpty(t, initialisers)

			*tt.skip = false

			defer func() { *tt.skip = false }()

			assert.NotNil(t, initialisers[0](p), "an unskipped forge must yield an initialiser")

			*tt.skip = true
			assert.Nil(t, initialisers[0](p), "a skipped forge must yield no initialiser")

			subcommands := setup.GetSubcommands()[tt.feature]
			require.NotEmpty(t, subcommands)

			cmds := subcommands[0](p)
			require.Len(t, cmds, 1)
			assert.Equal(t, tt.name, cmds[0].Use)

			flags := setup.GetFeatureFlags()[tt.feature]
			require.NotEmpty(t, flags)

			target := &cobra.Command{Use: "init"}
			flags[0](target)
			assert.NotNil(t, target.Flags().Lookup(tt.flag),
				"the forge must contribute its %s flag", tt.flag)
		})
	}
}
