package setup

import (
	"os"
	"strings"
	"unicode"

	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/internal/textescape"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// InstallManagerEnvVariable names the package manager or wrapper that installed
// the binary, for every gtb-based tool. A tool's <PREFIX>_INSTALL_MANAGER
// overrides it (spec 0217 D1).
const InstallManagerEnvVariable = "GTB_INSTALL_MANAGER"

// InstallUpgradeEnvVariable is the command that upgrades a managed install. It
// is printed, never run (spec 0217 D2).
const InstallUpgradeEnvVariable = "GTB_INSTALL_UPGRADE"

const (
	installManagerSuffix = "_INSTALL_MANAGER"
	installUpgradeSuffix = "_INSTALL_UPGRADE"
	variablesPerLevel    = 2
)

// EnvManagedInstall is an install whose manager the environment names. Manager
// and Upgrade are escaped for printing.
type EnvManagedInstall struct {
	Manager string
	Upgrade string
	// ManagerVariable named Manager; UpgradeVariable is its companion.
	ManagerVariable string
	UpgradeVariable string
}

// InstallManagerEnvVariables lists the variables that mark a tool with prefix
// as managed, its own pair first.
func InstallManagerEnvVariables(prefix string) []string {
	levels := installManagerLevels(prefix)
	names := make([]string, 0, variablesPerLevel*len(levels))

	for _, level := range levels {
		names = append(names, level.ManagerVariable, level.UpgradeVariable)
	}

	return names
}

// ManagedInstallFromEnv reports the manager the environment names for a tool
// with prefix. The tool's own pair wins over the framework's, as a pair.
func ManagedInstallFromEnv(prefix string) (EnvManagedInstall, bool) {
	return managedInstallFrom(os.Getenv, prefix)
}

func managedInstallFrom(getenv func(string) string, prefix string) (EnvManagedInstall, bool) {
	for _, level := range installManagerLevels(prefix) {
		manager := strings.TrimSpace(getenv(level.ManagerVariable))
		if manager == "" {
			continue
		}

		level.Manager = textescape.Unless(unicode.IsPrint, manager)
		level.Upgrade = textescape.Unless(unicode.IsPrint, strings.TrimSpace(getenv(level.UpgradeVariable)))

		return level, true
	}

	return EnvManagedInstall{}, false
}

// installManagerLevels normalises prefix as the config env backend does, so
// "widget" and "WIDGET_" both name WIDGET_INSTALL_MANAGER.
func installManagerLevels(prefix string) []EnvManagedInstall {
	prefix = strings.TrimSuffix(strings.ToUpper(prefix), "_")
	framework := EnvManagedInstall{ManagerVariable: InstallManagerEnvVariable, UpgradeVariable: InstallUpgradeEnvVariable}

	if prefix == "" || prefix+installManagerSuffix == InstallManagerEnvVariable {
		return []EnvManagedInstall{framework}
	}

	return []EnvManagedInstall{
		{ManagerVariable: prefix + installManagerSuffix, UpgradeVariable: prefix + installUpgradeSuffix},
		framework,
	}
}

// RefuseEnvManagedInstall is the refusal for tool when the environment names
// its manager, and nil otherwise.
func RefuseEnvManagedInstall(tool props.Tool) error {
	if m, ok := ManagedInstallFromEnv(tool.EnvPrefix); ok {
		return m.Refusal(tool.Name)
	}

	return nil
}

// Refusal is the error an update of tool returns, --force or not (spec 0217 D4).
func (m EnvManagedInstall) Refusal(tool string) error {
	err := errors.Wrapf(ErrManagedInstall, "%s is managed by %s (%s), so it does not update itself",
		tool, m.Manager, m.ManagerVariable)

	if m.Upgrade != "" {
		return errors.WithHintf(err, "Upgrade it with: %s", m.Upgrade)
	}

	return errors.WithHintf(err, "Upgrade it with %s. Set %s to have its upgrade command printed here",
		m.Manager, m.UpgradeVariable)
}
