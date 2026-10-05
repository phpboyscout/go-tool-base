package vcs_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"
	"gitlab.com/phpboyscout/go/errors"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

const (
	bbService = "testtool"
	bbAccount = "bitbucket.auth"
	bbBlob    = `{"username":"kc-user","app_password":"kc-pw"}`
)

// blobBackend serves one keychain entry and counts the reads, so the shared
// read can be asserted rather than assumed.
type blobBackend struct {
	raw       string
	retrieves int
}

func (b *blobBackend) Store(context.Context, string, string, string) error { return nil }

func (b *blobBackend) Retrieve(context.Context, string, string) (string, error) {
	b.retrieves++

	return b.raw, nil
}

func (b *blobBackend) Delete(context.Context, string, string) error { return nil }
func (b *blobBackend) Available() bool                              { return true }

func bitbucketOptions(t *testing.T, yamlDoc string) *forge.Options {
	t.Helper()

	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_APP_PASSWORD", "")

	cfg := vcs.ConfigFromReader(testutil.ViewFromYAML(t, yamlDoc))

	return forge.NewOptions(vcs.CredentialOptions(forge.Endpoint{Type: forge.SourceTypeBitbucket}, cfg, "")...)
}

func storeBlob(t *testing.T, raw string) {
	t.Helper()

	credtest.Install(t)
	require.NoError(t, credentials.Store(t.Context(), bbService, bbAccount, raw))
}

func TestBitbucketKeychain_SuppliesBothHalves(t *testing.T) {
	storeBlob(t, bbBlob)

	o := bitbucketOptions(t, "bitbucket:\n  keychain: testtool/bitbucket.auth\n  username: literal-user\n  app_password: literal-pw\n")

	user, err := o.Username(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "kc-user", user, "the keychain blob beats the literal")

	pw, err := o.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "kc-pw", pw)
}

func TestBitbucketKeychain_EnvReferenceBeatsTheBlob(t *testing.T) {
	storeBlob(t, bbBlob)
	t.Setenv("MY_BB_USER", "env-user")

	o := bitbucketOptions(t, "bitbucket:\n  keychain: testtool/bitbucket.auth\n  username:\n    env: MY_BB_USER\n")

	user, err := o.Username(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "env-user", user)

	pw, err := o.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "kc-pw", pw, "the other half still comes from the blob")
}

func TestBitbucketKeychain_AHalfMissingFromTheBlobFallsToTheLiteral(t *testing.T) {
	storeBlob(t, `{"username":"kc-user"}`)

	o := bitbucketOptions(t, "bitbucket:\n  keychain: testtool/bitbucket.auth\n  app_password: literal-pw\n")

	pw, err := o.Credential(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "literal-pw", pw)
}

func TestBitbucketKeychain_BothHalvesShareOneRead(t *testing.T) {
	credtest.Install(t) // restores the default backend on cleanup

	backend := &blobBackend{raw: bbBlob}
	credentials.RegisterBackend(backend)

	o := bitbucketOptions(t, "bitbucket:\n  keychain: testtool/bitbucket.auth\n")

	_, err := o.Username(t.Context())
	require.NoError(t, err)
	_, err = o.Credential(t.Context())
	require.NoError(t, err)

	assert.Equal(t, 1, backend.retrieves, "a locked keychain prompts once, not once per half")
}

func TestBitbucketKeychain_Refusals(t *testing.T) {
	tests := []struct {
		name    string
		ref     string
		stored  string
		wantErr error
		wantMsg string
	}{
		{
			name:    "a reference without an account",
			ref:     "testtool",
			wantErr: credentialposture.ErrMalformedKeychainRef,
		},
		{
			name:    "a reference with an empty service",
			ref:     "/bitbucket.auth",
			wantErr: credentialposture.ErrMalformedKeychainRef,
		},
		{
			name:    "an entry that is not there",
			ref:     "testtool/absent",
			wantMsg: `reading keychain entry "testtool/absent"`,
		},
		{
			name:    "an entry that is not the blob",
			ref:     "testtool/bitbucket.auth",
			stored:  "a-bare-token",
			wantMsg: "is not the username and app password blob",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			storeBlob(t, tc.stored)

			o := bitbucketOptions(t, "bitbucket:\n  keychain: "+tc.ref+"\n")

			_, err := o.Username(t.Context())
			require.Error(t, err)

			if tc.wantErr != nil {
				assert.True(t, errors.Is(err, tc.wantErr), "got %v", err)
			}

			if tc.wantMsg != "" {
				assert.Contains(t, err.Error(), tc.wantMsg)
			}

			_, pwErr := o.Credential(t.Context())
			assert.Equal(t, err, pwErr, "the other half reports the same refusal")
		})
	}
}
