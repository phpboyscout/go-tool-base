package forge

import (
	"fmt"
	"strings"

	"gitlab.com/phpboyscout/go/errors"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go/features"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ErrForgeNotLinked reports a forge feature enabled in a tool whose binary does
// not link the adapter that registers the forge. It is a build mistake, not a
// configuration one, so it is raised before configuration is read.
var ErrForgeNotLinked = errors.NewSentinel("gtb.setup.forge.not_linked", "forge feature enabled but its adapter is not linked")

// modulesByProvider names the module that registers each forge type. Codeberg
// is a Forgejo instance that forge-gitea serves under its own type.
var modulesByProvider = map[string]string{
	"github":    "gitlab.com/phpboyscout/go/forge-github",
	"gitlab":    "gitlab.com/phpboyscout/go/forge-gitlab",
	"gitea":     "gitlab.com/phpboyscout/go/forge-gitea",
	"codeberg":  "gitlab.com/phpboyscout/go/forge-gitea",
	"bitbucket": "gitlab.com/phpboyscout/go/forge-bitbucket",
}

// ModuleFor returns the module path whose blank import registers the forge
// type, and reports whether the type is one this package knows.
func ModuleFor(provider string) (string, bool) {
	module, ok := modulesByProvider[provider]

	return module, ok
}

// UnlinkedForge is a forge feature the tool enables without linking its adapter.
type UnlinkedForge struct {
	Feature  props.FeatureID
	Provider string
	Label    string
	Module   string
}

// Unlinked reports every forge feature the tool enables whose forge type has no
// registered provider, which means the adapter module is not linked into the
// binary.
func Unlinked(set features.Set) []UnlinkedForge {
	profiles := make([]Profile, 0, len(profilesByFeature))
	for _, profile := range profilesByFeature {
		profiles = append(profiles, profile)
	}

	return unlinked(profiles, set.Enabled, forgeapi.Registered)
}

func unlinked(profiles []Profile, enabled func(props.FeatureID) bool, registered func(string) bool) []UnlinkedForge {
	var missing []UnlinkedForge

	for _, profile := range profiles {
		if !enabled(profile.Feature) || registered(profile.Provider) {
			continue
		}

		module, _ := ModuleFor(profile.Provider)
		missing = append(missing, UnlinkedForge{
			Feature:  profile.Feature,
			Provider: profile.Provider,
			Label:    profile.Label,
			Module:   module,
		})
	}

	return missing
}

// UnlinkedError turns a non-empty Unlinked report into an error whose hint
// names the blank imports the tool's main package is missing. It returns nil
// for an empty report.
func UnlinkedError(missing []UnlinkedForge) error {
	if len(missing) == 0 {
		return nil
	}

	labels := make([]string, 0, len(missing))
	imports := make([]string, 0, len(missing))

	for _, m := range missing {
		labels = append(labels, m.Label)
		imports = append(imports, fmt.Sprintf("    _ %q", m.Module))
	}

	return errors.WithHintf(
		errors.Wrapf(ErrForgeNotLinked, "%s", strings.Join(labels, ", ")),
		"The binary enables %s but does not link the adapter. Add to the tool's main package:\n\nimport (\n%s\n)",
		strings.Join(labels, ", "), strings.Join(imports, "\n"),
	)
}
