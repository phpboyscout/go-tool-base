package vcs

import (
	"context"
	"encoding/json"
	"strings"
	"sync"

	"gitlab.com/phpboyscout/go/credentials"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// Bitbucket authenticates with a username and an app password, each with its
// own env-reference and literal keys, and one keychain entry holding both as a
// JSON blob (pkg/setup/forge writes it). GTB states the precedence for each
// half here, once: the env reference, the shared keychain entry, the literal,
// then the well-known variable. The forge adapter is handed both halves
// through forge.WithUsername and forge.WithCredential (go/forge spec 0026) and
// reads none of its credential keys.

// bitbucketSources composes the two halves over one forge section. The blob
// is read once, on the first half that reaches its keychain rung, and both
// halves share the result, so a locked keychain prompts once.
func bitbucketSources(sub forge.Config, userEnv, passEnv string) (username, password forge.CredentialSource) {
	keys := credentialposture.DualCredential("")
	reader := readerFor(sub)

	blob := &bitbucketBlob{reader: reader, key: strings.TrimPrefix(keys.Keychain, ".")}

	username = bitbucketHalf(reader, blob, func(b bitbucketEntry) string { return b.Username }, credentialposture.Descriptor{
		Owner:       "forge",
		EnvKey:      strings.TrimPrefix(keys.UserEnv, "."),
		LiteralKey:  strings.TrimPrefix(keys.User, "."),
		FallbackEnv: userEnv,
	})
	password = bitbucketHalf(reader, blob, func(b bitbucketEntry) string { return b.AppPassword }, credentialposture.Descriptor{
		Owner:       "forge",
		EnvKey:      strings.TrimPrefix(keys.PasswordEnv, "."),
		LiteralKey:  strings.TrimPrefix(keys.Password, "."),
		FallbackEnv: passEnv,
	})

	return username, password
}

// bitbucketHalf resolves one half: the env reference first, then the shared
// keychain blob, then the literal and the well-known variable. The two
// descriptor walks bracket the blob so the documented order holds without a
// KeychainKey on the descriptor (the blob is not a bare secret).
func bitbucketHalf(reader credentialposture.Reader, blob *bitbucketBlob, pick func(bitbucketEntry) string, d credentialposture.Descriptor) forge.CredentialSource {
	envOnly := credentialposture.Descriptor{Owner: d.Owner, EnvKey: d.EnvKey}
	rest := credentialposture.Descriptor{Owner: d.Owner, LiteralKey: d.LiteralKey, FallbackEnv: d.FallbackEnv}

	return func(ctx context.Context) (string, error) {
		if v, _, err := credentialposture.ResolveCredential(ctx, reader, envOnly); err != nil || v != "" {
			return v, err
		}

		entry, err := blob.load(ctx)
		if err != nil {
			return "", err
		}

		if v := pick(entry); v != "" {
			return v, nil
		}

		v, _, err := credentialposture.ResolveCredential(ctx, reader, rest)

		return v, err
	}
}

type bitbucketEntry struct {
	Username    string `json:"username"`
	AppPassword string `json:"app_password"`
}

// bitbucketBlob is the shared keychain entry, read at most once.
type bitbucketBlob struct {
	reader credentialposture.Reader
	key    string

	once  sync.Once
	entry bitbucketEntry
	err   error
}

func (b *bitbucketBlob) load(ctx context.Context) (bitbucketEntry, error) {
	b.once.Do(func() {
		if b.reader == nil {
			return
		}

		ref := strings.TrimSpace(b.reader.GetString(b.key))
		if ref == "" {
			return
		}

		service, account, ok := strings.Cut(ref, "/")
		if !ok || service == "" || account == "" {
			b.err = errors.Wrapf(credentialposture.ErrMalformedKeychainRef, "malformed keychain reference %q", ref)

			return
		}

		readCtx, cancel := context.WithTimeout(ctx, credentials.KeychainOpTimeout)
		defer cancel()

		raw, err := credentials.Retrieve(readCtx, service, account)
		if err != nil {
			b.err = errors.Wrapf(err, "reading keychain entry %q", ref)

			return
		}

		if err := json.Unmarshal([]byte(raw), &b.entry); err != nil {
			b.err = errors.Wrapf(err, "keychain entry %q is not the username and app password blob", ref)
		}
	})

	return b.entry, b.err
}
