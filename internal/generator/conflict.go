package generator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/pkg/utils"
)

// conflictOutcome is what the shared resolver decided for a single generated
// file that already exists on disk.
type conflictOutcome int

const (
	// conflictWrite: render and write. Either the file matches its recorded
	// hash, has no recorded hash, or the user/config approved the overwrite.
	conflictWrite conflictOutcome = iota
	// conflictKeep: the file has diverged and the developer chose to keep it.
	// The run continues; the file is left exactly as it is.
	conflictKeep
	// conflictIgnored: a .gtb/ignore rule covers the file. It is never
	// rendered, compared, prompted about or written.
	conflictIgnored
)

// Reasons a diverged file was kept, as they appear in the end-of-run summary.
const (
	keepReasonDeclined       = "declined at the prompt"
	keepReasonOverwriteDeny  = "--overwrite deny"
	keepReasonNonInteractive = "no terminal to prompt on"
)

// conflictDecision is the resolver's answer for one file.
type conflictDecision struct {
	Outcome conflictOutcome
	// Reason names why a diverged file was kept. Empty for the other outcomes.
	Reason string
	// RecordHash is the hash the manifest should carry for this file. It is
	// empty for conflictWrite, where the caller records the hash of what it
	// actually wrote.
	RecordHash string
}

// Write reports whether the caller should render and write the file.
func (d conflictDecision) Write() bool { return d.Outcome == conflictWrite }

// keptFile is one entry in the end-of-run summary.
type keptFile struct {
	RelPath string
	Reason  string
	// Note carries a structural consequence of keeping this file — a
	// subcommand present in the manifest but not registered in the kept
	// file (D12). Empty when there is none.
	Note string
}

// pendingChildCheck is a kept parent cmd.go whose registrations are compared
// against its manifest children once the run has finished.
//
// It cannot be evaluated when the file is kept: the children have not been
// visited yet, and each child's registration step patches its parent's AST —
// which is how a kept parent normally ends up correctly wired despite not
// being re-rendered. Checking early would report every kept parent as broken.
type pendingChildCheck struct {
	RelPath  string
	CmdDir   string
	Children []string
}

// conflictLog accumulates what a run left alone, so the summary can report it
// once at the end rather than the developer having to reconstruct it from
// interleaved per-file warnings.
type conflictLog struct {
	kept          []keptFile
	ignored       []string
	pendingChecks []pendingChildCheck
}

func (l *conflictLog) recordKeep(relPath, reason string) {
	for _, k := range l.kept {
		if k.RelPath == relPath {
			return
		}
	}

	l.kept = append(l.kept, keptFile{RelPath: relPath, Reason: reason})
}

func (l *conflictLog) recordIgnored(relPath string) {
	for _, p := range l.ignored {
		if p == relPath {
			return
		}
	}

	l.ignored = append(l.ignored, relPath)
}

// wasKept reports whether relPath was kept during this run.
func (l *conflictLog) wasKept(relPath string) bool {
	for _, k := range l.kept {
		if k.RelPath == relPath {
			return true
		}
	}

	return false
}

// annotate attaches a structural note to an already-recorded kept file.
func (l *conflictLog) annotate(relPath, note string) {
	for i, k := range l.kept {
		if k.RelPath == relPath {
			l.kept[i].Note = note

			return
		}
	}
}

func (l *conflictLog) reset() {
	l.kept = nil
	l.ignored = nil
	l.pendingChecks = nil
}

// ignoreRules returns the project's .gtb/ignore rules, loading them once per
// generator. Both write paths resolve against the same set (D4), so a rule
// cannot cover a skeleton file and miss a command file rendered in the same run.
func (g *Generator) ignoreRules() *IgnoreRules {
	if g.rules == nil {
		g.rules = LoadIgnoreRules(g.props.FS, g.config.Path)
	}

	return g.rules
}

// relProjectPath converts an absolute (or project-prefixed) path into the
// project-relative, slash-separated form that ignore rules match against and
// that `gtb ignore add` writes. Falling back to the input keeps a hint
// printable even if the path is somehow outside the project.
func (g *Generator) relProjectPath(fullPath string) string {
	rel, err := filepath.Rel(g.config.Path, fullPath)
	if err != nil {
		return filepath.ToSlash(fullPath)
	}

	return filepath.ToSlash(rel)
}

