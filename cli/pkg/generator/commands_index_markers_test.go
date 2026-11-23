package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestCommandsIndexRegion(t *testing.T) {
	t.Parallel()

	const (
		s = commandsIndexMarkerStart
		e = commandsIndexMarkerEnd
	)

	fenced := "```md\n" + s + "\n" + e + "\n```\n"

	for name, tc := range map[string]struct {
		content    string
		start, end int
		ok         bool
	}{
		"both markers":                   {s + "\n| t |\n" + e + "\n", 0, len(s) + 7, true},
		"indented markers":               {"  " + s + "\n" + e + "\n", 2, len(s) + 3, true},
		"end before start":               {e + "\n" + s + "\n", 0, -1, false},
		"a second start keeps the first": {s + "\n" + s + "\n" + e + "\n", 0, 2*len(s) + 2, true},
		"no end":                         {s + "\n", 0, -1, false},
		"markers only in a fence":        {fenced, -1, -1, false},
		"markers only in a ~~~":          {"~~~\n" + s + "\n" + e + "\n~~~\n", -1, -1, false},
		"a fence before the real":        {fenced + s + "\n" + e + "\n", len(fenced), len(fenced) + len(s) + 1, true},
		"a marker inside prose":          {"see " + s + " and " + e + "\n", -1, -1, false},
		"a ``` inside ~~~ is text":       {"~~~\n```\n~~~\n" + s + "\n" + e + "\n", 12, 13 + len(s), true},
		"an unclosed fence hides":        {"```\n" + s + "\n" + e + "\n", -1, -1, false},
		"a short closer stays inside":    {"````\n```\n" + s + "\n" + e + "\n````\n", -1, -1, false},
		"a closer with text stays":       {"```\n``` x\n" + s + "\n" + e + "\n```\n", -1, -1, false},
		"a longer closer closes":         {"```\n`````\n" + s + "\n" + e + "\n", 10, 11 + len(s), true},
		"inline code opens no fence":     {"```x```\n" + s + "\n" + e + "\n", 8, 9 + len(s), true},
		"a ~~~ info string holds a `":    {"~~~ a`b\n" + s + "\n" + e + "\n~~~\n", -1, -1, false},
		"two backticks open no fence":    {"``\n```\n" + s + "\n" + e + "\n```\n", -1, -1, false},
		"two tildes open no fence":       {"~~\n~~~\n" + s + "\n" + e + "\n~~~\n", -1, -1, false},
		"an inline end is prose":         {s + "\nsee " + e + " here\n" + e + "\n", 0, len(s + "\nsee " + e + " here\n"), true},
	} {
		start, end, ok := commandsIndexRegion(tc.content)
		assert.Equal(t, tc.ok, ok, name)

		if tc.ok {
			assert.Equal(t, []int{tc.start, tc.end}, []int{start, end}, name)
		}
	}
}

// The ownership test is main's: the first start before the first end,
// wherever they sit.
func TestHasCommandsIndexMarkers(t *testing.T) {
	t.Parallel()

	const (
		s = commandsIndexMarkerStart
		e = commandsIndexMarkerEnd
	)

	assert.True(t, hasCommandsIndexMarkers("Table: "+s+" "+e))
	assert.True(t, hasCommandsIndexMarkers("```\n"+s+"\n"+e+"\n"))
	assert.False(t, hasCommandsIndexMarkers(e+"\n"+s+"\n"+e+"\n"), "the first end precedes the start")
	assert.False(t, hasCommandsIndexMarkers(s+"\n"))
	assert.False(t, hasCommandsIndexMarkers(e+"\n"))
}

// A page that quotes the markers in a code fence keeps the example: the table
// goes between the real markers below it.
func TestMergeCommandsIndex_KeepsQuotedMarkers(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop()}, config: &Config{Path: "/work"}}
	path := filepath.Join("/work", skeletonCommandsIndex)

	example := "Wrap the table like this:\n\n```md\n" + commandsIndexMarkerStart + "\n" + commandsIndexMarkerEnd + "\n```\n\n"
	page := "# Commands\n\n" + example + commandsIndexMarkerStart + "\n| old |\n" + commandsIndexMarkerEnd + "\n"
	require.NoError(t, afero.WriteFile(fs, path, []byte(page), 0o644))

	got, write := g.mergeCommandsIndex(path, skeletonCommandsIndex, "| new |\n")

	require.True(t, write)
	assert.Equal(t, "# Commands\n\n"+example+commandsIndexMarkerStart+"\n| new |\n"+commandsIndexMarkerEnd+"\n", got)
}

// Reordered markers are not a region: the page is preserved, not duplicated.
func TestMergeCommandsIndex_ReorderedMarkersArePreserved(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop()}, config: &Config{Path: "/work"}}
	path := filepath.Join("/work", skeletonCommandsIndex)

	page := "# Commands\n\nprose\n" + commandsIndexMarkerEnd + "\n| old |\n" + commandsIndexMarkerStart + "\n"
	require.NoError(t, afero.WriteFile(fs, path, []byte(page), 0o644))

	_, write := g.mergeCommandsIndex(path, skeletonCommandsIndex, "| new |\n")

	assert.False(t, write)
}

// Prose quoting the start marker inline is not the region's start, so the
// prose between it and the real markers survives the splice.
func TestMergeCommandsIndex_InlineQuotedStartIsProse(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop()}, config: &Config{Path: "/work"}}
	path := filepath.Join("/work", skeletonCommandsIndex)

	prose := "# Commands\n\nThe table sits after " + commandsIndexMarkerStart + " below.\n\nKeep this.\n\n"
	page := prose + commandsIndexMarkerStart + "\n| old |\n" + commandsIndexMarkerEnd + "\ntail\n"
	require.NoError(t, afero.WriteFile(fs, path, []byte(page), 0o644))

	got, write := g.mergeCommandsIndex(path, skeletonCommandsIndex, "| new |\n")

	require.True(t, write)
	assert.Equal(t, prose+commandsIndexMarkerStart+"\n| new |\n"+commandsIndexMarkerEnd+"\ntail\n", got)
}

// A quoted example inside a longer fence keeps its markers, whatever a
// shorter fence inside it does.
func TestMergeCommandsIndex_KeepsMarkersInANestedFence(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	g := &Generator{props: &props.Props{FS: fs, Logger: logger.NewNoop()}, config: &Config{Path: "/work"}}
	path := filepath.Join("/work", skeletonCommandsIndex)

	example := "# C\n````md\n```\n" + commandsIndexMarkerStart + "\nEXAMPLE\n" + commandsIndexMarkerEnd + "\n```\n````\n"
	page := example + commandsIndexMarkerStart + "\n| old |\n" + commandsIndexMarkerEnd + "\ntail\n"
	require.NoError(t, afero.WriteFile(fs, path, []byte(page), 0o644))

	got, write := g.mergeCommandsIndex(path, skeletonCommandsIndex, "| new |\n")

	require.True(t, write)
	assert.Equal(t, example+commandsIndexMarkerStart+"\n| new |\n"+commandsIndexMarkerEnd+"\ntail\n", got)
}
