package generator

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

func (g *Generator) FindCommandParentPath(name string) ([]string, error) {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return nil, errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Newf("failed to unmarshal manifest: %w", err)
	}

	path, found := findCommandPathRecursive(m.Commands, name)
	if !found {
		return nil, errors.Newf("command %s not found in manifest", name)
	}

	// The path returned includes the command name at the end.
	// Parent path is everything except the last element.
	if len(path) > 0 {
		return path[:len(path)-1], nil
	}

	return []string{}, nil
}

// findCommandAt returns a pointer to the command named `name` whose parent
// chain matches `parentPath`.  Unlike findCommandRecursive, it handles a root-
// level command where parentPath is empty.
func findCommandAt(commands []ManifestCommand, parentPath []string, name string) *ManifestCommand {
	if len(parentPath) == 0 {
		for i := range commands {
			if commands[i].Name == name {
				return &commands[i]
			}
		}

		return nil
	}

	for i := range commands {
		if commands[i].Name == parentPath[0] {
			return findCommandAt(commands[i].Commands, parentPath[1:], name)
		}
	}

	return nil
}

func findCommandPathRecursive(commands []ManifestCommand, targetName string) ([]string, bool) {
	for _, cmd := range commands {
		if cmd.Name == targetName {
			return []string{cmd.Name}, true
		}

		if subPath, found := findCommandPathRecursive(cmd.Commands, targetName); found {
			return append([]string{cmd.Name}, subPath...), true
		}
	}

	return nil, false
}

func (g *Generator) loadFlagsFromManifest() ([]CommandFlag, error) {
	cmd, err := g.findManifestCommand()
	if err != nil {
		return nil, err
	}

	g.syncConfigWithCommand(cmd)

	flags := make([]CommandFlag, 0, len(cmd.Flags))
	for _, f := range cmd.Flags {
		flags = append(flags, CommandFlag{
			Name:          f.Name,
			Type:          f.Type,
			Description:   string(f.Description),
			Persistent:    f.Persistent,
			Shorthand:     f.Shorthand,
			Default:       f.Default,
			DefaultIsCode: f.DefaultIsCode,
			Required:      f.Required,
			Hidden:        f.Hidden,
		})
	}

	return flags, nil
}

func (g *Generator) findManifestCommand() (*ManifestCommand, error) {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read manifest")
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal manifest")
	}

	pathParts := g.getParentPathParts()
	if len(pathParts) == 0 {
		for i := range m.Commands {
			if m.Commands[i].Name == g.config.Name {
				return &m.Commands[i], nil
			}
		}

		return nil, errors.New("command not found in manifest")
	}

	cmd := findCommandRecursive(m.Commands, pathParts, g.config.Name)
	if cmd == nil {
		return nil, errors.New("command not found in manifest")
	}

	return cmd, nil
}

func (g *Generator) syncConfigWithCommand(cmd *ManifestCommand) {
	g.syncDisplayConfig(cmd)

	if g.config.Args == "" && cmd.Args != "" {
		g.config.Args = cmd.Args
	}
}

func (g *Generator) syncDisplayConfig(cmd *ManifestCommand) {
	if g.config.Short == "" && cmd.Description != "" {
		g.config.Short = string(cmd.Description)
	}

	if g.config.Long == "" && cmd.LongDescription != "" {
		g.config.Long = string(cmd.LongDescription)
	}
}

func findCommandRecursive(commands []ManifestCommand, parentPath []string, name string) *ManifestCommand {
	if len(parentPath) == 0 {
		return nil
	}

	for i := range commands {
		if commands[i].Name == parentPath[0] {
			if len(parentPath) == 1 {
				for j := range commands[i].Commands {
					if commands[i].Commands[j].Name == name {
						return &commands[i].Commands[j]
					}
				}

				return nil
			}

			return findCommandRecursive(commands[i].Commands, parentPath[1:], name)
		}
	}

	return nil
}

func (g *Generator) setCommandProtection(name string, pathParts []string, protected bool) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	found := false

	if len(pathParts) <= 1 {
		// Root level command relative to the context (or just a single command name provided)
		// pathParts comes from splitting the command name, so "kube/ctx" -> ["kube", "ctx"]
		for i, cmd := range m.Commands {
			if cmd.Name == name {
				m.Commands[i].Protected = &protected
				found = true

				break
			}
		}
	} else {
		// Subcommand
		found = updateProtectionRecursive(&m.Commands, pathParts, protected)
	}

	if !found {
		return errors.Newf("command %s not found in manifest", strings.Join(pathParts, "/"))
	}

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	permission := DefaultFileMode

	if err := afero.WriteFile(g.props.FS, manifestPath, updated, os.FileMode(permission)); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

func updateProtectionRecursive(commands *[]ManifestCommand, pathParts []string, protected bool) bool {
	if len(pathParts) == 0 {
		return false
	}

	target := pathParts[0]

	for i := range *commands {
		if (*commands)[i].Name == target {
			if len(pathParts) == 1 {
				(*commands)[i].Protected = &protected

				return true
			}

			return updateProtectionRecursive(&(*commands)[i].Commands, pathParts[1:], protected)
		}
	}

	return false
}

func removeCommand(commands *[]ManifestCommand, pathParts []string, name string) bool {
	if len(pathParts) == 0 {
		for i, cmd := range *commands {
			if cmd.Name == name {
				*commands = append((*commands)[:i], (*commands)[i+1:]...)

				return true
			}
		}

		return false
	}

	return removeFromCommandRecursive(commands, pathParts, name)
}

func (g *Generator) removeFromManifest() error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	if !removeCommand(&m.Commands, g.getParentPathParts(), g.config.Name) {
		return errors.Newf("command %s not found in manifest", g.config.Name)
	}

	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	if err := afero.WriteFile(g.props.FS, manifestPath, updated, os.FileMode(DefaultFileMode)); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

func removeFromCommandRecursive(commands *[]ManifestCommand, parentPath []string, name string) bool {
	if len(parentPath) == 0 {
		return false
	}

	for i := range *commands {
		if (*commands)[i].Name == parentPath[0] {
			if len(parentPath) == 1 {
				// Found the parent
				for j, sub := range (*commands)[i].Commands {
					if sub.Name == name {
						(*commands)[i].Commands = append((*commands)[i].Commands[:j], (*commands)[i].Commands[j+1:]...)

						return true
					}
				}

				return false
			}
			// Descend further
			return removeFromCommandRecursive(&(*commands)[i].Commands, parentPath[1:], name)
		}
	}

	return false
}
