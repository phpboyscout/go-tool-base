package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestValidateConfigSpec_FileOnlyPatterns(t *testing.T) {
	t.Parallel()

	for _, pattern := range []string{"update.**", "update.require_signature", "a.*.c", "**", "*"} {
		require.NoError(t, props.ValidateConfigSpec(props.ConfigSpec{FileOnly: []string{pattern}}), pattern)
	}

	for _, pattern := range []string{"", ".", "a..b", ".a", "a.", "a.**.b", "**.a", "a.**.**"} {
		err := props.ValidateConfigSpec(props.ConfigSpec{FileOnly: []string{pattern}})
		require.ErrorIs(t, err, props.ErrConfigFileOnly, "%q", pattern)
		assert.Contains(t, err.Error(), `"`+pattern+`"`)
	}
}

func TestConfigSpec_EnvDenials_CoverEverySectionAbove(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"update.**":                {"update.**", "update"},
		"update.require_signature": {"update.require_signature", "update"},
		"a.b.c":                    {"a.b.c", "a", "a.b"},
		"a.*.c":                    {"a.*.c", "a", "a.*"},
		"**":                       {"**"},
	}

	for pattern, want := range cases {
		got := props.ConfigSpec{FileOnly: []string{pattern}}.EnvDenials()
		assert.ElementsMatch(t, want, got, pattern)
	}
}

func TestConfigSpec_EnvDenials_SharedSectionsAppearOnce(t *testing.T) {
	t.Parallel()

	got := props.ConfigSpec{FileOnly: []string{"update.require_signature", "update.require_checksum"}}.EnvDenials()

	assert.ElementsMatch(t, []string{"update.require_signature", "update.require_checksum", "update"}, got)
}

func TestConfigSpec_EnvDenials_NoneDeclared(t *testing.T) {
	t.Parallel()

	assert.Empty(t, props.ConfigSpec{}.EnvDenials())
}
