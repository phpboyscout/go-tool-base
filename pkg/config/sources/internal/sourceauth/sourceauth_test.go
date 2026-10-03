package sourceauth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
)

type reporter struct {
	setup.ConfigBootstrap
	rung string
}

func (r *reporter) ReportCredential(origin string) { r.rung = origin }

func settings(t *testing.T, yaml string) config.Reader {
	t.Helper()

	store, err := config.NewStore(t.Context(), config.WithReaders(config.NamedSource{Name: "settings", Content: []byte(yaml)}))
	require.NoError(t, err)

	return store.View()
}

// Spec 0204 D18: a source's token may come through GTB's rungs. Not parallel:
// the environment is process-wide.
func TestToken(t *testing.T) {
	t.Setenv("CI", "")
	t.Setenv("TEAM_VAULT_TOKEN", "from-env")

	tests := []struct {
		name, yaml, want, rung string
	}{
		{name: "an environment variable named by auth.env", yaml: "auth:\n  env: TEAM_VAULT_TOKEN\n", want: "from-env", rung: "auth.env"},
		{name: "a literal outside CI", yaml: "auth:\n  value: literal\n", want: "literal", rung: "auth.value"},
		{name: "none: the provider's own variable applies", yaml: "address: x\n", want: "", rung: "VAULT_TOKEN"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := &reporter{}
			got, err := Token(t.Context(), settings(t, tc.yaml), b, "team", "VAULT_TOKEN")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.rung, b.rung, "spec 0204 D10: the rung that answered is reported")
		})
	}
}

func TestToken_RefusesALiteralUnderCI(t *testing.T) {
	t.Setenv("CI", "true")

	_, err := Token(t.Context(), settings(t, "auth:\n  value: literal\n"), &reporter{}, "team", "VAULT_TOKEN")
	require.ErrorIs(t, err, ErrLiteralUnderCI)
}
