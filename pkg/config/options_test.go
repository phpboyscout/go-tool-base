package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWithEnvPrefix_Applied(t *testing.T) {
	t.Parallel()

	o := applyOptions([]ContainerOption{WithEnvPrefix("GTB")})
	assert.Equal(t, "GTB", o.envPrefix)
}

func TestWithEnvPrefix_Empty(t *testing.T) {
	t.Parallel()

	o := applyOptions([]ContainerOption{WithEnvPrefix("")})
	assert.Empty(t, o.envPrefix)
}

func TestApplyOptions_Nil(t *testing.T) {
	t.Parallel()

	o := applyOptions(nil)
	assert.Empty(t, o.envPrefix)
}

func TestWithEnvPrefix_LastWins(t *testing.T) {
	t.Parallel()

	o := applyOptions([]ContainerOption{
		WithEnvPrefix("AAA"),
		WithEnvPrefix("BBB"),
	})
	assert.Equal(t, "BBB", o.envPrefix)
}
