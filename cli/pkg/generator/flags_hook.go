package generator

import (
	"strings"

	"gitlab.com/phpboyscout/go-tool-base/cli/pkg/generator/templates"
)

// flagSetClash is a declared flag whose options field is the one the flags hook
// adds, and the command that declares it.
type flagSetClash struct {
	flag    string
	command string
}

// CheckFlagsHooks refuses a manifest in which a command with a flags hook
// declares or inherits a flag stored in the field the hook adds (spec 0224 D4).
func CheckFlagsHooks(m *Manifest) error {
	for i := range m.Commands {
		if err := checkFlagsHook(&m.Commands[i], nil, nil); err != nil {
			return err
		}
	}

	return nil
}

// checkFlagsHook checks the command being generated with flags, and the
// descendants that would inherit them.
func (g *Generator) checkFlagsHook(flags []CommandFlag) error {
	parentPath := g.getParentPathParts()

	cmd := ManifestCommand{
		Name:      g.config.Name,
		FlagsHook: g.config.FlagsHook,
		Flags:     g.convertFlagsToManifest(g.convertFlagsToTemplate(flags)),
	}

	var inherited *flagSetClash

	if m := g.readManifestQuiet(); m != nil {
		if recorded := walkCommandPath(m.Commands, joinCommandPath(parentPath, cmd.Name)); recorded != nil {
			cmd.Commands = recorded.Commands
		}

		for depth := range parentPath {
			ancestorPath := parentPath[:depth+1]

			recorded := walkCommandPath(m.Commands, ancestorPath)
			if recorded == nil {
				continue
			}

			ancestor := *recorded
			ancestor.Commands = nil

			if err := checkFlagsHook(&ancestor, parentPath[:depth], inherited); err != nil {
				return err
			}

			if inherited == nil {
				inherited = findFlagSetClash(&ancestor, ancestorPath, true)
			}
		}
	}

	return checkFlagsHook(&cmd, parentPath, inherited)
}

func checkFlagsHook(cmd *ManifestCommand, parentPath []string, inherited *flagSetClash) error {
	path := joinCommandPath(parentPath, cmd.Name)

	if cmd.FlagsHook {
		clash := findFlagSetClash(cmd, path, false)
		if clash == nil {
			clash = inherited
		}

		if clash != nil {
			return rejectf("FlagsHook",
				"command "+strings.Join(path, "/")+" has a flags hook, whose options field "+templates.FlagSetField+
					" the flag of command "+clash.command+" would also take; rename the flag or turn the hook off",
				"--"+clash.flag)
		}
	}

	if inherited == nil {
		inherited = findFlagSetClash(cmd, path, true)
	}

	for i := range cmd.Commands {
		if err := checkFlagsHook(&cmd.Commands[i], path, inherited); err != nil {
			return err
		}
	}

	return nil
}

func findFlagSetClash(cmd *ManifestCommand, path []string, persistentOnly bool) *flagSetClash {
	for _, f := range cmd.Flags {
		if persistentOnly && !f.Persistent {
			continue
		}

		if templates.OptionsField(f.Name) == templates.FlagSetField {
			return &flagSetClash{flag: f.Name, command: strings.Join(path, "/")}
		}
	}

	return nil
}
