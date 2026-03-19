package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/internal/generator/templates"
)

func (g *Generator) loadManifest() (*Manifest, error) {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")
	if exists, _ := afero.Exists(g.props.FS, manifestPath); !exists {
		return nil, errors.New("manifest.yaml not found")
	}

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return nil, err
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, errors.Newf("failed to unmarshal manifest: %w", err)
	}

	return &m, nil
}

type MultilineString string

func (s MultilineString) MarshalYAML() (any, error) {
	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: string(s),
	}
	if strings.Contains(string(s), "\n") {
		node.Style = yaml.LiteralStyle
	}

	return node, nil
}

type Manifest struct {
	Properties    ManifestProperties    `yaml:"properties"`
	ReleaseSource ManifestReleaseSource `yaml:"release_source"`
	Version       ManifestVersion       `yaml:"version"`
	Commands      []ManifestCommand     `yaml:"commands,omitempty"`
}

type ManifestCommand struct {
	Name              string            `yaml:"name"`
	Description       MultilineString   `yaml:"description"`
	LongDescription   MultilineString   `yaml:"long_description,omitempty"`
	Aliases           []string          `yaml:"aliases,omitempty"`
	Hidden            bool              `yaml:"hidden,omitempty"`
	Args              string            `yaml:"args,omitempty"`
	Hash              string            `yaml:"hash,omitempty"` // Deprecated: use Hashes
	Hashes            map[string]string `yaml:"hashes,omitempty"`
	WithAssets        bool              `yaml:"with_assets,omitempty"`
	Protected         *bool             `yaml:"protected,omitempty"`
	PersistentPreRun  bool              `yaml:"persistent_pre_run,omitempty"`
	PreRun            bool              `yaml:"pre_run,omitempty"`
	MutuallyExclusive [][]string        `yaml:"mutually_exclusive,omitempty"`
	RequiredTogether  [][]string        `yaml:"required_together,omitempty"`
	Flags             []ManifestFlag    `yaml:"flags,omitempty"`
	Commands          []ManifestCommand `yaml:"commands,omitempty"`
	Warning           string            `yaml:"-"` // Used for comments
}

type ManifestFlag struct {
	Name          string          `yaml:"name"`
	Type          string          `yaml:"type"`
	Description   MultilineString `yaml:"description"`
	Persistent    bool            `yaml:"persistent,omitempty"`
	Shorthand     string          `yaml:"shorthand,omitempty"`
	Default       string          `yaml:"default,omitempty"`
	DefaultIsCode bool            `yaml:"default_is_code,omitempty"`
	Required      bool            `yaml:"required,omitempty"`
	Hidden        bool            `yaml:"hidden,omitempty"`
	Warning       string          `yaml:"-"` // Used for comments
}

func (c ManifestCommand) MarshalYAML() (any, error) {
	type manifestCommandAlias ManifestCommand

	alias := manifestCommandAlias(c)

	// Migration: If we have a single hash but no hashes map, move it to the map
	if alias.Hash != "" {
		if alias.Hashes == nil {
			alias.Hashes = make(map[string]string)
		}

		if _, ok := alias.Hashes["cmd.go"]; !ok {
			alias.Hashes["cmd.go"] = alias.Hash
		}

		alias.Hash = "" // Clear deprecated field
	}

	node := &yaml.Node{}
	if err := node.Encode(alias); err != nil {
		return nil, err
	}

	if c.Warning != "" {
		// Set comment on the name value
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "name" {
				node.Content[i+1].LineComment = c.Warning

				break
			}
		}
		// Also set on the node itself just in case
		node.HeadComment = "# " + c.Warning
	}

	return node, nil
}

func (f ManifestFlag) MarshalYAML() (any, error) {
	type manifestFlagAlias ManifestFlag

	node := &yaml.Node{}
	if err := node.Encode(manifestFlagAlias(f)); err != nil {
		return nil, err
	}

	if f.Warning != "" {
		// Find the "default" key in the mapping
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Value == "default" {
				// Add the comment to the value node
				// node.Content[i+1].LineComment = "# " + f.Warning
				// Actually, user wants it "in the manifest... include raw representation"
				// A line comment is perfect.
				node.Content[i+1].LineComment = f.Warning

				break
			}
		}
	}

	return node, nil
}

type ManifestFeature struct {
	Name    string `yaml:"name"`
	Enabled bool   `yaml:"enabled"`
}

type ManifestProperties struct {
	Name        string            `yaml:"name"`
	Repo        string            `yaml:"repo"`
	Host        string            `yaml:"host"`
	Description MultilineString   `yaml:"description"`
	Features    []ManifestFeature `yaml:"features"`
	Help        ManifestHelp      `yaml:"help,omitempty"`
}

