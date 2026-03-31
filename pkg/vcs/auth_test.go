package vcs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	testifymock "github.com/stretchr/testify/mock"

	mockcfg "github.com/phpboyscout/go-tool-base/mocks/pkg/config"
)

func TestResolveToken_FromConfigEnv(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("MY_CUSTOM_TOKEN", "token-from-env")

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", "auth.env").Return(true)
	mock.On("GetString", "auth.env").Return("MY_CUSTOM_TOKEN")
	mock.On("Has", "auth.value").Maybe().Return(false)

	token := ResolveToken(mock, "")

	assert.Equal(t, "token-from-env", token)
}

func TestResolveToken_FromConfigValue(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", "auth.env").Return(false)
	mock.On("Has", "auth.value").Return(true)
	mock.On("GetString", "auth.value").Return("literal-token")

	token := ResolveToken(mock, "")

	assert.Equal(t, "literal-token", token)
}

func TestResolveToken_FromFallbackEnv(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("GITHUB_TOKEN", "fallback-token")

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", testifymock.Anything).Return(false)

	token := ResolveToken(mock, "GITHUB_TOKEN")

	assert.Equal(t, "fallback-token", token)
}

func TestResolveToken_PrecedenceConfigEnvOverValue(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("PRIORITY_TOKEN", "env-wins")

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", "auth.env").Return(true)
	mock.On("GetString", "auth.env").Return("PRIORITY_TOKEN")
	mock.On("Has", "auth.value").Maybe().Return(true)
	mock.On("GetString", "auth.value").Maybe().Return("value-loses")

	token := ResolveToken(mock, "")

	assert.Equal(t, "env-wins", token, "auth.env should take precedence over auth.value")
}

func TestResolveToken_PrecedenceConfigOverFallback(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("FALLBACK_TOKEN", "fallback-loses")

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", "auth.env").Return(false)
	mock.On("Has", "auth.value").Return(true)
	mock.On("GetString", "auth.value").Return("config-wins")

	token := ResolveToken(mock, "FALLBACK_TOKEN")

	assert.Equal(t, "config-wins", token, "config auth.value should take precedence over fallback env")
}

func TestResolveToken_NilConfig(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("FALLBACK_TOKEN", "from-fallback")

	token := ResolveToken(nil, "FALLBACK_TOKEN")

	assert.Equal(t, "from-fallback", token)
}

func TestResolveToken_NilConfigNoFallback(t *testing.T) {
	t.Parallel()

	token := ResolveToken(nil, "")

	assert.Empty(t, token)
}

func TestResolveToken_EmptyEnvVar(t *testing.T) {
	// Not parallel — modifies environment
	t.Setenv("EMPTY_TOKEN", "")

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", "auth.env").Return(true)
	mock.On("GetString", "auth.env").Return("EMPTY_TOKEN")
	mock.On("Has", "auth.value").Return(false)

	token := ResolveToken(mock, "")

	assert.Empty(t, token, "empty env var should not be treated as a valid token")
}

func TestResolveToken_NoTokenFound(t *testing.T) {
	t.Parallel()

	mock := mockcfg.NewMockContainable(t)
	mock.On("Has", testifymock.Anything).Return(false)

	token := ResolveToken(mock, "")

	assert.Empty(t, token)
}
