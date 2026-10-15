package setup

import (
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0209 D2: init's editor agrees with the runtime Store, so a key init
// writes in an exact tool is the key the tool then reads.
func TestOpenConfigEditor_FollowsTheToolsKeyCase(t *testing.T) {
	t.Parallel()

	p := editorProps(t)
	p.Tool.Config.KeyCase = props.KeyCaseExact

	require.NoError(t, p.FS.MkdirAll("/home/user/.edtool", 0o700))
	require.NoError(t, afero.WriteFile(p.FS, "/home/user/.edtool/config.yaml",
		[]byte("Log:\n  Level: warn\nlog:\n  level: debug\n"), 0o600))

	editor, _, err := OpenConfigEditor(t.Context(), p, "/home/user/.edtool", false)
	require.NoError(t, err, "an exact tool's file may hold two spellings")

	require.NoError(t, editor.Set("Log.Level", "error"))
	assert.Equal(t, "error", editor.View().GetString("Log.Level"))
	assert.Equal(t, "debug", editor.View().GetString("log.level"))
}
