package props_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"

	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func storeKeyCase(t *testing.T, spec props.ConfigSpec) bool {
	t.Helper()

	opts := spec.WithKeyCase([]config.StoreOption{config.WithReaders(config.NamedSource{Name: "doc", Content: []byte("Log:\n  Level: warn\n")})})

	store, err := config.NewStore(t.Context(), opts...)
	require.NoError(t, err)

	return store.View().CaseSensitiveKeys()
}

// Spec 0209 D1, D2: an undeclared key case is folded, as every tool before it.
func TestConfigSpec_WithKeyCase_UnstatedIsFolded(t *testing.T) {
	t.Parallel()

	assert.False(t, storeKeyCase(t, props.ConfigSpec{}))
	assert.False(t, storeKeyCase(t, props.ConfigSpec{KeyCase: props.KeyCaseFolded}))
}

func TestConfigSpec_WithKeyCase_ExactBuildsACaseSensitiveStore(t *testing.T) {
	t.Parallel()

	assert.True(t, storeKeyCase(t, props.ConfigSpec{KeyCase: props.KeyCaseExact}))
}

func TestConfigSpec_WithKeyCase_KeepsTheCallersOptions(t *testing.T) {
	t.Parallel()

	opts := props.ConfigSpec{KeyCase: props.KeyCaseExact}.WithKeyCase([]config.StoreOption{
		config.WithReaders(config.NamedSource{Name: "doc", Content: []byte("Log:\n  Level: warn\n")}),
	})

	store, err := config.NewStore(t.Context(), opts...)
	require.NoError(t, err)
	assert.Equal(t, "warn", store.View().GetString("Log.Level"))
	assert.False(t, store.View().IsSet("log.level"), "an exact store does not fold the lookup")
}

func TestValidateConfigSpec_RefusesAnUnknownKeyCase(t *testing.T) {
	t.Parallel()

	err := props.ValidateConfigSpec(props.ConfigSpec{Layers: props.DefaultConfigLayers(), KeyCase: "upper"})
	require.ErrorIs(t, err, props.ErrConfigKeyCase)

	for _, kc := range []props.KeyCase{"", props.KeyCaseFolded, props.KeyCaseExact} {
		require.NoError(t, props.ValidateConfigSpec(props.ConfigSpec{Layers: props.DefaultConfigLayers(), KeyCase: kc}))
	}
}
