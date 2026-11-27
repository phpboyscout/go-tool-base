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

// ErrCommandCycle is a manifest rebuild that met commands registering each
// other. A cobra tree cannot hold a cycle, and every later walk over the tree
// would recurse until the stack overflowed (#260), so nothing is written.
var ErrCommandCycle = errors.NewSentinel("gtb.generator.command_cycle", "commands register each other")

// ErrCommandOutsideParent is a manifest rebuild that met a registration the
// manifest cannot record: one from outside the directory that holds the
// registering command, or a second one, from another directory, of a command
// the root already reaches. Nothing is written.
var ErrCommandOutsideParent = errors.NewSentinel("gtb.generator.command_outside_parent", "command registration cannot be recorded in the manifest")

// ErrCommandMisplaced is a manifest rebuild that met a command the root reaches
// outside the directory regenerate project would put it in. Nothing is written.
var ErrCommandMisplaced = errors.NewSentinel("gtb.generator.command_misplaced", "command is not where regenerate project puts it")

// ErrCommandNotFound is a manifest rebuild that met a registration of a package
// under pkg/cmd in which the scan finds no constructor of that name. The
// manifest would drop the command and everything beneath it, so nothing is
// written.
var ErrCommandNotFound = errors.NewSentinel("gtb.generator.command_not_found", "registered command not found")

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
	dir             string
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

	if err := g.checkUnfoundRegistrations(roots); err != nil {
		return nil, err
	}

	if err := checkUnlinkedRegistrations(roots); err != nil {
		return nil, err
	}

	if err := checkPlacement(roots); err != nil {
		return nil, err
	}

	sort.Slice(roots, func(i, j int) bool {
		return roots[i].cmd.Name < roots[j].cmd.Name
	})

	commands := make([]ManifestCommand, 0, len(roots))

	for _, root := range roots {
		if root.cmd.Name == "root" {
			// If we found the root command, add its children as top-level commands
			for _, child := range root.children {
				commands = append(commands, g.buildCmdTree(child))
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

	// Every command here is walked, not only the roots, because commands that
	// register each other across levels leave this directory with no root.
	if err := checkCycles(allCommands); err != nil {
		return nil, err
	}

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

// checkUnlinkedRegistrations stops the scan at a command the root reaches that
// registers a scanned command not linked beneath it: one outside the directory
// that holds the registering command, or one another command already holds.
// The manifest has nowhere to put that registration, and without the check it
// vanished from the manifest and the next regenerate project dropped it from
// the tool (#256).
func checkUnlinkedRegistrations(roots []*commandEntry) error {
	tree := indexScannedTree(roots)

	for _, entry := range tree.reachable {
		for _, constructor := range entry.subcommandFuncs {
			child, ok := tree.scanned[constructor]
			if !ok || child == entry || slices.Contains(entry.children, child) {
				continue
			}

			if holder, ok := tree.placedUnder[child]; ok {
				return registeredTwice(entry, child, holder)
			}

			// An orphan's claim only matters when it took a package already
			// where this registration needs it, so the move hint would say
			// to move it where it is.
			if claimer, ok := tree.claimedBy[child]; ok && placedDir(entry, child.cmd.Name) == child.dir {
				return registeredTwice(entry, child, claimer)
			}

			return registeredFromAnotherSubtree(entry, child)
		}
	}

	return nil
}

type scannedTree struct {
	scanned     map[string]*commandEntry
	placedUnder map[*commandEntry]*commandEntry
	claimedBy   map[*commandEntry]*commandEntry
	reachable   []*commandEntry
}

func indexScannedTree(roots []*commandEntry) scannedTree {
	tree := scannedTree{
		scanned:     make(map[string]*commandEntry),
		placedUnder: make(map[*commandEntry]*commandEntry),
		claimedBy:   make(map[*commandEntry]*commandEntry),
	}

	var collect func(parent *commandEntry, entries []*commandEntry, fromRoot bool)

	collect = func(parent *commandEntry, entries []*commandEntry, fromRoot bool) {
		for _, entry := range entries {
			if entry.constructorName != "" {
				tree.scanned[entry.constructorName] = entry
			}

			if parent != nil {
				tree.claimedBy[entry] = parent

				if fromRoot {
					tree.placedUnder[entry] = parent
				}
			}

			if fromRoot {
				tree.reachable = append(tree.reachable, entry)
			}

			collect(entry, entry.children, fromRoot)
		}
	}

	for _, root := range roots {
		collect(nil, []*commandEntry{root}, root.cmd.Name == "root")
	}

	return tree
}

// registeredTwice names the registration to keep: the one regenerate project
// would scaffold the package beneath. It judges the two registrations the scan
// met, and a third registrant shows on the next run. Keeping a registration
// that matches neither walks into #262, so then it gives both move targets.
func registeredTwice(parent, child, holder *commandEntry) error {
	err := errors.Newf("%w: %s registers %s, which %s registers", ErrCommandOutsideParent, parent.dir, child.dir, holder.dir)

	for _, pair := range [][2]*commandEntry{{holder, parent}, {parent, holder}} {
		keep, drop := pair[0], pair[1]
		if placedDir(keep, child.cmd.Name) == child.dir {
			return errors.WithHintf(err,
				"regenerate project puts %s beneath %s, so keep that registration and remove the one in %s. "+
					"The manifest has not been changed.",
				child.dir, keep.dir, drop.dir)
		}
	}

	return errors.WithHintf(err,
		"regenerate project puts each command at pkg/cmd/<parent commands>/<name>, and neither registration "+
			"matches. Keep one and move the package to match it: to %s to keep %s's, or to %s to keep %s's. "+
			"The manifest has not been changed.",
		placedDir(holder, child.cmd.Name), holder.dir, placedDir(parent, child.cmd.Name), parent.dir)
}

// checkCycles stops the scan at a command that registers one of its own
// ancestors, naming the loop from the first command in it. It runs on each
// directory's scan, before anything else walks the tree.
func checkCycles(entries []*commandEntry) error {
	done := make(map[*commandEntry]bool)

	var path []*commandEntry

	var visit func(*commandEntry) error

	visit = func(entry *commandEntry) error {
		if done[entry] {
			return nil
		}

		if at := slices.Index(path, entry); at >= 0 {
			return commandCycle(append(path[at:], entry))
		}

		path = append(path, entry)

		for _, child := range entry.children {
			if err := visit(child); err != nil {
				return err
			}
		}

		path = path[:len(path)-1]
		done[entry] = true

		return nil
	}

	for _, entry := range entries {
		if err := visit(entry); err != nil {
			return err
		}
	}

	return nil
}

func commandCycle(loop []*commandEntry) error {
	dirs := make([]string, len(loop))
	for i, entry := range loop {
		dirs[i] = entry.dir
	}

	return errors.WithHint(errors.Newf("%w: %s", ErrCommandCycle, strings.Join(dirs, " registers ")),
		"A command cannot register one of its own ancestors. Remove one of these registrations. The manifest has not been changed.")
}

func registeredFromAnotherSubtree(parent, child *commandEntry) error {
	return errors.WithHintf(errors.Newf("%w: %s registers %s", ErrCommandOutsideParent, parent.dir, child.dir),
		"regenerate project puts each command at pkg/cmd/<parent commands>/<name>. Move %s to %s, "+
			"or register it from the command that directory belongs to. The manifest has not been changed.",
		child.dir, placedDir(parent, child.cmd.Name))
}

// checkPlacement stops the scan at a command the root reaches that is not
// where regenerate project would scaffold it beneath each command registering
// it, since that run would stub the command or list it twice (#262).
func checkPlacement(roots []*commandEntry) error {
	tree := indexScannedTree(roots)

	if err := checkRegisteredTwice(tree); err != nil {
		return err
	}

	placed := make(map[string]*commandEntry)

	held := make(map[string]*commandEntry)
	for _, entry := range tree.reachable {
		held[entry.dir] = entry
	}

	for _, parent := range tree.reachable {
		for _, child := range parent.children {
			want := placedDir(parent, child.cmd.Name)

			if child.dir != want {
				if occupant := held[want]; occupant != nil && occupant.cmd.Name != child.cmd.Name {
					return occupiedByAnother(parent, child, want, occupant, tree.placedUnder[occupant])
				}

				return misplaced(parent, child, want, held[want])
			}

			if other, ok := placed[want]; ok && other != child {
				return misplaced(parent, child, want, other)
			}

			placed[want] = child
		}
	}

	return nil
}

// checkRegisteredTwice stops at a command two commands the root reaches both
// register, before either registration is judged on its own, so the hint can
// name the one to keep rather than move a package one parent already had right.
func checkRegisteredTwice(tree scannedTree) error {
	first := make(map[*commandEntry]*commandEntry)

	for _, parent := range tree.reachable {
		for _, child := range parent.children {
			if holder, seen := first[child]; seen {
				return registeredTwice(parent, child, holder)
			}

			first[child] = parent
		}
	}

	return nil
}

func misplaced(parent, child *commandEntry, want string, occupant *commandEntry) error {
	err := errors.Newf("%w: %s registers %s as %q", ErrCommandMisplaced, parent.dir, child.dir, child.cmd.Name)

	if occupant != nil {
		return errors.WithHintf(err,
			"regenerate project puts it at %s, which already holds another command named %q. "+
				"Rename one of them, then move each package to the directory its name gives, "+
				"name its constructor NewCmd<Directory>, and update the registration in %s. "+
				"The manifest has not been changed.",
			want, occupant.cmd.Name, parent.dir)
	}

	hint := fmt.Sprintf("regenerate project puts each command at pkg/cmd/<parent commands>/<name>, so it would stub "+
		"this one at %s. Move %s there%s, and update the registration in %s.%s",
		want, child.dir, constructorRename(child, want), parent.dir, namesToFollow(child, want))

	if base := filepath.Base(child.dir); placedDir(parent, base) == child.dir && !outsidePatterns(base) {
		hint += fmt.Sprintf(" Or set the name in its Use back to %q, which keeps it where it is.", base)
	}

	return errors.WithHint(err, hint+" The manifest has not been changed.")
}

// occupiedByAnother is a command whose place holds a command of another name,
// itself out of place, so that one has to move first.
func occupiedByAnother(parent, child *commandEntry, want string, occupant, occupantParent *commandEntry) error {
	err := errors.Newf("%w: %s registers %s as %q", ErrCommandMisplaced, parent.dir, child.dir, child.cmd.Name)

	occupantWant := placedDir(occupantParent, occupant.cmd.Name)

	if occupantWant == child.dir {
		return errors.WithHintf(err,
			"regenerate project puts it at %s, which holds %q, a command it puts here. "+
				"Swap the two directories, so that %s moves to %s%s and %s moves to %s%s, "+
				"and update both registrations.%s%s The manifest has not been changed.",
			want, occupant.cmd.Name,
			child.dir, want, constructorRename(child, want),
			occupant.dir, child.dir, constructorRename(occupant, child.dir),
			namesToFollow(child, want), namesToFollow(occupant, child.dir))
	}

	return errors.WithHintf(err,
		"regenerate project puts it at %s, which holds %q, a command it puts at %s. "+
			"Move %s to %s%s first, then %s to %s%s, and update both registrations.%s%s "+
			"The manifest has not been changed.",
		want, occupant.cmd.Name, occupantWant,
		occupant.dir, occupantWant, constructorRename(occupant, occupantWant),
		child.dir, want, constructorRename(child, want),
		namesToFollow(occupant, occupantWant), namesToFollow(child, want))
}

// namesToFollow lists what else regenerate project names after a command, for
// a package moving to a directory of another name: the package clause, the
// options type, the run function and the setup.Wrap feature. Without them the
// next regenerate stubs a second run function beside the first.
func namesToFollow(entry *commandEntry, dir string) string {
	name := filepath.Base(dir)
	if filepath.Base(entry.dir) == name {
		return ""
	}

	pascal := PascalCase(name)

	return fmt.Sprintf(" In %s, also rename whichever of these it has to match %q: the package clause "+
		"to package %s, the options type to %sOptions, the run function to Run%s, and the setup.Wrap name.",
		entry.dir, name, commandPackageName(name), pascal, pascal)
}

// constructorRename says how to rename a command's constructor for the scan to
// find it at dir, which reads it as NewCmd<Directory>, or nothing when its
// name already matches.
func constructorRename(entry *commandEntry, dir string) string {
	want := "NewCmd" + PascalCase(filepath.Base(dir))

	have := entry.constructorName[strings.LastIndex(entry.constructorName, ".")+1:]
	if have == want {
		return ""
	}

	return fmt.Sprintf(" with its constructor renamed from %s to %s", have, want)
}

// checkUnfoundRegistrations stops at a command the root reaches that registers
// a NewCmd constructor from another package under pkg/cmd that the scan did not
// read as a command, such as a package moved without renaming its constructor,
// which would otherwise drop out of the manifest unannounced. A helper in the
// registrant's own package, such as newExtraCmd, is left out as before.
func (g *Generator) checkUnfoundRegistrations(roots []*commandEntry) error {
	tree := indexScannedTree(roots)

	moduleName, err := g.getModuleName()
	if err != nil {
		return errors.WithHintf(errors.Newf("%w: %s: %w", ErrCommandSourceUnreadable, filepath.Join(g.config.Path, "go.mod"), err),
			"regenerate manifest reads the module path from go.mod. Restore it and run it again. "+
				"The manifest has not been changed.")
	}

	cmdPkg := moduleName + "/pkg/cmd/"

	for _, parent := range tree.reachable {
		ownPkg, _ := splitConstructor(parent.constructorName)

		for _, constructor := range parent.subcommandFuncs {
			pkgPath, fn := splitConstructor(constructor)

			if !strings.HasPrefix(fn, "NewCmd") || !strings.HasPrefix(pkgPath, cmdPkg) ||
				pkgPath == ownPkg || tree.scanned[constructor] != nil ||
				slices.ContainsFunc(strings.Split(strings.TrimPrefix(pkgPath, cmdPkg), "/"), notScanned) {
				continue
			}

			return commandNotFound(parent, fn, filepath.Join(g.config.Path, strings.TrimPrefix(pkgPath, moduleName)))
		}
	}

	return nil
}

// splitConstructor parts a scanned constructor name, <package path>.<func>.
func splitConstructor(constructor string) (pkgPath, fn string) {
	at := strings.LastIndex(constructor, ".")
	if at < 0 {
		return "", constructor
	}

	return constructor[:at], constructor[at+1:]
}

func commandNotFound(parent *commandEntry, fn, dir string) error {
	return errors.WithHintf(
		errors.Newf("%w: %s registers %s from %s", ErrCommandNotFound, parent.dir, fn, dir),
		"The scan reads one command from each file in %s other than main.go, root.go and tests: "+
			"%s, or else the first function returning *cobra.Command. Give %s its own package at the directory "+
			"its name gives, or, when it is the package's only command, rename it to match its directory. "+
			"Move it out of root.go, or fix the registration. "+
			"The manifest has not been changed.",
		dir, "NewCmd"+PascalCase(filepath.Base(dir)), fn)
}

// placedDir is where regenerate project scaffolds a command named name beneath
// registrant. The root lives in pkg/cmd/root, but its commands sit in pkg/cmd.
func placedDir(registrant *commandEntry, name string) string {
	if registrant.cmd.Name == "root" {
		return filepath.Join(filepath.Dir(registrant.dir), name)
	}

	return filepath.Join(registrant.dir, name)
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

// notScanned names the directories under pkg/cmd that hold no commands: the
// embedded assets, helpers, and the author-owned external adapter.
func notScanned(name string) bool {
	return name == "assets" || name == "internal" || name == "external"
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
	if notScanned(entry.Name()) {
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
		dir:             dir,
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
				// A constructor registered twice is one command, recorded once.
				if child != parent && !slices.Contains(parent.children, child) {
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

	for _, child := range entry.children {
		cmd.Commands = append(cmd.Commands, g.buildCmdTree(child))
	}

	return cmd
}
