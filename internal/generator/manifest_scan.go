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
)

func (g *Generator) RegenerateManifest(ctx context.Context) error {
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

	// Load existing manifest to preserve properties/release_source/version.
	// If it doesn't exist yet, start from an empty manifest.
	var m Manifest

	manifestExisted := false

	if data, readErr := afero.ReadFile(g.props.FS, manifestPath); readErr == nil {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return errors.Newf("failed to unmarshal manifest: %w", err)
		}

		manifestExisted = true
	} else if !os.IsNotExist(readErr) {
		return errors.Newf("failed to read manifest: %w", readErr)
	}

	m.Commands = commands

	// The generated root cmd.go is the source of truth ONLY for name,
	// description, and release_source. Every other property is manifest-only
	// author configuration the AST cannot losslessly recover: features (the root
	// encodes only non-default Enable()/Disable() calls and never the
	// scaffold-only `keychain` feature), update policy/interval, env prefix,
	// help, telemetry, signing, ci, custom templates, docs_layout, and
	// module_published. So update only the recoverable fields in place and
	// preserve the rest — replacing the whole Properties struct silently wiped
	// them (keryx defect B; and the diataxis docs_layout regression).
	g.applyRecoveredProperties(&m, manifestExisted)

	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	if g.config.DryRun {
		return g.previewManifest(manifestPath, m)
	}

	return g.writeManifestFile(manifestPath, m)
}

// previewManifest renders the rebuilt manifest and shows what would change
// WITHOUT writing to disk, honouring --dry-run (keryx Bug 3). It mirrors the
// marshaller used by the real write path so the preview matches the bytes that
// would be written.
func (g *Generator) previewManifest(manifestPath string, m Manifest) error {
	incoming, err := marshalManifestBytes(&m)
	if err != nil {
		return err
	}

	existing, _ := afero.ReadFile(g.props.FS, manifestPath)

	diff, diffErr := generateUnifiedDiff(manifestPath, existing, incoming)

	switch {
	case diffErr != nil:
		// Diff rendering is best-effort; the contract is simply "do not write".
		g.props.Logger.Warn("dry run: manifest.yaml would be updated (diff unavailable); not written", "error", diffErr)
	case strings.TrimSpace(diff) == "":
		g.props.Logger.Info("Dry run: manifest.yaml is already up to date; no changes.")
	default:
		g.props.Logger.Info(fmt.Sprintf("Dry run: manifest.yaml would change (not written):\n%s", diff))
	}

	return nil
}

func (g *Generator) writeManifestFile(manifestPath string, m Manifest) error {
	// Ensure the .gtb directory exists before writing.
	gtbDir := filepath.Dir(manifestPath)
	if err := g.props.FS.MkdirAll(gtbDir, os.FileMode(DefaultDirMode)); err != nil {
		return errors.Newf("failed to create .gtb directory: %w", err)
	}

	g.props.Logger.Info("Writing updated manifest.yaml...")

	return g.marshalManifestFile(manifestPath, &m)
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
		g.dedupeCommandName(&c, seen)

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
		g.props.Logger.Warn("skipping orphaned command not linked in command hierarchy", "command", root.cmd.Name, "package", root.constructorName)
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
		return nil, errors.Wrap(err, "failed to read commands directory")
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
	if name == "main.go" || name == "root.go" || name == provenanceFileName || strings.HasSuffix(name, "_test.go") {
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

		g.dedupeCommandName(&childCmd, seen)

		cmd.Commands = append(cmd.Commands, childCmd)
	}

	return cmd
}

// dedupeCommandName renames c in place to "<name>-<n>" when its name has already
// been seen at this level, recording the collision warning on the command and
// logging it. seen maps each base name to how many times it has appeared.
func (g *Generator) dedupeCommandName(c *ManifestCommand, seen map[string]int) {
	count, ok := seen[c.Name]
	if !ok {
		seen[c.Name] = 1

		return
	}

	count++
	seen[c.Name] = count

	oldName := c.Name
	c.Name = fmt.Sprintf("%s-%d", oldName, count)
	c.Warning = fmt.Sprintf("Duplicate command name detected: %s. Renamed to %s to avoid collision.", oldName, c.Name)
	g.props.Logger.Warn(c.Warning)
}