type ManifestHelp struct {
	Type         string `yaml:"type,omitempty"`
	SlackChannel string `yaml:"slack_channel,omitempty"`
	SlackTeam    string `yaml:"slack_team,omitempty"`
	TeamsChannel string `yaml:"teams_channel,omitempty"`
	TeamsTeam    string `yaml:"teams_team,omitempty"`
}

// GetReleaseSource returns the release source type, owner, and repo.
func (m *Manifest) GetReleaseSource() (sourceType, owner, repo string) {
	return m.ReleaseSource.Type, m.ReleaseSource.Owner, m.ReleaseSource.Repo
}

type ManifestReleaseSource struct {
	Type  string `yaml:"type"`
	Host  string `yaml:"host"`
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
}

type ManifestVersion struct {
	GoToolBase string `yaml:"gtb"`
}

func (g *Generator) convertFlagsToManifest(parsedFlags []templates.CommandFlag) []ManifestFlag {
	mFlags := make([]ManifestFlag, 0, len(parsedFlags))

	for _, f := range parsedFlags {
		mFlags = append(mFlags, ManifestFlag{
			Name:          f.Name,
			Type:          f.Type,
			Description:   MultilineString(f.Description),
			Persistent:    f.Persistent,
			Shorthand:     f.Shorthand,
			Default:       f.Default,
			DefaultIsCode: f.DefaultIsCode,
			Required:      f.Required,
			Hidden:        f.Hidden,
		})
	}

	return mFlags
}

