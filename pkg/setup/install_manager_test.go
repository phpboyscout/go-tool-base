package setup

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/pkgmanager"
)

func TestManagedInstallFrom(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prefix  string
		env     map[string]string
		want    EnvManagedInstall
		managed bool
	}{
		{name: "nothing set", prefix: "WIDGET"},
		{name: "whitespace is unset", prefix: "WIDGET", env: map[string]string{"GTB_INSTALL_MANAGER": "  \t"}},
		{
			name: "the framework pair", prefix: "WIDGET",
			env: map[string]string{"GTB_INSTALL_MANAGER": " mise ", "GTB_INSTALL_UPGRADE": "mise upgrade widget"},
			want: EnvManagedInstall{Manager: "mise", Upgrade: "mise upgrade widget",
				ManagerVariable: "GTB_INSTALL_MANAGER", UpgradeVariable: "GTB_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "the tool's own pair", prefix: "WIDGET",
			env: map[string]string{"WIDGET_INSTALL_MANAGER": "aqua", "WIDGET_INSTALL_UPGRADE": "aqua update widget"},
			want: EnvManagedInstall{Manager: "aqua", Upgrade: "aqua update widget",
				ManagerVariable: "WIDGET_INSTALL_MANAGER", UpgradeVariable: "WIDGET_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "a lower-case prefix is upper-cased", prefix: "widget",
			env: map[string]string{"WIDGET_INSTALL_MANAGER": "aqua"},
			want: EnvManagedInstall{Manager: "aqua",
				ManagerVariable: "WIDGET_INSTALL_MANAGER", UpgradeVariable: "WIDGET_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "a trailing underscore on the prefix is dropped", prefix: "WIDGET_",
			env: map[string]string{"WIDGET_INSTALL_MANAGER": "aqua"},
			want: EnvManagedInstall{Manager: "aqua",
				ManagerVariable: "WIDGET_INSTALL_MANAGER", UpgradeVariable: "WIDGET_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "the tool's pair wins as a pair", prefix: "WIDGET",
			env: map[string]string{
				"WIDGET_INSTALL_MANAGER": "aqua",
				"GTB_INSTALL_MANAGER":    "mise", "GTB_INSTALL_UPGRADE": "mise upgrade widget",
			},
			want: EnvManagedInstall{Manager: "aqua",
				ManagerVariable: "WIDGET_INSTALL_MANAGER", UpgradeVariable: "WIDGET_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "a tool with no prefix reads the framework pair", env: map[string]string{"GTB_INSTALL_MANAGER": "mise"},
			want:    EnvManagedInstall{Manager: "mise", ManagerVariable: "GTB_INSTALL_MANAGER", UpgradeVariable: "GTB_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "the GTB prefix is the framework pair", prefix: "GTB", env: map[string]string{"GTB_INSTALL_MANAGER": "mise"},
			want:    EnvManagedInstall{Manager: "mise", ManagerVariable: "GTB_INSTALL_MANAGER", UpgradeVariable: "GTB_INSTALL_UPGRADE"},
			managed: true,
		},
		{
			name: "control characters are escaped", prefix: "WIDGET",
			env: map[string]string{"GTB_INSTALL_MANAGER": "mi\x1b[2Jse", "GTB_INSTALL_UPGRADE": "run\nfake: line"},
			want: EnvManagedInstall{Manager: `mi\u001b[2Jse`, Upgrade: `run\u000afake: line`,
				ManagerVariable: "GTB_INSTALL_MANAGER", UpgradeVariable: "GTB_INSTALL_UPGRADE"},
			managed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, managed := managedInstallFrom(func(k string) string { return tt.env[k] }, tt.prefix)
			assert.Equal(t, tt.managed, managed)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstallManagerEnvVariables(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"WIDGET_INSTALL_MANAGER", "WIDGET_INSTALL_UPGRADE", "GTB_INSTALL_MANAGER", "GTB_INSTALL_UPGRADE"},
		InstallManagerEnvVariables("WIDGET"))
	assert.Equal(t, InstallManagerEnvVariables("WIDGET"), InstallManagerEnvVariables("widget_"))
	assert.Equal(t, []string{"GTB_INSTALL_MANAGER", "GTB_INSTALL_UPGRADE"}, InstallManagerEnvVariables("GTB"))
	assert.Equal(t, []string{"GTB_INSTALL_MANAGER", "GTB_INSTALL_UPGRADE"}, InstallManagerEnvVariables("GTB_"))
	assert.Equal(t, []string{"GTB_INSTALL_MANAGER", "GTB_INSTALL_UPGRADE"}, InstallManagerEnvVariables(""))
}

func TestEnvManagedInstall_Refusal(t *testing.T) {
	t.Parallel()

	m := EnvManagedInstall{Manager: "mise", Upgrade: "mise upgrade widget",
		ManagerVariable: "GTB_INSTALL_MANAGER", UpgradeVariable: "GTB_INSTALL_UPGRADE"}

	err := m.Refusal("widget")
	require.ErrorIs(t, err, ErrManagedInstall)
	assert.Contains(t, err.Error(), "widget is managed by mise (GTB_INSTALL_MANAGER), so it does not update itself")
	assert.Contains(t, errors.FlattenHints(err), "Upgrade it with: mise upgrade widget")

	m.Upgrade = ""
	hint := errors.FlattenHints(m.Refusal("widget"))
	assert.Contains(t, hint, "Upgrade it with mise")
	assert.Contains(t, hint, "GTB_INSTALL_UPGRADE")
}

// Spec 0217 D4: the environment refuses a library caller of Update and
// UpdateFromFile, --force or not, before the path detector is asked.
func TestSelfUpdater_RefusesAnEnvManagedInstall(t *testing.T) {
	t.Setenv(InstallManagerEnvVariable, "mise")
	t.Setenv(InstallUpgradeEnvVariable, "mise upgrade "+e2eToolName)

	notManaged := func(context.Context, string) (pkgmanager.Install, bool) {
		t.Error("the path detector runs only when the environment names no manager")

		return pkgmanager.Install{}, false
	}

	for _, force := range []bool{false, true} {
		s := newManagedUpdater(t, "/usr/local/bin/"+e2eToolName)
		s.force = force
		s.detectManaged = notManaged
		s.osExecutable = func() (string, error) {
			t.Error("the target is not resolved, since resolving it can prompt")

			return "", errors.New("not resolved")
		}

		_, err := s.Update(t.Context())
		require.ErrorIs(t, err, ErrManagedInstall)
		assert.Contains(t, errors.FlattenHints(err), "mise upgrade "+e2eToolName)

		_, err = s.UpdateFromFile(t.Context(), "/nonexistent/release.tar.gz")
		require.ErrorIs(t, err, ErrManagedInstall)

		got, readErr := afero.ReadFile(s.Fs, "/usr/local/bin/"+e2eToolName)
		require.NoError(t, readErr)
		assert.Equal(t, "old-binary", string(got))
	}
}
