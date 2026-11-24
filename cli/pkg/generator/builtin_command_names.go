package generator

import (
	"fmt"
	"slices"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrBuiltinCommandName is a top-level command and a built-in with one name or alias:
// only one of them could run.
var ErrBuiltinCommandName = errors.NewSentinel("gtb.generator.builtin_command_name",
	"a top-level command would share its name with a built-in")

// ErrAttachedCommandName is an attached command and another command of the
// project, its own or attached, with one name or alias.
var ErrAttachedCommandName = errors.NewSentinel("gtb.generator.attached_command_name",
	"an attached command would share its name with another command")

// featureCommandNames are the features whose root command has the feature's name.
var featureCommandNames = map[string]bool{
	"update": true, "init": true, "mcp": true, "docs": true, "doctor": true,
	"config": true, "changelog": true, "man": true, "telemetry": true,
}

// frameworkCommandNames have no feature to disable. completion is not one:
// cobra leaves its own out when the project has one.
var frameworkCommandNames = map[string]bool{"version": true, "help": true}

// builtinHoldsName reports whether a built-in has name on a root with these
// features, and what to do about it.
func builtinHoldsName(name string, features []ManifestFeature) (remedy string, held bool) {
	if frameworkCommandNames[name] {
		return fmt.Sprintf("Choose another name: every tool has a %s command.", name), true
	}

	if featureCommandNames[name] && featureEnabledIn(features, name) {
		return fmt.Sprintf("Choose another name, or run `gtb disable %s`.", name), true
	}

	return "", false
}

// rootNames are the words a top-level command answers to.
func rootNames(c ManifestCommand) []string {
	return append([]string{c.Name}, c.Aliases...)
}

// checkRootNames refuses a new top-level command whose name or alias a built-in
// (spec 0216, #224) or an attached command holds. One the project already has is
// left to warn.
func (g *Generator) checkRootNames() error {
	if len(g.getParentPathParts()) > 0 {
		return nil
	}

	if exists, _ := afero.Exists(g.props.FS, ManifestPathFor(g.config.Path)); !exists {
		return nil
	}

	m, err := g.loadManifest()
	if err != nil {
		return err
	}

	asked := ManifestCommand{Name: g.config.Name, Aliases: g.config.Aliases}

	var already []string

	if existing := findCommandAt(m.Commands, nil, asked.Name); existing != nil {
		already = rootNames(*existing)

		if len(asked.Aliases) == 0 {
			asked.Aliases = existing.Aliases
		}
	}

	for _, name := range rootNames(asked) {
		if slices.Contains(already, name) {
			continue
		}

		if err := checkNameFree(m, name); err != nil {
			return err
		}
	}

	g.warnBuiltinCommandName(asked, m.Properties.Features)

	return nil
}

func checkNameFree(m *Manifest, name string) error {
	if remedy, held := builtinHoldsName(name, m.Properties.Features); held {
		return errors.WithHint(errors.Wrap(ErrBuiltinCommandName, name), remedy)
	}

	if holder, held := attachmentDeclaring(m, name); held {
		return errors.WithHintf(errors.Wrap(ErrAttachedCommandName, name),
			"The attached command %s of %s has that name. Choose another name, or detach that one first.", holder.constructor, holder.module)
	}

	return nil
}

// checkEnabledFeatureNames refuses to turn on a feature whose command name a
// top-level command of the project has, as its name or an alias, or an attached
// command declares.
func checkEnabledFeatureNames(m *Manifest, after []ManifestFeature) error {
	turnedOn := func(name string) bool {
		return featureCommandNames[name] && !featureEnabledIn(m.Properties.Features, name) && featureEnabledIn(after, name)
	}

	for _, ec := range m.Properties.ExternalCommands {
		for _, a := range ec.Attach {
			if turnedOn(a.Name) {
				return errors.WithHintf(errors.Wrapf(ErrBuiltinCommandName, "the %s feature", a.Name),
					"The attached command %s has that name. Run `gtb detach command %s` before enabling the %s feature.", a.Constructor, ec.Module, a.Name)
			}
		}
	}

	for _, c := range m.Commands {
		for _, name := range rootNames(c) {
			if !turnedOn(name) {
				continue
			}

			hint := fmt.Sprintf("This project has its own %s command. Remove it, or generate it under another name, before enabling the %s feature.", name, name)
			if name != c.Name {
				hint = fmt.Sprintf("This project's %s command has the alias %s. Remove it from that command's aliases in .gtb/manifest.yaml and run gtb regenerate project before enabling the %s feature.", c.Name, name, name)
			}

			return errors.WithHint(errors.Wrapf(ErrBuiltinCommandName, "the %s feature", name), hint)
		}
	}

	return nil
}

// checkAttachedNames refuses to attach a command that declares a name a built-in
// or a top-level command of the project has (#229), or another attachment
// declares (#249).
func checkAttachedNames(attach []ManifestExternalAttach, m *Manifest) error {
	asked := map[string]string{}

	for _, a := range attach {
		if a.Name == "" {
			continue
		}

		if other, twice := asked[a.Name]; twice {
			return errors.WithHintf(errors.Wrap(ErrAttachedCommandName, a.Name), "%s and %s both have that name. %s", other, a.Constructor, attachRenamed)
		}

		asked[a.Name] = a.Constructor

		if holder, held := attachmentDeclaring(m, a.Name); held {
			return errors.WithHintf(errors.Wrap(ErrAttachedCommandName, a.Name), "The attached command %s of %s has that name. %s", holder.constructor, holder.module, attachRenamed)
		}

		if _, held := builtinHoldsName(a.Name, m.Properties.Features); held {
			hint := "Every tool has that command."
			if featureCommandNames[a.Name] {
				hint = fmt.Sprintf("Run `gtb disable %s` first.", a.Name)
			}

			return errors.WithHint(errors.Wrap(ErrBuiltinCommandName, a.Name), hint+" "+attachRenamed)
		}

		for _, c := range m.Commands {
			if !slices.Contains(rootNames(c), a.Name) {
				continue
			}

			hint := fmt.Sprintf("This project has its own %s command.", a.Name)
			if a.Name != c.Name {
				hint = fmt.Sprintf("In this project %s is an alias of its %s command.", a.Name, c.Name)
			}

			return errors.WithHint(errors.Wrap(ErrAttachedCommandName, a.Name), hint+" "+attachRenamed)
		}
	}

	return nil
}

const attachRenamed = "To attach it under another name, use `gtb attach adapter` and set the command's Use there."

type attachment struct{ module, constructor string }

// attachmentDeclaring finds the attached command that declares name.
func attachmentDeclaring(m *Manifest, name string) (holder attachment, held bool) {
	for _, ec := range m.Properties.ExternalCommands {
		for _, a := range ec.Attach {
			if a.Name == name {
				return attachment{module: ec.Module, constructor: a.Constructor}, true
			}
		}
	}

	return attachment{}, false
}

func (g *Generator) warnBuiltinCommandNames(m *Manifest) {
	for _, c := range m.Commands {
		g.warnBuiltinCommandName(c, m.Properties.Features)
	}
}

func (g *Generator) warnBuiltinCommandName(c ManifestCommand, features []ManifestFeature) {
	for _, name := range rootNames(c) {
		remedy, held := builtinHoldsName(name, features)
		if !held {
			continue
		}

		which := fmt.Sprintf("the top-level command %q", c.Name)
		if name != c.Name {
			which = fmt.Sprintf("the alias %q of %s", name, which)
		}

		g.props.Logger.Warn(fmt.Sprintf("%s shares its name with a built-in, so only one of them can run. %s", which, remedy))
	}
}
