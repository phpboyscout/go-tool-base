package root

import (
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
)

// stubBindable records BindPFlag calls and can be told to error.
type stubBindable struct {
	bound   map[string]*pflag.Flag
	failKey string
}

func (s *stubBindable) BindPFlag(key string, flag *pflag.Flag) error {
	if key == s.failKey {
		return errors.New("boom")
	}

	if s.bound == nil {
		s.bound = map[string]*pflag.Flag{}
	}

	s.bound[key] = flag

	return nil
}

type stubLogger struct{ msgs []string }

func (s *stubLogger) Debug(msg string, _ ...any) { s.msgs = append(s.msgs, msg) }

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

func TestBindChangedFlags(t *testing.T) {
	t.Parallel()

	changed := changedFlag(t, "changed", "v")
	unchanged := changedFlag(t, "unchanged", "")

	tests := []struct {
		name      string
		flags     map[string]*pflag.Flag
		failKey   string
		wantBound int
	}{
		{
			name:      "binds only changed flags",
			flags:     map[string]*pflag.Flag{"a": changed, "b": unchanged, "c": nil},
			wantBound: 1,
		},
		{
			name:      "bind error is skipped",
			flags:     map[string]*pflag.Flag{"fail": changed},
			failKey:   "fail",
			wantBound: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			b := &stubBindable{failKey: tc.failKey}
			got := bindChangedFlags(b, tc.flags, &stubLogger{})
			assert.Equal(t, tc.wantBound, got)
		})
	}
}
