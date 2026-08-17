package generator

import (
	"path/filepath"
	"strings"

	"github.com/dave/dst/decorator"
	"github.com/spf13/afero"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
)

// runStubSentinels are the two bodies the generator itself writes for a run
// function. A stub returning either is one nobody has touched.
var runStubSentinels = []string{"ErrRunSubCommand", "ErrNotImplemented"}

// pureGroup reports whether a command only groups its subcommands, and so has no
// run logic of its own to call.
//
// The decision is about the run function's BODY, not about whether main.go
// exists. Deciding on file presence is what made a built binary's exit code
// depend on .gtb/ignore (issue #22): the same command answered a bare invocation
// with exit 0 or exit 2 according to whether a seal happened to have stopped
// main.go being created. A body, by contrast, is the developer stating intent.
//
// A group is pure when nothing defines its run function, or when what defines it
// is a stub the generator wrote and nobody has since changed. Anything else is a
// WORKING group: it keeps the wiring it has today, because it may legitimately
// take positional arguments and only its author knows whether it does.
//
// Note what this deliberately does not attempt. A parent whose body only prints
// its own verb list is semantically pure and is classified working, because
// recognising a "printer-shaped" body means guessing at intent, and the
// alternative — rewriting a developer-owned main.go — is a licence this does not
// take. Such a group is offered the change instead, in the run summary.
func (g *Generator) pureGroup(cmdDir string, data templates.CommandData) bool {
	if !data.HasSubcommands {
		return false
	}

	mainFile := filepath.Join(cmdDir, "main.go")

	exists, err := afero.Exists(g.props.FS, mainFile)
	if err != nil || !exists {
		// Absent, or unreadable in a way that makes the question unanswerable:
		// either way nothing there defines a run function.
		return !exists
	}

	src, err := afero.ReadFile(g.props.FS, mainFile)
	if err != nil {
		return false
	}

	f, err := decorator.Parse(src)
	if err != nil {
		// Unparseable: treat it as working. Guessing wrong in this direction
		// leaves today's wiring in place, which at worst is unchanged
		// behaviour; guessing wrong the other way silently drops a RunE the
		// developer may depend on.
		return false
	}

	runFunc := "Run" + data.PascalName

	if !declaresFunc(f, runFunc) {
		return true
	}

	return isUntouchedStub(f, runFunc, runStubSentinels...)
}

// mainFileHasContent reports whether main.go would define anything at all.
//
// Every command used to define at least Run<Name>, so the question never arose.
// A pure group has no run function, which leaves main.go holding only the hooks
// the command asked for — and with none of those, an empty file.
func mainFileHasContent(data templates.CommandData) bool {
	return !data.PureGroup || data.PersistentPreRun || data.PreRun
}

// noteGroupBehaviourChange records that this run is about to change what a
// command does, not merely how its registration file is written.
//
// The transition is detectable only from the cmd.go already on disk: one that
// wires RunE to Run<Name> for a command now classified pure is one whose built
// binary answered a bare invocation with the usage error, and will now answer it
// with usage and success. Once rewritten, nothing distinguishes it from a command
// generated into the new model — which is exactly why this reports once and then
// stays quiet forever.
//
// A working group is also noted here, as adoptable: it keeps its own behaviour,
// so the developer never hears about the new model unless something tells them.
func (g *Generator) noteGroupBehaviourChange(cmdDir string, data templates.CommandData) {
	if !data.HasSubcommands {
		return
	}

	relPath := g.relProjectPath(filepath.Join(cmdDir, "cmd.go"))

	if !data.PureGroup {
		g.conflicts.recordAdoptable(data.Name)

		return
	}

	src, err := afero.ReadFile(g.props.FS, filepath.Join(cmdDir, "cmd.go"))
	if err != nil {
		// No cmd.go yet: a fresh command has never had the old behaviour, and
		// announcing a change would be announcing a fiction.
		return
	}

	if !strings.Contains(string(src), "Run"+data.PascalName+"(cmd.Context()") {
		return
	}

	g.conflicts.recordBehaviourChange(behaviourChange{
		Command: data.Name,
		RelPath: relPath,
		Was:     "exit 2 (subcommand required)",
		Now:     "exit 0 (usage), exit 2 on an unknown subcommand",
		KeepOld: "give Run" + data.PascalName +
			" a body returning errorhandling.ErrRunSubCommand",
	})
}
