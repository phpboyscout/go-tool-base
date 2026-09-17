package credentialposture

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSingleToken(t *testing.T) {
	t.Parallel()

	assert.Equal(t, SingleTokenKeys{
		Env:      "github.auth.env",
		Keychain: "github.auth.keychain",
		Literal:  "github.auth.value",
		Account:  "github.auth",
	}, SingleToken("github"))
}

func TestDualCredential(t *testing.T) {
	t.Parallel()

	assert.Equal(t, DualCredentialKeys{ //nolint:gosec // G101: config key names, not a credential
		User:        "bitbucket.username",
		UserEnv:     "bitbucket.username.env",
		Password:    "bitbucket.app_password",
		PasswordEnv: "bitbucket.app_password.env",
		Keychain:    "bitbucket.keychain",
		Account:     "bitbucket.auth",
	}, DualCredential("bitbucket"))
}