// resolveConflict is the single conflict decision for both write paths — the
// skeleton walk and the per-command registration files. It decides whether an
// existing generated file may be overwritten, and what hash the manifest should
// record for it.
//
// Precedence is deliberate and is the one signing_goreleaser.go already states:
// .gtb/ignore outranks everything, including --force and --overwrite allow. A
// developer who declared a file hands-off said so more deliberately than a flag
// on one invocation.
//
// It never returns an error. A declined file is a skip, not a failure (D2):
// callers leave it alone and carry on to the next file.
func (g *Generator) resolveConflict(fullPath, relPath, storedHash string, newContent []byte) conflictDecision {
	if g.ignoreRules().IsIgnored(relPath) {
		g.props.Logger.Debug("ignored by .gtb/ignore, leaving untouched", "path", relPath)
		g.conflicts.recordIgnored(relPath)

		// Track the hash of what is actually on disk, so removing the rule
		// later resumes conflict detection against current content rather than
		// against a baseline that predates every edit the rule allowed.
		existing, err := afero.ReadFile(g.props.FS, fullPath)
		if err != nil {
			return conflictDecision{Outcome: conflictIgnored}
		}

		return conflictDecision{Outcome: conflictIgnored, RecordHash: calculateHash(existing)}
	}

	existing, err := afero.ReadFile(g.props.FS, fullPath)
	if err != nil {
		// Nothing on disk to protect.
		return conflictDecision{Outcome: conflictWrite}
	}

	if storedHash == "" || storedHash == calculateHash(existing) || g.config.Force {
		return conflictDecision{Outcome: conflictWrite}
	}

	g.props.Logger.Warn("conflict detected: file has been manually modified",
		"path", relPath, "hint", ignoreConflictHint(relPath))

	if g.promptOverwrite(fullPath, existing, newContent) {
		g.props.Logger.Warn("overwriting modified file", "path", relPath)

		return conflictDecision{Outcome: conflictWrite}
	}

	reason := g.keepReason()

	g.props.Logger.Warn("skipping overwrite", "path", relPath, "reason", reason)
	g.conflicts.recordKeep(relPath, reason)

	// The stored hash is preserved rather than refreshed (D3): recording the
	// on-disk hash would silently adopt the edit as the new baseline, so the
	// file would never conflict again and the next run would destroy it
	// without asking.
	return conflictDecision{Outcome: conflictKeep, Reason: reason, RecordHash: storedHash}
}

// keepReason names why the resolver declined an overwrite, for the summary.
func (g *Generator) keepReason() string {
	if g.config.Overwrite == "deny" {
		return keepReasonOverwriteDeny
	}

	if g.isNonInteractive() {
		return keepReasonNonInteractive
	}

	return keepReasonDeclined
}

// reportConflicts prints the end-of-run summary: what was kept and how to make
// it permanent, plus a count of what an ignore rule already covers (D8).
func (g *Generator) reportConflicts() {
	g.evaluateChildChecks()

	kept, ignored := g.conflicts.kept, g.conflicts.ignored

	for _, path := range ignored {
		g.props.Logger.Debug("ignored (covered by .gtb/ignore)", "path", path)
	}

	if len(kept) == 0 && len(ignored) == 0 {
		return
	}

	g.props.Logger.Info(pluraliseConflictSummary(len(ignored), len(kept)))

	sorted := append([]keptFile{}, kept...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RelPath < sorted[j].RelPath })

	for _, k := range sorted {
		g.props.Logger.Warn("kept your version",
			"path", k.RelPath,
			"reason", k.Reason,
			"remedy", "gtb ignore add "+k.RelPath)

		if k.Note != "" {
			g.props.Logger.Warn("kept file is out of step with the manifest", "path", k.RelPath, "detail", k.Note)
		}
	}
}

