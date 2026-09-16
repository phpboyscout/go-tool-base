package forge

import (
	"context"
	"testing"

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

func ctxCapturingProviderFactory(token string, invoked, hasDeadline *bool) func(context.Context, config.Reader) (forgeapi.Provider, error) {
	return func(context.Context, config.Reader) (forgeapi.Provider, error) {
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

	// Literal mode: the OAuth capture path with no display-once page.
	p.IO, _ = singleAuthIO(t, credentials.ModeLiteral, "", false)
	init := NewGitHubInitialiser(p, false, true,
		withProviderFactory(ctxCapturingProviderFactory("ghp_regression", &invoked, &hasDeadline)),
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

// deadlineRecordingBackend captures the context the credential's Store
// receives. The storage-mode selector probes the backend with a throwaway
// item first, so the fake keeps what it is given (the probe reads it back)
// and records only the write that is not the probe's.
type deadlineRecordingBackend struct {
	items            map[string]string
	storeHasDeadline bool
	storeHasMarker   bool
	stored           bool
}

const probeService = "credentials-keychain-probe"

func (b *deadlineRecordingBackend) Store(ctx context.Context, service, account, value string) error {
	if b.items == nil {
		b.items = map[string]string{}
	}

	b.items[service+"/"+account] = value

	if service == probeService {
		return nil
	}

	_, b.storeHasDeadline = ctx.Deadline()
	b.storeHasMarker = ctx.Value(ctxScopeKey{}) != nil
	b.stored = true

	return nil
}

func (b *deadlineRecordingBackend) Retrieve(_ context.Context, service, account string) (string, error) {
	if v, ok := b.items[service+"/"+account]; ok {
		return v, nil
	}

	return "", credentials.ErrCredentialNotFound
}

func (b *deadlineRecordingBackend) Delete(_ context.Context, service, account string) error {
	delete(b.items, service+"/"+account)

	return nil
}

func (b *deadlineRecordingBackend) Available() bool { return true }

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

	p.IO, _ = singleAuthIO(t, credentials.ModeKeychain, "", false)
	init := NewGitHubInitialiser(p, false, true,
		withProviderFactory(authProviderFactory("ghp_keychain", nil)),
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
	// Seed a recorded SSH key: Bitbucket reaches the SSH stage now (0186 D1),
	// and this test is about the keychain store's ctx scoping, not the key.
	cfg := newTestEditor(t, p, "bitbucket:\n  ssh:\n    key:\n      path: /home/u/.ssh/id_x\n")

	p.IO = dualCredentialIO(t, credentials.ModeKeychain, "user", "app-pw")
	init := NewBitbucketInitialiser(p)

	ctx := context.WithValue(t.Context(), ctxScopeKey{}, "caller")
	require.NoError(t, init.Configure(ctx, p, cfg))

	require.True(t, backend.stored, "keychain Store was never invoked")
	assert.True(t, backend.storeHasDeadline,
		"the dual keychain store must run under a per-operation KeychainOpTimeout deadline")
	assert.True(t, backend.storeHasMarker,
		"the store ctx must derive from the caller's ctx, not context.Background()")
}
