package generator

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestConflictLog_RecordsEachPathOnce(t *testing.T) {
	t.Parallel()

	var l conflictLog

	for range 2 {
		l.recordBehaviourChange(behaviourChange{Command: "lexicon", RelPath: "pkg/cmd/lexicon/cmd.go"})
		l.recordAdoptable("lexicon")
		l.recordKeep("pkg/cmd/a/cmd.go", keepReasonOverwriteDeny)
		l.recordSealed("pkg/cmd/a/main.go")
		l.recordIgnored("docs/index.md")
	}

	assert.Len(t, l.behaviourChanges, 1)
	assert.Equal(t, []string{"lexicon"}, l.adoptable)
	assert.Len(t, l.kept, 1)
	assert.Equal(t, []string{"pkg/cmd/a/main.go"}, l.sealed)
	assert.Equal(t, []string{"docs/index.md"}, l.ignored)
}

func TestReportBehaviourChanges_ListsAdoptableCommands(t *testing.T) {
	t.Parallel()

	buf := logger.NewBuffer()
	g := New(&props.Props{FS: afero.NewMemMapFs(), Logger: buf}, &Config{Path: "/work"})

	g.reportBehaviourChanges()
	assert.Empty(t, buf.Messages(), "nothing changed, nothing reported")

	g.conflicts.recordBehaviourChange(behaviourChange{Command: "zeta", RelPath: "pkg/cmd/zeta/cmd.go"})
	g.conflicts.recordBehaviourChange(behaviourChange{Command: "alpha", RelPath: "pkg/cmd/alpha/cmd.go"})
	g.conflicts.recordAdoptable("zeta")
	g.conflicts.recordAdoptable("alpha")

	g.reportBehaviourChanges()

	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "group no longer returns a usage error"))
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "2 commands could adopt the group model"))
	assert.Equal(t, "1 command", plural(1, "command"))
	assert.Equal(t, "2 commands", plural(2, "command"))
}