// pluraliseConflictSummary renders the one-line summary counts, e.g.
// "3 files ignored, 1 file kept".
func pluraliseConflictSummary(ignored, kept int) string {
	var parts []string

	if ignored > 0 {
		parts = append(parts, fmt.Sprintf("%s ignored", plural(ignored, "file")))
	}

	if kept > 0 {
		parts = append(parts, fmt.Sprintf("%s kept", plural(kept, "file")))
	}

	return strings.Join(parts, ", ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}

	return fmt.Sprintf("%d %ss", n, noun)
}

// recordChildCheck queues a kept parent cmd.go for the end-of-run registration
// check (D12). Queuing rather than checking is the point: see pendingChildCheck.
func (g *Generator) recordChildCheck(cmdDir string, cmd ManifestCommand) {
	if len(cmd.Commands) == 0 {
		return
	}

	relPath := g.relProjectPath(filepath.Join(cmdDir, "cmd.go"))
	if !g.conflicts.wasKept(relPath) {
		return
	}

	children := make([]string, 0, len(cmd.Commands))
	for _, child := range cmd.Commands {
		children = append(children, child.Name)
	}

	g.conflicts.pendingChecks = append(g.conflicts.pendingChecks, pendingChildCheck{
		RelPath:  relPath,
		CmdDir:   cmdDir,
		Children: children,
	})
}

// evaluateChildChecks compares each kept parent's final cmd.go against its
// manifest children and annotates the summary with any child it does not
// register (D12).
//
// Keeping a parent's cmd.go is legitimate, and in the ordinary case the child's
// own registration step patches the kept file so nothing is missing. When that
// repair does not happen — an unparseable file, a missing constructor — the
// project will not build, and saying which subcommand is adrift beats leaving
// the compiler to report what regenerate already knew.
func (g *Generator) evaluateChildChecks() {
	for _, check := range g.conflicts.pendingChecks {
		cmdPath := filepath.Join(check.CmdDir, "cmd.go")

		_, _, registered, err := g.extractCommandMetadata(cmdPath)
		if err != nil {
			g.props.Logger.Debug("could not parse kept command file to check registrations",
				"path", check.RelPath, "error", err)

			continue
		}

		var missing []string

		for _, child := range check.Children {
			if !registersChild(registered, child) {
				missing = append(missing, child)
			}
		}

		if len(missing) == 0 {
			continue
		}

		g.conflicts.annotate(check.RelPath, fmt.Sprintf(
			"%s in the manifest but not registered in the file you kept — the project may not build",
			quotedList(missing)))
	}
}

// registersChild reports whether any registration extracted from a parent's
// cmd.go refers to the child's constructor. Matching on the constructor name
// alone is deliberate: the package path in an extracted registration varies
// with how the import was aliased, while NewCmd<Pascal> does not.
func registersChild(registered []string, child string) bool {
	constructor := "." + "NewCmd" + PascalCase(child)

	for _, fq := range registered {
		if strings.HasSuffix(fq, constructor) {
			return true
		}
	}

	return false
}

// quotedList renders subcommand names for the summary, e.g.
// `subcommand 'build'` or `subcommands 'build', 'watch'`.
func quotedList(names []string) string {
	quoted := make([]string, 0, len(names))
	for _, n := range names {
		quoted = append(quoted, "'"+n+"'")
	}

	noun := "subcommands"
	if len(names) == 1 {
		noun = "subcommand"
	}

	return noun + " " + strings.Join(quoted, ", ")
}

// isNonInteractive reports whether the generator is running without a usable
// controlling terminal, so interactive huh prompts must be skipped rather than
// attempted.
//
// It agrees with the rest of the toolchain on what "CI" means
// (pkg/cmd/root.isCIEnvironment): the --ci flag and `ci` config key count, not
// only the bare CI=true environment variable. A --ci run in a terminal used to
// prompt anyway, because this check read the environment and nothing else.
//
// It also honours the explicit GTB_NON_INTERACTIVE=true opt-out and, finally,
// the absence of a TTY on stdin.
func (g *Generator) isNonInteractive() bool {
	if os.Getenv("GTB_NON_INTERACTIVE") == "true" {
		return true
	}

	if os.Getenv("CI") == "true" {
		return true
	}

	if g.ciConfigured() {
		return true
	}

	if !utils.IsInteractive() {
		return true
	}

	// stdin looks like a terminal, but huh/bubbletea drives the controlling
	// terminal (/dev/tty on unix, the console on Windows), not stdin. In some
	// headless containers a char-device stdin (e.g. /dev/null) coexists with no
	// attachable controlling terminal, so probe it directly and skip cleanly
	// rather than letting huh fail with an "open /dev/tty" error (issue #6.2).
	return !controllingTerminalAvailable()
}

// ciConfigured reports the resolved `ci` config key, which the --ci persistent
// flag feeds. Absent props or store means "not CI" rather than a panic: the
// generator is constructed directly in tests that wire neither.
func (g *Generator) ciConfigured() bool {
	if g.props == nil || g.props.Config == nil {
		return false
	}

	return g.props.Config.View().GetBool("ci")
}
