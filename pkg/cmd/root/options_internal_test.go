package root

import (
	"testing"

	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

func changedFlag(t *testing.T, name, value string) *pflag.Flag {
	t.Helper()

	fs := pflag.NewFlagSet(name, pflag.ContinueOnError)
	fs.String(name, "", "")

	if value != "" {
		_ = fs.Set(name, value)
	}

	return fs.Lookup(name)
}

func TestApplyRootOptions_NilOptionIgnored(t *testing.T) {
	t.Parallel()

	o := applyRootOptions([]RootOption{nil, WithConfigPaths("a", "b")})
	assert.Equal(t, []string{"a", "b"}, o.configPaths)
}

func TestWithBoundFlags_SkipsNil(t *testing.T) {
	t.Parallel()

	flag := changedFlag(t, "x", "v")
	o := applyRootOptions([]RootOption{WithBoundFlags(map[string]*pflag.Flag{
		"a": flag,
		"b": nil,
	})})

	assert.Contains(t, o.boundFlags, "a")
	assert.NotContains(t, o.boundFlags, "b")
}

func TestWithConventionBoundFlags_NilSet(t *testing.T) {
	t.Parallel()

	o := applyRootOptions([]RootOption{WithConventionBoundFlags(nil)})
	assert.Empty(t, o.boundFlags)
}

func TestWithConventionBoundFlags_MapsNames(t *testing.T) {
	t.Parallel()

	fs := pflag.NewFlagSet("x", pflag.ContinueOnError)
	fs.Int("server-port", 0, "")

	o := applyRootOptions([]RootOption{WithConventionBoundFlags(fs)})
	assert.Contains(t, o.boundFlags, "server.port")
}
