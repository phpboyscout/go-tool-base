// Package sourceauth resolves a config source's token through GTB's rungs:
// config.sources.<name>.auth.env names a variable, .auth.keychain a
// "service/account" entry, .auth.value holds the token itself (spec 0204
// D18). It is the chain forges use, so a source reports its credential in the
// same vocabulary. None set is not an error: the provider's own variable
// (VAULT_TOKEN, CONSUL_HTTP_TOKEN) then applies.
package sourceauth

import (
	"context"

	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// ErrLiteralUnderCI is a token stored as a literal and read under CI, where
// a plaintext credential in a config file is refused.
var ErrLiteralUnderCI = errors.NewSentinel("gtb.config.sources.literal_under_ci", "a literal source token is refused under CI")

// Token returns the source's token from the first rung that holds one, or ""
// when none does.
func Token(ctx context.Context, settings credentialposture.Reader, source string) (string, error) {
	token, posture, err := credentialposture.ResolveCredential(ctx, settings, credentialposture.Descriptor{
		Owner:       "config-source:" + source,
		Label:       "the " + source + " config source",
		EnvKey:      "auth.env",
		KeychainKey: "auth.keychain",
		LiteralKey:  "auth.value",
	})
	if err != nil {
		return "", err
	}

	if posture.Origin == credentialposture.OriginLiteral && credentials.IsCI() {
		return "", errors.WithHintf(errors.Wrapf(ErrLiteralUnderCI, "%s", source),
			"name a variable in config.sources.%s.auth.env instead", source)
	}

	return token, nil
}
