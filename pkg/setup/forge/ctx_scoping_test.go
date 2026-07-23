package forge

import (
	"context"
	"testing"

	"charm.land/huh/v2"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	forgeapi "gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// ctxCapturingAuthProvider records deadline presence on the context its Login
// receives, so tests can assert which deadline (if any) governs the OAuth
// device flow. It captures the observation, not the ctx itself (fatcontext).
type ctxCapturingAuthProvider struct {
	forgeapi.Provider
	token       string
	invoked     *bool
	hasDeadline *bool
}

func (f ctxCapturingAuthProvider) Login(ctx context.Context, _ forgeapi.Prompter) (string, error) {
	*f.invoked = true
	_, *f.hasDeadline = ctx.Deadline()

	return f.token, nil
}

func ctxCapturingProviderFactory(token string, invoked, hasDeadline *bool) func(config.Reader) (forgeapi.Provider, error) {
	return func(config.Reader) (forgeapi.Provider, error) {
		return ctxCapturingAuthProvider{token: token, invoked: invoked, hasDeadline: hasDeadline}, nil
	}
}

// Regression test for the 2026-07-23 architectural review CRITICAL finding
// (spec 2026-07-23-setup-credential-stage-context-scoping): the interactive
// OAuth device flow must NOT run under the 5-second KeychainOpTimeout deadline
// that configureAuth derives for the already-configured credential check. A
// human device flow takes minutes; any stage-wide 5s deadline kills it before
// the user can act and silently degrades every login to manual token entry.
func TestConfigureAuth_LoginNotBoundByKeychainTimeout(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("CI", "")

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := newTestEditor(t, p, "")

	var invoked, hasDeadline bool

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(ctxCapturingProviderFactory("ghp_regression", &invoked, &hasDeadline)),
		WithAuthForms(WithAuthForm(
			func(cfg *AuthConfig) []*huh.Form {
				cfg.StorageMode = "" // OAuth capture path, no storage-mode form

				return nil
			},
			func(_ string, _ string) *huh.Form { return nil },
		)),
	)

	require.NoError(t, init.Configure(t.Context(), p, cfg))
	require.True(t, invoked, "Login was never invoked")

	assert.False(t, hasDeadline,
		"the OAuth login context must carry no KeychainOpTimeout-derived deadline; "+
			"a 5s bound kills every human device flow before it can complete")
}

// ctxScopeKey marks the caller's context so backend fakes can prove the ctx
// they receive derives from the caller, not from context.Background().
type ctxScopeKey struct{}

// deadlineRecordingBackend captures the context each Store receives.
type deadlineRecordingBackend struct {
	storeHasDeadline bool
	storeHasMarker   bool
	stored           bool
}

func (b *deadlineRecordingBackend) Store(ctx context.Context, _, _, _ string) error {
	_, b.storeHasDeadline = ctx.Deadline()
	b.storeHasMarker = ctx.Value(ctxScopeKey{}) != nil
	b.stored = true

	return nil
}

func (b *deadlineRecordingBackend) Retrieve(context.Context, string, string) (string, error) {
	return "", credentials.ErrCredentialNotFound
}

func (b *deadlineRecordingBackend) Delete(context.Context, string, string) error { return nil }
func (b *deadlineRecordingBackend) Available() bool                              { return true }

// unavailableBackend restores the default-stub behaviour after a test swapped
// in a fake (RegisterBackend has no undo; the stub type is unexported).
type unavailableBackend struct{}

func (unavailableBackend) Store(context.Context, string, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (unavailableBackend) Retrieve(context.Context, string, string) (string, error) {
	return "", credentials.ErrCredentialUnsupported
}

func (unavailableBackend) Delete(context.Context, string, string) error {
	return credentials.ErrCredentialUnsupported
}

func (unavailableBackend) Available() bool { return false }

// swapBackend installs a fake credentials backend for one sequential test.
// Deliberately no t.Parallel() in callers: the registry is process-global, and
// Go never overlaps sequential tests with the package's parallel ones.
func swapBackend(t *testing.T, b credentials.Backend) {
	t.Helper()
	credentials.RegisterBackend(b)
	t.Cleanup(func() { credentials.RegisterBackend(unavailableBackend{}) })
}

// The keychain write must derive its own KeychainOpTimeout deadline at the
// store call site, from the caller's context — not inherit a stage-wide clock
// that started before the human-paced forms.
func TestConfigureAuth_KeychainStoreScopedPerOperation(t *testing.T) {
	t.Setenv("HOME", "/home/testuser")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("CI", "")

	backend := &deadlineRecordingBackend{}
	swapBackend(t, backend)

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := newTestEditor(t, p, "")

	init := NewGitHubInitialiser(p, false, true,
		WithProviderFactory(authProviderFactory("ghp_keychain", nil)),
		WithAuthForms(WithAuthForm(
			func(cfg *AuthConfig) []*huh.Form {
				cfg.StorageMode = credentials.ModeKeychain

				return nil
			},
			func(_ string, _ string) *huh.Form { return nil },
		)),
	)

	ctx := context.WithValue(t.Context(), ctxScopeKey{}, "caller")
	require.NoError(t, init.Configure(ctx, p, cfg))

	require.True(t, backend.stored, "keychain Store was never invoked")
	assert.True(t, backend.storeHasDeadline,
		"the keychain store must run under a per-operation KeychainOpTimeout deadline")
	assert.True(t, backend.storeHasMarker,
		"the store ctx must derive from the caller's ctx (cancellation propagates), not context.Background()")
}

// The dual-credential flow has the same shape: no stage-wide deadline spanning
// the forms, a fresh per-operation deadline at the keychain blob write.
func TestConfigureDual_KeychainStoreScopedPerOperation(t *testing.T) {
	t.Setenv("CI", "")

	backend := &deadlineRecordingBackend{}
	swapBackend(t, backend)

	p := &props.Props{
		FS:     afero.NewMemMapFs(),
		Logger: logger.NewNoop(),
		Tool:   props.Tool{Name: "testtool"},
	}
	cfg := newTestEditor(t, p, "")

	init := NewBitbucketInitialiser(p, WithDualForms(mockForms(func(c *DualConfig) {
		c.StorageMode = credentials.ModeKeychain
		c.Username = "user"
		c.AppPassword = "app-pw"
	})))

	ctx := context.WithValue(t.Context(), ctxScopeKey{}, "caller")
	require.NoError(t, init.Configure(ctx, p, cfg))

	require.True(t, backend.stored, "keychain Store was never invoked")
	assert.True(t, backend.storeHasDeadline,
		"the dual keychain store must run under a per-operation KeychainOpTimeout deadline")
	assert.True(t, backend.storeHasMarker,
		"the store ctx must derive from the caller's ctx, not context.Background()")
}
