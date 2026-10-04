package setup

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Found by spec 0204's acceptance run: every init re-encoded the user's whole
// file, sorting it, changing its indentation and dropping its comments, even
// when the template had nothing to add.
func TestOpenConfigEditor_LeavesACompleteFileAsWritten(t *testing.T) {
	t.Parallel()

	p := editorProps(t)
	dir := "/home/user/.edtool"

	_, path, err := OpenConfigEditor(t.Context(), p, dir, false)
	require.NoError(t, err)

	fresh, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)

	mine := "# my settings, in my order\n" + string(fresh)
	require.NoError(t, afero.WriteFile(p.FS, path, []byte(mine), 0o600))

	_, _, err = OpenConfigEditor(t.Context(), p, dir, false)
	require.NoError(t, err)

	after, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)
	assert.Equal(t, mine, string(after), "nothing to add, so nothing is rewritten")
}

func TestOpenConfigEditor_AddsOnlyTheMissingTemplateKeys(t *testing.T) {
	t.Parallel()

	p := editorProps(t)
	dir := "/home/user/.edtool"
	path := dir + "/config.yaml"
	require.NoError(t, afero.WriteFile(p.FS, path, []byte("# keep me\nzeta: last\nlog:\n  level: debug\n"), 0o600))

	editor, _, err := OpenConfigEditor(t.Context(), p, dir, false)
	require.NoError(t, err)

	after, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)

	assert.True(t, strings.HasPrefix(string(after), "# keep me\nzeta: last\nlog:\n  level: debug\n"),
		"the user's comment, order and values stand:\n%s", after)
	assert.Equal(t, "debug", editor.View().GetString("log.level"))
	assert.True(t, editor.View().IsSet("update.policy"), "the template's missing keys are gained")
}

// One indentation throughout: what init writes matches what the format-
// preserving edits add later.
func TestOpenConfigEditor_AFreshFileIndentsByTwo(t *testing.T) {
	t.Parallel()

	p := editorProps(t)

	_, path, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err)

	fresh, err := afero.ReadFile(p.FS, path)
	require.NoError(t, err)
	assert.Contains(t, string(fresh), "log:\n  level:")
	assert.NotContains(t, string(fresh), "\n    level:")
}
