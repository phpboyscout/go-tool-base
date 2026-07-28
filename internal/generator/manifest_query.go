package generator

import (
	"strings"

	"github.com/cockroachdb/errors"
)

func (g *Generator) FindCommandParentPath(name string) ([]string, error) {
	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	if err != nil {
		return nil, err
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

// walkCommandPath resolves path — a slice of command names from the root of
// commands down to a target — to a pointer to the target ManifestCommand, or
// nil when any segment is missing or path is empty. The returned pointer aliases
// the underlying slice element, so mutations through it (Protected, Hashes,
// Commands) persist. This is the single walk-a-known-path primitive every
// manifest find/update/remove helper is built on, so the G602 slice-index
// reasoning lives in exactly one audited place.
func walkCommandPath(commands []ManifestCommand, path []string) *ManifestCommand {
	if len(path) == 0 {
		return nil
	}

	for i := range commands {
		//nolint:gosec // G602 false positive: the len(path)==0 guard above proves path is non-empty, so path[0] and path[1:] are in range.
		if commands[i].Name != path[0] {
			continue
		}

		if len(path) == 1 {
			return &commands[i]
		}

		//nolint:gosec // G602 false positive: len(path)>1 here (len 0 and 1 handled above), so path[1:] is in range.
		return walkCommandPath(commands[i].Commands, path[1:])
	}

	return nil
}

// joinCommandPath returns a fresh parentPath+leaf slice without aliasing
// parentPath's backing array.
func joinCommandPath(parentPath []string, leaf string) []string {
	path := make([]string, 0, len(parentPath)+1)
	path = append(path, parentPath...)

	return append(path, leaf)
}

// findCommandAt returns a pointer to the command named `name` whose parent
// chain matches `parentPath` (empty parentPath means a root-level command).
func findCommandAt(commands []ManifestCommand, parentPath []string, name string) *ManifestCommand {
	return walkCommandPath(commands, joinCommandPath(parentPath, name))
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
	m, err := g.decodeManifestFile(ManifestPathFor(g.config.Path))
	if err != nil {
		return nil, err
	}

	cmd := walkCommandPath(m.Commands, joinCommandPath(g.getParentPathParts(), g.config.Name))
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

// setCommandProtection sets the Protected flag on the command addressed by
// pathParts (the "/"-split command path, e.g. "kube/ctx" -> ["kube","ctx"]).
func (g *Generator) setCommandProtection(pathParts []string, protected bool) error {
	manifestPath := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	cmd := walkCommandPath(m.Commands, pathParts)
	if cmd == nil {
		return errors.Newf("command %s not found in manifest", strings.Join(pathParts, "/"))
	}

	cmd.Protected = &protected

	return g.marshalManifestFile(manifestPath, m)
}

// removeCommand removes the child named `name` from the command tree: from the
// top level when parentPath is empty, otherwise from the command addressed by
// parentPath. Returns false when the parent or child is absent.
func removeCommand(commands *[]ManifestCommand, parentPath []string, name string) bool {
	target := commands
	if len(parentPath) > 0 {
		parent := walkCommandPath(*commands, parentPath)
		if parent == nil {
			return false
		}

		target = &parent.Commands
	}

	return removeChildByName(target, name)
}

// removeChildByName drops the first command named `name` from *commands.
func removeChildByName(commands *[]ManifestCommand, name string) bool {
	for i, cmd := range *commands {
		if cmd.Name == name {
			*commands = append((*commands)[:i], (*commands)[i+1:]...)

			return true
		}
	}

	return false
}

func (g *Generator) removeFromManifest() error {
	manifestPath := ManifestPathFor(g.config.Path)

	m, err := g.decodeManifestFile(manifestPath)
	if err != nil {
		return err
	}

	if !removeCommand(&m.Commands, g.getParentPathParts(), g.config.Name) {
		return errors.Newf("command %s not found in manifest", g.config.Name)
	}

	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	return g.marshalManifestFile(manifestPath, m)
}
