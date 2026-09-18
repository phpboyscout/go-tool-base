package templates

import (
	"bytes"
	"testing"

	"github.com/dave/jennifer/jen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCommandRegistration_EveryFlagTypeRendersConsistently pins #47: the
// flag type used to be consulted by six independent switches, and pullFlag
// (the PreRunE that copies an ancestral persistent flag into the options
// struct) had no case for the sized integers, so a uint32 flag rendered
// `opts.Workers = v` from GetString, which does not compile. Every accepted
// type is rendered as a flag and as an ancestral persistent flag, and the
// getter must match the field's Go type.
func TestCommandRegistration_EveryFlagTypeRendersConsistently(t *testing.T) {
	t.Parallel()

	want := map[string]struct{ goType, getter string }{
		"string":      {"string", "GetString"},
		"bool":        {"bool", "GetBool"},
		"int":         {"int", "GetInt"},
		"int32":       {"int32", "GetInt32"},
		"int64":       {"int64", "GetInt64"},
		"uint":        {"uint", "GetUint"},
		"uint32":      {"uint32", "GetUint32"},
		"uint64":      {"uint64", "GetUint64"},
		"float64":     {"float64", "GetFloat64"},
		"duration":    {"time.Duration", "GetDuration"},
		"stringSlice": {"[]string", "GetStringSlice"},
		"stringslice": {"[]string", "GetStringSlice"},
		"stringArray": {"[]string", "GetStringArray"},
		"stringarray": {"[]string", "GetStringArray"},
		"intSlice":    {"[]int", "GetIntSlice"},
		"intslice":    {"[]int", "GetIntSlice"},
	}

	for _, ft := range FlagTypes() {
		exp, ok := want[ft]
		require.Truef(t, ok, "flag type %q is accepted but this table does not know it", ft)

		t.Run(ft, func(t *testing.T) {
			t.Parallel()

			data := CommandData{
				Package:                  "sub",
				PascalName:               "Sub",
				Name:                     "sub",
				Short:                    "s",
				Long:                     "s",
				Flags:                    []CommandFlag{{Name: "local", Type: ft}},
				AncestralPersistentFlags: []CommandFlag{{Name: "inherited", Type: ft}},
			}

			var buf bytes.Buffer
			require.NoError(t, CommandRegistration(data).Render(&buf))
			src := buf.String()

			assert.Contains(t, src, "Inherited "+exp.goType, "options field type")
			assert.Contains(t, src, exp.getter+`("inherited")`, "PreRunE getter must match the field type")
			if exp.getter != "GetString" {
				assert.NotContains(t, src, `GetString("inherited")`, "no fall-through to GetString")
			}
		})
	}

	assert.Len(t, want, len(FlagTypes()), "the accepted set and this table must agree")
}

// TestIntDefaultRendersOnce (D11 of the v0.43.0 manual round): an int flag's
// recorded default rendered as int(int64(0)), a double conversion jen emits
// when handed an int64 literal for a narrower type; after regenerate manifest
// normalised an absent default to "0", the next regenerate rewrote every
// command file with it. The literal is emitted as the target type once.
func TestIntDefaultRendersOnce(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		flag CommandFlag
		want string
	}{
		{CommandFlag{Name: "port", Type: "int", Default: "0"}, "defaultPort = 0"},
		{CommandFlag{Name: "port", Type: "int", Default: "8080"}, "defaultPort = 8080"},
		{CommandFlag{Name: "size", Type: "int32", Default: "7"}, "defaultSize = int32(7)"},
		{CommandFlag{Name: "big", Type: "int64", Default: "9"}, "defaultBig = int64(9)"},
		{CommandFlag{Name: "n", Type: "uint32", Default: "3"}, "defaultN = uint32(3)"},
	} {
		code, ok := getConstantForFlag(tc.flag)
		require.True(t, ok, tc.flag.Name)

		src := renderStatement(t, code)
		assert.Contains(t, src, tc.want)
		assert.NotContains(t, src, "int(int64(")
	}
}

func renderStatement(t *testing.T, code jen.Code) string {
	t.Helper()

	f := jen.NewFile("x")
	f.Const().Defs(code)

	var buf bytes.Buffer
	require.NoError(t, f.Render(&buf))

	return buf.String()
}
