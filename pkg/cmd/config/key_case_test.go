package config_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/cmd/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newExactFileConfig is newFileConfig for a tool that declared exact keys,
// with the live Store built the way the root builds it.
func newExactFileConfig(t *testing.T, contents string) (*props.Props, afero.Fs, string) {
	t.Helper()

	p, fs, path := newFileConfig(t, contents, cfg.WithCaseSensitiveKeys())
	p.Tool.Config.KeyCase = props.KeyCaseExact

	return p, fs, path
}

// Spec 0209 D4: in a folded tool the candidate unset validates is the
// document it writes, however the key is spelled. A mis-cased unset of a
// required key used to validate the untouched file and then remove the key.
func TestCmdUnset_FoldedMisCasedRequiredKeyRefused(t *testing.T) {
	t.Parallel()

	p, fs, path := newFileConfig(t, "log:\n  level: info\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"LOG.Level"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")

	data, rerr := afero.ReadFile(fs, path)
	require.NoError(t, rerr)
	assert.Contains(t, string(data), "level")
}

// Spec 0209 D2, D4: an exact tool's file may hold two spellings of a key,
// and unset removes only the one named.
func TestCmdUnset_ExactRemovesOnlyTheNamedSpelling(t *testing.T) {
	t.Parallel()

	p, fs, path := newExactFileConfig(t, "log:\n  level: info\nFeature:\n  Enabled: true\nfeature:\n  enabled: false\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"Feature.Enabled"})
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "Enabled")
	assert.Contains(t, string(data), "enabled: false")
}

// Spec 0209 D4: in an exact tool a key spelled differently is another key, so
// unsetting it is refused as absent rather than removing a lookalike.
func TestCmdUnset_ExactMisCasedKeyNotFound(t *testing.T) {
	t.Parallel()

	p, _, _ := newExactFileConfig(t, "log:\n  level: info\nfeature:\n  enabled: true\n")

	cmd := config.NewCmdUnset(p)
	cmd.SetArgs([]string{"Feature.Enabled"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Spec 0209 D2: config edit validates the edited document in the tool's mode,
// so an exact tool can save a file holding two spellings of a key.
func TestCmdEdit_ExactSavesTwoSpellings(t *testing.T) {
	t.Parallel()

	p, fs, path := newExactFileConfig(t, "log:\n  level: info\n")
	edited := "log:\n  level: info\nFeature:\n  Enabled: true\nfeature:\n  enabled: false\n"

	cmd := config.NewCmdEdit(atATerminal(p), config.WithEditorRunner(fakeEditor(fs, edited)))
	require.NoError(t, cmd.Execute())

	data, err := afero.ReadFile(fs, path)
	require.NoError(t, err)
	assert.Equal(t, edited, string(data))
	assert.True(t, p.Config.View().GetBool("Feature.Enabled"))
	assert.False(t, p.Config.View().GetBool("feature.enabled"))
}

// Spec 0209 D4: get hands the key to the Store, so an exact tool finds the
// spelling asked for and nothing else.
func TestCmdGet_ExactReadsTheSpellingAskedFor(t *testing.T) {
	t.Parallel()

	p, _, _ := newExactFileConfig(t, "Log:\n  Level: warn\nlog:\n  level: debug\n")

	for key, want := range map[string]string{"Log.Level": "warn", "log.level": "debug"} {
		cmd := config.NewCmdGet(p, config.NewMasker())
		var buf bytes.Buffer
		cmd.SetOut(&buf)
		cmd.SetArgs([]string{key})

		require.NoError(t, cmd.Execute())
		assert.Equal(t, want, strings.TrimSpace(buf.String()), key)
	}

	cmd := config.NewCmdGet(p, config.NewMasker())
	cmd.SetArgs([]string{"LOG.LEVEL"})
	require.Error(t, cmd.Execute(), "a third spelling is another key")
}

// Spec 0209 D5: masking matches a credential key in any case, so an exact
// tool's mis-cased credential is still hidden.
func TestMasker_MatchesACredentialKeyInAnyCase(t *testing.T) {
	t.Parallel()

	m := config.NewMasker(config.WithLiteralKeys("github.auth.value"))

	assert.True(t, m.IsSensitive("GitHub.Auth.Value", "abc"))
	assert.True(t, m.IsSensitive("Service.API_Token", "abc"), "a credential-shaped leaf in any case")
}

// Spec 0209 D6: in an exact tool a framework key spelled in another case is
// one nothing reads, so validate calls it unknown and names the key meant.
func TestCmdValidate_ExactNamesTheKeyAMisCasedOneMeant(t *testing.T) {
	t.Parallel()

	p, _, _ := newExactFileConfig(t, "log:\n  level: info\nLog:\n  Format: json\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, buf.String(), "Log.Format")
	assert.Contains(t, buf.String(), `did you mean "log.format"? keys are case-sensitive in this tool`)
}

// In a folded tool keys arrive folded, so no hint is due.
func TestCmdValidate_FoldedGivesNoCaseHint(t *testing.T) {
	t.Parallel()

	p, _, _ := newFileConfig(t, "log:\n  level: info\nweirdsection:\n  typo: value\n")
	cmd := config.NewCmdValidate(p)

	var buf bytes.Buffer
	cmd.SetOut(&buf)

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, buf.String(), "case-sensitive")
}
