package generator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

// ErrNoCommandsDirectory is a manifest rebuild pointed at a directory with no
// pkg/cmd to scan.
var ErrNoCommandsDirectory = errors.NewSentinel("gtb.generator.no_commands_directory", "pkg/cmd directory not found")

// ErrCommandSourceUnreadable is a manifest rebuild that met a Go file or a
// directory under pkg/cmd it could not read or parse. Nothing is written,
// because the manifest would lose the command and everything beneath it.
var ErrCommandSourceUnreadable = errors.NewSentinel("gtb.generator.command_source_unreadable", "command source cannot be read")

func (g *Generator) RegenerateManifest(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	g.props.Logger.Info("Scanning project for commands to rebuild manifest...")

	cmdRoot := filepath.Join(g.config.Path, "pkg", "cmd")

	exists, _ := afero.Exists(g.props.FS, cmdRoot)
	if !exists {
		return ErrNoCommandsDirectory
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

		m.slashHashKeys()

		manifestExisted = true
	} else if !os.IsNotExist(readErr) {
		return errors.Newf("failed to read manifest: %w", readErr)
	}

	m.Commands = mergeScannedCommands(m.Commands, commands)

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

	m.Version.GoToolBase = g.props.Version.GetVersion()

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

// mergeScannedCommands overlays the freshly scanned command tree onto the
// existing one, carrying forward the per-command state the source cannot
// express.
//
// The scan is authoritative for structure and for everything it reads out of
// cmd.go — names, nesting, descriptions, flags, hooks, argument contracts. It
// cannot recover the bookkeeping the manifest alone holds:
//
//   - hashes, the drift-detection baseline that 0187 D10 taught doctor to read;
//   - hidden, which the template writes into cmd.go but the extractor never
//     reads back;
//   - protected, which is manifest-only and reaches no generated file at all.
//
// Assigning the scan result wholesale discarded every one of them. On keryx
// that was all 67 command hashes in a single run, leaving nothing tracked and
// the next regenerate treating every file as new (issue #16).
//
// This is the same merge-in-place shape applyRecoveredProperties already
// applies to Properties, adopted for the same reason and after the same defect
// — a manifest is authored state, and a rebuild reconciles what the code can
// prove rather than replacing what it cannot see.
//
// A command the scan found but the manifest did not know about picks up the
// zero value for each of these, which is what a newly-discovered command
// should have.
func mergeScannedCommands(existing, scanned []ManifestCommand) []ManifestCommand {
	prior := make(map[string]ManifestCommand, len(existing))
	for _, cmd := range existing {
		prior[cmd.Name] = cmd
	}

	merged := make([]ManifestCommand, 0, len(scanned))

	for _, cmd := range scanned {
		old := prior[cmd.Name]

		cmd.Hash = old.Hash
		cmd.Hashes = old.Hashes
		cmd.Hidden = old.Hidden
		cmd.Protected = old.Protected
		cmd.Commands = mergeScannedCommands(old.Commands, cmd.Commands)

		merged = append(merged, cmd)
	}

	return merged
}

func (g *Generator) scanCommands(dir string) ([]ManifestCommand, error) {
	var unread []unreadDir

	roots, err := g.scanRecursive(dir, &unread)
	if err != nil {
		return nil, err
	}

	if err := g.checkUnread(unread, roots); err != nil {
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

func (g *Generator) scanRecursive(dir string, unread *[]unreadDir) ([]*commandEntry, error) {
	entries, err := afero.ReadDir(g.props.FS, dir)
	if err != nil {
		return nil, unreadableSource(dir, err)
	}

	allCommands, err := g.scanFileSystem(dir, entries, unread)
	if err != nil {
		return nil, err
	}

	childSet := g.linkParentChild(allCommands)

	return g.findRoots(allCommands, childSet), nil
}

func (g *Generator) scanFileSystem(dir string, entries []os.FileInfo, unread *[]unreadDir) ([]*commandEntry, error) {
	var allCommands []*commandEntry

	for _, entry := range entries {
		var (
			cmds []*commandEntry
			err  error
		)

		if entry.IsDir() {
			cmds, err = g.processDirectoryEntry(dir, entry, unread)
		} else {
			cmds, err = g.processFileEntry(dir, entry)
		}

		if err != nil {
			return nil, err
		}

		allCommands = append(allCommands, cmds...)
	}

	return allCommands, nil
}

// unreadDir is a directory outside the go command's patterns that could not be
// read. Whether that loses a command depends on what registers it.
type unreadDir struct {
	path string
	err  error
}

// checkUnread stops the scan at an unread directory the tool builds: one that a
// command under the root registers.
func (g *Generator) checkUnread(unread []unreadDir, roots []*commandEntry) error {
	var registered []string

	for _, root := range roots {
		if root.cmd.Name == "root" {
			registered = append(registered, root.registers()...)
		}
	}

	for _, dir := range unread {
		pkg := "/" + g.relProjectPath(dir.path)

		for _, constructor := range registered {
			if strings.Contains(constructor, pkg+".") || strings.Contains(constructor, pkg+"/") {
				return dir.err
			}
		}

		g.props.Logger.Warn("left out a directory no command is seen to register, because it cannot be read", "path", dir.path, "error", dir.err)
	}

	return nil
}

// registers lists every constructor the command and those beneath it register.
func (e *commandEntry) registers() []string {
	constructors := slices.Clone(e.subcommandFuncs)

	for _, child := range e.children {
		constructors = append(constructors, child.registers()...)
	}

	return constructors
}

func hiddenFromGo(name string) bool {
	return strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

// outsidePatterns reports whether the go command leaves the directory out of
// patterns. A build still takes a package there that another imports.
func outsidePatterns(name string) bool {
	return name == "testdata" || hiddenFromGo(name)
}

func unreadableSource(path string, err error) error {
	return errors.WithHintf(errors.Newf("%w: %s: %w", ErrCommandSourceUnreadable, path, err),
		"Fix or remove %s and run it again. The manifest has not been changed.", path)
}

func (g *Generator) processDirectoryEntry(dir string, entry os.FileInfo, unread *[]unreadDir) ([]*commandEntry, error) {
	if entry.Name() == "assets" || entry.Name() == "internal" || entry.Name() == "external" {
		return nil, nil
	}

	path := filepath.Join(dir, entry.Name())

	cmds, err := g.scanRecursive(path, unread)
	if err != nil && outsidePatterns(entry.Name()) {
		*unread = append(*unread, unreadDir{path: path, err: err})

		return nil, nil
	}

	return cmds, err
}

func (g *Generator) processFileEntry(dir string, entry os.FileInfo) ([]*commandEntry, error) {
	if !strings.HasSuffix(entry.Name(), ".go") {
		return nil, nil
	}

	name := entry.Name()
	if name == "main.go" || name == "root.go" || name == provenanceFileName || strings.HasSuffix(name, "_test.go") || hiddenFromGo(name) {
		return nil, nil
	}

	path := filepath.Join(dir, name)

	cmd, cName, subFuncs, err := g.extractCommandMetadata(path)
	if errors.Is(err, errNoConstructor) {
		return nil, nil
	}

	if err != nil {
		return nil, unreadableSource(path, err)
	}

	return []*commandEntry{{
		cmd:             cmd,
		constructorName: cName,
		subcommandFuncs: subFuncs,
	}}, nil
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
