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

	if data, readErr := afero.ReadFile(g.props.FS, manifestPath); readErr == nil {
		if err := yaml.Unmarshal(data, &m); err != nil {
			return errors.Newf("failed to unmarshal manifest: %w", err)
		}
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
	rootCmdPath := filepath.Join(g.config.Path, "pkg", "cmd", "root", "cmd.go")
	if mProps, rs, err := g.extractProjectProperties(rootCmdPath); err == nil {
		m.Properties.Name = mProps.Name
		m.Properties.Description = mProps.Description
		m.ReleaseSource = *rs

		// Fall back to the (lossy) AST-derived feature set only when the existing
		// manifest recorded none.
		if len(m.Properties.Features) == 0 {
			m.Properties.Features = mProps.Features
		}
	} else {
		g.props.Logger.Warnf("Could not extract project properties from root cmd.go: %v", err)
	}

	if g.props.Version != nil {
		m.Version.GoToolBase = g.props.Version.GetVersion()
	}

	return g.writeManifestFile(manifestPath, m)
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