func (g *Generator) updateManifest(parsedFlags []templates.CommandFlag, hashes map[string]string) error {
	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest

	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	mFlags := g.convertFlagsToManifest(parsedFlags)

	pathParts := g.getParentPathParts()

	// Update version
	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	if len(pathParts) == 0 {
		g.updateRootCommand(&m, mFlags, hashes)
	} else if !updateCommandRecursive(&m.Commands, pathParts, g.config.Name, g.config.Short, g.config.Long, g.config.Aliases, g.config.Args, hashes, g.config.WithAssets, g.config.PersistentPreRun, g.config.PreRun, g.config.Protected, g.config.Hidden, mFlags) {
		return errors.Newf("%w: %s", ErrParentPathNotFound, g.config.Parent)
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

func (g *Generator) updateRootCommand(m *Manifest, mFlags []ManifestFlag, hashes map[string]string) {
	found := false

	for i, cmd := range m.Commands {
		if cmd.Name == g.config.Name {
			m.Commands[i].Description = MultilineString(g.config.Short)
			m.Commands[i].LongDescription = MultilineString(g.config.Long)
			m.Commands[i].Aliases = g.config.Aliases
			m.Commands[i].Args = g.config.Args
			m.Commands[i].Hashes = hashes
			m.Commands[i].WithAssets = g.config.WithAssets
			m.Commands[i].PersistentPreRun = g.config.PersistentPreRun
			m.Commands[i].PreRun = g.config.PreRun
			m.Commands[i].Hidden = g.config.Hidden

			m.Commands[i].Flags = mFlags
			if g.config.Protected != nil {
				m.Commands[i].Protected = g.config.Protected
			}

			found = true

			break
		}
	}

	if !found {
		m.Commands = append(m.Commands, ManifestCommand{
			Name:            g.config.Name,
			Description:     MultilineString(g.config.Short),
			LongDescription: MultilineString(g.config.Long),
			Aliases:         g.config.Aliases,
			Args:            g.config.Args,
			Hidden:          g.config.Hidden,
			Flags:           mFlags,
			Hashes:          hashes,
			Protected:       g.config.Protected,
			// Aliases, Hidden, MutuallyExclusive are not usually set via config flags for root/new commands yet
			// unless we add config support. For now, manifest updates preserve them if they existed?
			// But here we are creating a new one.
			// The user request implies *updating manifest to reflect correct properties* and *regenerate*.
			// It implies manually editing manifest is the source of truth for these advanced props.
			// So when updating, we should preserve existing values if we are not careful.
			// But updateRootCommand overwrites fields. I should check if I need to preserve them.
			// Currently updateRootCommand finds by name and overwrites desc, etc.
			// It does NOT touch Aliases/Hidden/MutuallyExclusive if I don't set them.
			// But for NEW commands, they will be empty/false.
			// For EXISTING commands (found=true case), I should probably not wipe them out.
		})
	}
}

func updateCommandRecursive(commands *[]ManifestCommand, parentPath []string, name, description, longDescription string, aliases []string, args string, hashes map[string]string, withAssets, persistentPreRun, preRun bool, protected *bool, hidden bool, flags []ManifestFlag) bool {
	if len(parentPath) == 0 {
		return false
	}

	for i := range *commands {
		if (*commands)[i].Name == parentPath[0] {
			return handleCommandRecursiveUpdate(commands, i, parentPath, name, description, longDescription, aliases, args, hashes, withAssets, persistentPreRun, preRun, protected, hidden, flags)
		}
	}

	return false
}

func handleCommandRecursiveUpdate(commands *[]ManifestCommand, idx int, parentPath []string, name, description, longDescription string, aliases []string, args string, hashes map[string]string, withAssets, persistentPreRun, preRun bool, protected *bool, hidden bool, flags []ManifestFlag) bool {
	if len(parentPath) == 1 {
		// Found the parent
		found := false

		for j, sub := range (*commands)[idx].Commands {
			if sub.Name == name {
				(*commands)[idx].Commands[j].Description = MultilineString(description)
				(*commands)[idx].Commands[j].LongDescription = MultilineString(longDescription)
				(*commands)[idx].Commands[j].Aliases = aliases
				(*commands)[idx].Commands[j].Args = args
				(*commands)[idx].Commands[j].Hashes = hashes
				(*commands)[idx].Commands[j].WithAssets = withAssets
				(*commands)[idx].Commands[j].PersistentPreRun = persistentPreRun
				(*commands)[idx].Commands[j].PreRun = preRun
				(*commands)[idx].Commands[j].Hidden = hidden

				(*commands)[idx].Commands[j].Flags = flags
				if protected != nil {
					(*commands)[idx].Commands[j].Protected = protected
				}

				found = true

				break
			}
		}

		if !found {
			(*commands)[idx].Commands = append((*commands)[idx].Commands, ManifestCommand{
				Name:             name,
				Description:      MultilineString(description),
				LongDescription:  MultilineString(longDescription),
				Aliases:          aliases,
				Args:             args,
				Hashes:           hashes,
				WithAssets:       withAssets,
				PersistentPreRun: persistentPreRun,
				PreRun:           preRun,
				Hidden:           hidden,
				Flags:            flags,
				Protected:        protected,
			})
		}

		return true
	}
	// Descend further
	return updateCommandRecursive(&(*commands)[idx].Commands, parentPath[1:], name, description, longDescription, aliases, args, hashes, withAssets, persistentPreRun, preRun, protected, hidden, flags)
}

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
		return nil, err
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
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
	g.syncPreRunConfig(cmd)
	g.syncDisplayConfig(cmd)

	if !g.config.WithAssets && cmd.WithAssets {
		g.config.WithAssets = true
	}

	if g.config.Args == "" && cmd.Args != "" {
		g.config.Args = cmd.Args
	}
}

func (g *Generator) syncPreRunConfig(cmd *ManifestCommand) {
	if !g.config.PersistentPreRun && cmd.PersistentPreRun {
		g.config.PersistentPreRun = true
	}

	if !g.config.PreRun && cmd.PreRun {
		g.config.PreRun = true
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
func (g *Generator) RegenerateManifest(ctx context.Context) error {
	if err := g.verifyProject(); err != nil {
		return err
	}

	g.props.Logger.Info("Scanning project for commands to rebuild manifest...")

	cmdRoot := filepath.Join(g.config.Path, "pkg", "cmd")

	exists, _ := afero.Exists(g.props.FS, cmdRoot)
	if !exists {
		return errors.New("pkg/cmd directory not found")
	}

	commands, err := g.scanCommands(cmdRoot)
	if err != nil {
		return err
	}

	manifestPath := filepath.Join(g.config.Path, ".gtb", "manifest.yaml")

	data, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(data, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	m.Commands = commands
	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	updated, err := yaml.Marshal(m)
	if err != nil {
		return errors.Newf("failed to marshal manifest: %w", err)
	}

	g.props.Logger.Info("Writing updated manifest.yaml...")

	return afero.WriteFile(g.props.FS, manifestPath, updated, DefaultFileMode)
}

type commandEntry struct {
	cmd             *ManifestCommand
	constructorName string
	subcommandFuncs []string
	children        []*commandEntry
}

func (g *Generator) scanCommands(dir string) ([]ManifestCommand, error) {
	roots, err := g.scanRecursive(dir)
	if err != nil {
		return nil, err
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].cmd.Name < roots[j].cmd.Name
	})

	commands := make([]ManifestCommand, 0, len(roots))
	seen := make(map[string]int)

	appendCmd := func(c ManifestCommand) {
		if count, ok := seen[c.Name]; ok {
			count++
			seen[c.Name] = count
			oldName := c.Name
			c.Name = fmt.Sprintf("%s-%d", c.Name, count)
			msg := fmt.Sprintf("Duplicate command name detected: %s. Renamed to %s to avoid collision.", oldName, c.Name)
			c.Warning = msg
			g.props.Logger.Warn(msg)
		} else {
			seen[c.Name] = 1
		}

		commands = append(commands, c)
	}

	for _, root := range roots {
		if root.cmd.Name == "root" {
			// If we found the root command, add its children as top-level commands
			for _, child := range root.children {
				appendCmd(g.buildCmdTree(child))
			}

			continue
		}
		// Skip orphaned commands
		g.props.Logger.Warnf("Skipping orphaned command %s (found in filesystem but not linked in command hierarchy). Package: %s", root.cmd.Name, root.constructorName)
	}

	// Sort the final list of commands
	sort.Slice(commands, func(i, j int) bool {
		return commands[i].Name < commands[j].Name
	})

	return commands, nil
}

func (g *Generator) scanRecursive(dir string) ([]*commandEntry, error) {
	entries, err := afero.ReadDir(g.props.FS, dir)
	if err != nil {
		return nil, err
	}

	allCommands := g.scanFileSystem(dir, entries)

	childSet := g.linkParentChild(allCommands)

	return g.findRoots(allCommands, childSet), nil
}

func (g *Generator) scanFileSystem(dir string, entries []os.FileInfo) []*commandEntry {
	var allCommands []*commandEntry

	for _, entry := range entries {
		if entry.IsDir() {
			if cmds := g.processDirectoryEntry(dir, entry); len(cmds) > 0 {
				allCommands = append(allCommands, cmds...)
			}
		} else {
			if cmd := g.processFileEntry(dir, entry); cmd != nil {
				allCommands = append(allCommands, cmd)
			}
		}
	}

	return allCommands
}

func (g *Generator) processDirectoryEntry(dir string, entry os.FileInfo) []*commandEntry {
	if entry.Name() == "assets" || entry.Name() == "internal" {
		return nil
	}

	children, err := g.scanRecursive(filepath.Join(dir, entry.Name()))
	if err == nil && len(children) > 0 {
		return children
	}

	return nil
}

func (g *Generator) processFileEntry(dir string, entry os.FileInfo) *commandEntry {
	if !strings.HasSuffix(entry.Name(), ".go") {
		return nil
	}

	name := entry.Name()
	if name == "main.go" || name == "root.go" || strings.HasSuffix(name, "_test.go") {
		return nil
	}

	cmd, cName, subFuncs, err := g.extractCommandMetadata(filepath.Join(dir, name))
	if err == nil {
		return &commandEntry{
			cmd:             cmd,
			constructorName: cName,
			subcommandFuncs: subFuncs,
		}
	}

	return nil
}

func (g *Generator) linkParentChild(allCommands []*commandEntry) map[*commandEntry]bool {
	cmdMap := make(map[string]*commandEntry)

	for _, entry := range allCommands {
		if entry.constructorName != "" {
			cmdMap[entry.constructorName] = entry
		}
	}

	childSet := make(map[*commandEntry]bool)

	for _, parent := range allCommands {
		for _, subFunc := range parent.subcommandFuncs {
			if child, ok := cmdMap[subFunc]; ok {
				// Avoid self-nesting or cycles
				if child != parent {
					parent.children = append(parent.children, child)
					// Mark child as handled (not a root for this level)
					childSet[child] = true
				}
			}
		}
	}

	return childSet
}

func (g *Generator) findRoots(allCommands []*commandEntry, childSet map[*commandEntry]bool) []*commandEntry {
	var roots []*commandEntry

	for _, entry := range allCommands {
		if !childSet[entry] {
			roots = append(roots, entry)
		}
	}

	return roots
}

func (g *Generator) buildCmdTree(entry *commandEntry) ManifestCommand {
	// Create a shallow copy of the command content
	cmd := *entry.cmd
	// Reset commands slice to ensure we build it fresh from our resolved children
	cmd.Commands = make([]ManifestCommand, 0, len(entry.children))

	sort.Slice(entry.children, func(i, j int) bool {
		return entry.children[i].cmd.Name < entry.children[j].cmd.Name
	})

	seen := make(map[string]int)

	for _, child := range entry.children {
		childCmd := g.buildCmdTree(child)

		if count, ok := seen[childCmd.Name]; ok {
			count++
			seen[childCmd.Name] = count
			oldName := childCmd.Name
			childCmd.Name = fmt.Sprintf("%s-%d", childCmd.Name, count)
			msg := fmt.Sprintf("Duplicate command name detected: %s. Renamed to %s to avoid collision.", oldName, childCmd.Name)
			childCmd.Warning = msg
			g.props.Logger.Warn(msg)
		} else {
			seen[childCmd.Name] = 1
		}

		cmd.Commands = append(cmd.Commands, childCmd)
	}

	return cmd
}
