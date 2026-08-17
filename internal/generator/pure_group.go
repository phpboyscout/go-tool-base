package generator

import (
	"path/filepath"

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
