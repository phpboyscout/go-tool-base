package forge

import (
	"context"
	"io"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/formtest"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// Spec 0186: the SSH stage is shape-agnostic and now runs from the shared
// dispatch, so a dual-credential forge that can accept a key gets offered one.
// These cover the stage being *reached* and *ordered*; ssh_test.go covers what
// the stage does once it runs.

// keyManager hands the SSH stage a fake key manager, so the run does not
// depend on a forge adapter being linked.
func keyManager(km forge.KeyManager, kmErr error) InitialiserOption {
	return WithSSHOptions(WithKeyManager(keyManagerFactory(km, kmErr)))
}

// dualEnvThenGenerateIO drives the whole Bitbucket wizard with keys: env-var
// credentials with the fallback names, then a generated SSH key and the
// upload answer. Keys rather than answers because the passphrase is a
// password field. Paced by time, so callers do not call t.Parallel.
func dualEnvThenGenerateIO(t *testing.T, p *props.Props, upload bool) props.IO {
	t.Helper()

	scripts := append([]io.Reader{formtest.Keys(dualEnvKeys(t)...)}, sshGenerateScripts(t, p, upload)...)

	return formtest.TUIForms(scripts...)
}

func dualSSHProps(t *testing.T) *props.Props {
	t.Helper()
	t.Setenv("CI", "")
	t.Setenv("HOME", "/home/testuser")

	return newTestProps(t)
}

// TestDualProfileReachesTheSSHStage is the headline of 0186: before this, a
// dual-credential profile could not reach the stage at all, because the call
// sat inside configureSingle.
func TestDualProfileReachesTheSSHStage(t *testing.T) {
	p := dualSSHProps(t)
	cfg := newTestEditor(t, p, "")

	km := &fakeKeyManager{}

	p.IO = dualEnvThenGenerateIO(t, p, true)
	i := NewBitbucketInitialiser(p, keyManager(km, nil))

	require.NoError(t, i.Configure(t.Context(), p, cfg))

	view := cfg.View()
	assert.NotEmpty(t, view.GetString("bitbucket.ssh.key.path"),
		"the SSH stage must record the key path for a dual-credential profile")
	assert.True(t, km.uploaded, "the key must reach the forge's KeyManager")
}

// TestDualSSHRunsAfterCredentialCapture covers D2. Bitbucket's UploadKey is
// authorised by the username and app password, so an upload before capture
// could not succeed. The ordering was previously an accident of where the call
// sat; it is now a requirement.
func TestDualSSHRunsAfterCredentialCapture(t *testing.T) {
	p := dualSSHProps(t)
	cfg := newTestEditor(t, p, "")

	var credentialSeenAtUpload bool

	km := &fakeKeyManager{}

	p.IO = dualEnvThenGenerateIO(t, p, true)
	i := NewBitbucketInitialiser(p,
		// The factory receives the config the stage was handed: if the
		// credential stage ran first, its writes are visible here.
		WithSSHOptions(WithKeyManager(func(_ context.Context, c config.Reader) (forge.KeyManager, error) {
			credentialSeenAtUpload = c.GetString("bitbucket.username.env") != ""

			return km, nil
		})),
	)

	require.NoError(t, i.Configure(t.Context(), p, cfg))
	assert.True(t, credentialSeenAtUpload,
		"the SSH stage must run after credential capture, so the upload is authorised")
}

// TestDualSkipKeySuppressesTheStage covers the checklist item that --skip-key
// works identically for both shapes.
func TestDualSkipKeySuppressesTheStage(t *testing.T) {
	p := dualSSHProps(t)
	cfg := newTestEditor(t, p, "")

	km := &fakeKeyManager{}

	// The SSH stage is skipped, so only the credential form runs.
	p.IO = dualEnvIO(t, "BB_USER", "BB_APP_PW")
	i := NewBitbucketInitialiser(p, keyManager(km, nil))
	i.SkipKey = true

	require.NoError(t, i.Configure(t.Context(), p, cfg))

	assert.Empty(t, cfg.View().GetString("bitbucket.ssh.key.path"))
	assert.False(t, km.uploaded, "--skip-key must suppress the stage entirely")
}

// TestProfileWithoutSSHNeverConstructsAKeyManager covers the checklist item that
// a profile declining SSH does not pay for the stage at all — the factory must
// not be reached.
func TestProfileWithoutSSHNeverConstructsAKeyManager(t *testing.T) {
	p := dualSSHProps(t)
	cfg := newTestEditor(t, p, "")

	noSSHProfile := bitbucketProfile
	noSSHProfile.OffersSSH = false

	factoryCalled := false

	p.IO = dualEnvIO(t, "BB_USER", "BB_APP_PW")
	i := New(p, noSSHProfile,
		WithSSHOptions(WithKeyManager(func(context.Context, config.Reader) (forge.KeyManager, error) {
			factoryCalled = true

			return nil, nil
		})),
	)

	require.NoError(t, i.Configure(t.Context(), p, cfg))
	assert.False(t, factoryCalled, "a profile that does not offer SSH must not construct a KeyManager")
}

// TestBitbucketProfileHasAHost covers D4 directly. defaultKeyManager builds the
// provider with forgeapi.Endpoint{Type: profile.Provider, Host: profile.Host},
// so an empty Host is the same hazard 0185 D3 records for token instructions,
// reached by a different path — and it is required whether or not SSH is
// offered. Spec 0192 added the Type half; this test still owns the Host half.
func TestBitbucketProfileHasAHost(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "bitbucket.org", bitbucketProfile.Host)
	assert.True(t, bitbucketProfile.OffersSSH)
}

// TestSingleTokenSSHStillRuns is the regression check for D6: hoisting the call
// out of configureSingle must leave single-token behaviour intact.
func TestSingleTokenSSHStillRuns(t *testing.T) {
	p := dualSSHProps(t)
	p.FS = afero.NewMemMapFs()

	cfg := newTestEditor(t, p, "github:\n  auth:\n    value: already-set-token\n")

	km := &fakeKeyManager{}

	p.IO = formtest.TUIForms(sshGenerateScripts(t, p, true)...)
	i := New(p, gitHubProfile, keyManager(km, nil))
	i.SkipLogin = true

	require.NoError(t, i.Configure(t.Context(), p, cfg))

	assert.NotEmpty(t, cfg.View().GetString("github.ssh.key.path"),
		"a single-token profile must still reach the SSH stage after the hoist")
	assert.True(t, km.uploaded)
}
