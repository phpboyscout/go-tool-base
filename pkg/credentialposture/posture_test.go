package credentialposture_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/credentialposture"
)

// Spec 0189 phase 1 (R1, R2, D4, D5): report which source supplies each
// credential, and which lower-precedence copies are still present, for every
// credential rather than only forges — by name, never by value.

// fakeReader is a config reader over a fixed key/value map.
type fakeReader map[string]string

func (f fakeReader) GetString(key string) string { return f[key] }

// descriptor is the shape a forge declares. AI providers differ only in their
// key names, which is the whole point of the generalisation.
func forgeDescriptor() credentialposture.Descriptor {
	return credentialposture.Descriptor{
		Owner:       "forge:github",
		Label:       "GitHub",
		EnvKey:      "github.auth.env",
		KeychainKey: "github.auth.keychain",
		LiteralKey:  "github.auth.value",
		FallbackEnv: "GITHUB_TOKEN",
	}
}

// noFallbackDescriptor is the forge shape with the well-known variable removed,
// for tests whose result must not depend on whether the developer running them
// happens to have GITHUB_TOKEN exported.
func noFallbackDescriptor() credentialposture.Descriptor {
	d := forgeDescriptor()
	d.FallbackEnv = ""

	return d
}

func providerDescriptor() credentialposture.Descriptor {
	return credentialposture.Descriptor{
		Owner:       "chat:anthropic",
		Label:       "Anthropic",
		EnvKey:      "anthropic.api.env",
		KeychainKey: "anthropic.api.keychain",
		LiteralKey:  "anthropic.api.key",
		FallbackEnv: "ANTHROPIC_API_KEY",
	}
}

func TestResolve_ReportsTheWinningRung(t *testing.T) {
	tests := []struct {
		name   string
		desc   credentialposture.Descriptor
		config fakeReader
		env    map[string]string
		want   credentialposture.Origin
	}{
		{
			name:   "env reference wins",
			desc:   forgeDescriptor(),
			config: fakeReader{"github.auth.env": "MY_TOKEN"},
			env:    map[string]string{"MY_TOKEN": "s3cret"},
			want:   credentialposture.OriginEnvRef,
		},
		{
			name:   "literal wins when nothing above it resolves",
			desc:   forgeDescriptor(),
			config: fakeReader{"github.auth.value": "s3cret"},
			want:   credentialposture.OriginLiteral,
		},
		{
			name:   "fallback variable is the last rung",
			desc:   forgeDescriptor(),
			config: fakeReader{},
			env:    map[string]string{"GITHUB_TOKEN": "s3cret"},
			want:   credentialposture.OriginFallbackEnv,
		},
		{
			name:   "nothing configured resolves to none",
			desc:   forgeDescriptor(),
			config: fakeReader{},
			want:   credentialposture.OriginNone,
		},
		{
			// R1: the same walk, for a credential that is not a forge.
			name:   "an AI provider resolves by the same rules",
			desc:   providerDescriptor(),
			config: fakeReader{"anthropic.api.env": "ANTHROPIC_KEY"},
			env:    map[string]string{"ANTHROPIC_KEY": "s3cret"},
			want:   credentialposture.OriginEnvRef,
		},
		{
			// A named variable that is unset is not an error: it falls through,
			// which is how one config file serves a laptop and a CI runner.
			name:   "a named but unset variable falls through",
			desc:   forgeDescriptor(),
			config: fakeReader{"github.auth.env": "NOT_SET", "github.auth.value": "s3cret"},
			want:   credentialposture.OriginLiteral,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear the well-known variable unless the case sets it. Without
			// this the result depends on whether whoever runs the tests happens
			// to have GITHUB_TOKEN exported — which is exactly the leak this
			// package reports on, and it should not decide its own tests.
			if _, set := tt.env[tt.desc.FallbackEnv]; !set && tt.desc.FallbackEnv != "" {
				t.Setenv(tt.desc.FallbackEnv, "")
			}

			for k, v := range tt.env {
				t.Setenv(k, v)
			}

			got, err := credentialposture.Resolve(context.Background(), tt.config, tt.desc)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.Origin)
		})
	}
}

func TestResolve_ReportsShadowedCopies(t *testing.T) {
	t.Setenv("MY_TOKEN", "s3cret")

	// R2: the env reference wins, but a literal is still sitting underneath it.
	// Telling those apart is the whole point — "a literal is in use" and "a
	// literal is dead configuration" read identically today.
	cfg := fakeReader{
		"github.auth.env":   "MY_TOKEN",
		"github.auth.value": "an-old-token",
	}

	got, err := credentialposture.Resolve(context.Background(), cfg, forgeDescriptor())
	require.NoError(t, err)

	assert.Equal(t, credentialposture.OriginEnvRef, got.Origin)
	require.Len(t, got.Shadowed, 1)
	assert.Equal(t, "github.auth.value", got.Shadowed[0].Key)
	assert.Equal(t, credentialposture.OriginLiteral, got.Shadowed[0].Origin)
}

func TestResolve_NoShadowWhenTheWinnerIsTheLowestRung(t *testing.T) {
	t.Parallel()

	cfg := fakeReader{"github.auth.value": "s3cret"}

	got, err := credentialposture.Resolve(context.Background(), cfg, noFallbackDescriptor())
	require.NoError(t, err)

	assert.Equal(t, credentialposture.OriginLiteral, got.Origin)
	assert.Empty(t, got.Shadowed)
}

func TestResolve_NeverReturnsTheValue(t *testing.T) {
	t.Setenv("MY_TOKEN", "super-secret-value")

	cfg := fakeReader{
		"github.auth.env":   "MY_TOKEN",
		"github.auth.value": "another-secret",
	}

	got, err := credentialposture.Resolve(context.Background(), cfg, forgeDescriptor())
	require.NoError(t, err)

	// The rendered report is the surface that reaches a terminal and a support
	// bundle. Asserting on it directly is what keeps the discipline enforced
	// rather than trusted.
	rendered := got.String()
	assert.NotContains(t, rendered, "super-secret-value")
	assert.NotContains(t, rendered, "another-secret")
	assert.Contains(t, rendered, "auth.env")
}

func TestResolve_ConfiguredButBrokenIsDiagnosedNotAbsent(t *testing.T) {
	t.Parallel()

	// A malformed keychain reference is a broken configuration, not an absent
	// one — the distinction spec 0183 established and this must preserve.
	cfg := fakeReader{"github.auth.keychain": "no-slash-here"}

	got, err := credentialposture.Resolve(context.Background(), cfg, noFallbackDescriptor())

	require.Error(t, err, "a malformed reference must be reported, not swallowed")
	assert.Equal(t, credentialposture.OriginNone, got.Origin)
}

func TestResolve_ContextCancellationIsHonoured(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := fakeReader{"github.auth.value": "s3cret"}

	_, err := credentialposture.Resolve(ctx, cfg, forgeDescriptor())
	assert.ErrorIs(t, err, context.Canceled, "a cancelled context must stop the walk")
}

func TestDescriptor_RungsAreStatedOnce(t *testing.T) {
	t.Parallel()

	// The precedence order is declared in one place so the resolver and the
	// reporter cannot disagree — the property pkg/vcs already relies on.
	rungs := forgeDescriptor().Rungs()

	require.Len(t, rungs, 4)
	assert.Equal(t, credentialposture.OriginEnvRef, rungs[0].Origin)
	assert.Equal(t, credentialposture.OriginKeychain, rungs[1].Origin)
	assert.Equal(t, credentialposture.OriginLiteral, rungs[2].Origin)
	assert.Equal(t, credentialposture.OriginFallbackEnv, rungs[3].Origin)
}

func TestPosture_DeprecatedIsTrueOnlyForALiteral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		origin credentialposture.Origin
		want   bool
	}{
		{credentialposture.OriginLiteral, true},
		{credentialposture.OriginEnvRef, false},
		{credentialposture.OriginKeychain, false},
		{credentialposture.OriginFallbackEnv, false},
		{credentialposture.OriginNone, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.origin), func(t *testing.T) {
			t.Parallel()

			p := credentialposture.Posture{Origin: tt.origin}
			assert.Equal(t, tt.want, p.Deprecated())
		})
	}
}

func TestPosture_StringReportsAnAbsentCredential(t *testing.T) {
	t.Parallel()

	p := credentialposture.Posture{Label: "GitHub", Origin: credentialposture.OriginNone}
	assert.Equal(t, "GitHub: no credential configured", p.String())
}

func TestResolve_AKeychainReferenceThatCannotBeReadIsAnError(t *testing.T) {
	t.Parallel()

	// Well-formed reference, no keychain backend linked in the test binary:
	// the read fails, and that failure must surface rather than be read as
	// "nothing configured".
	cfg := fakeReader{"github.auth.keychain": "some-service/some-account"}

	got, err := credentialposture.Resolve(context.Background(), cfg, noFallbackDescriptor())

	require.Error(t, err)
	assert.Equal(t, credentialposture.OriginNone, got.Origin)
	assert.NotContains(t, err.Error(), "some-account/", "the reference is named, the secret is not")
}

func TestResolve_ANilReaderIsNotAPanic(t *testing.T) {
	t.Parallel()

	// A command may run before configuration is loaded. Reporting "nothing
	// configured" is the useful answer; a panic is not.
	got, err := credentialposture.Resolve(context.Background(), nil, noFallbackDescriptor())

	require.NoError(t, err)
	assert.Equal(t, credentialposture.OriginNone, got.Origin)
}

func TestResolve_ADescriptorWithNoFallbackSkipsThatRung(t *testing.T) {
	t.Parallel()

	got, err := credentialposture.Resolve(context.Background(), fakeReader{}, noFallbackDescriptor())

	require.NoError(t, err)
	assert.Equal(t, credentialposture.OriginNone, got.Origin)
}

func TestResolve_CancellationIsHonouredAtEveryRung(t *testing.T) {
	t.Parallel()

	// Each rung checks the context, so a cancellation part-way through the walk
	// stops it rather than being noticed only at the end.
	for _, cfg := range []fakeReader{
		{"github.auth.env": "X"},
		{"github.auth.keychain": "svc/acct"},
		{"github.auth.value": "s3cret"},
		{},
	} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := credentialposture.Resolve(ctx, cfg, forgeDescriptor())
		assert.ErrorIs(t, err, context.Canceled)
	}
}

// Spec 0189 R5/D7: a credential established in a secure store must not silently
// resolve from a plaintext copy below it.

func TestResolveCredential_RefusesWhenAKeychainRegressesToALiteral(t *testing.T) {
	t.Parallel()

	// All three conditions: a keychain is configured, it will not answer (no
	// backend is linked in this binary), and a literal sits below it.
	cfg := fakeReader{
		"github.auth.keychain": "svc/acct",
		"github.auth.value":    "a-plaintext-token",
	}

	value, got, err := credentialposture.ResolveCredential(context.Background(), cfg, noFallbackDescriptor())

	require.Error(t, err)
	require.ErrorIs(t, err, credentialposture.ErrSecureStoreRegressed)
	assert.Empty(t, value, "the plaintext credential must not be handed out")
	assert.Equal(t, credentialposture.OriginNone, got.Origin)
	assert.NotContains(t, err.Error(), "a-plaintext-token", "the refusal must not quote the secret")
}

func TestResolveCredential_LeavesTheOrdinaryCasesAlone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config fakeReader
		want   credentialposture.Origin
	}{
		{
			// A first run: no keychain was ever established, so a literal is
			// the credential, not a regression. Incremental migration depends
			// on this staying silent.
			name:   "a literal with no keychain configured is not a regression",
			config: fakeReader{"github.auth.value": "s3cret"},
			want:   credentialposture.OriginLiteral,
		},
		{
			// The keychain is broken but there is nothing plaintext to fall
			// back to, so nothing regressed — the credential is simply absent,
			// which the error already says.
			name:   "a broken keychain with no literal below it is not a regression",
			config: fakeReader{"github.auth.keychain": "svc/acct"},
			want:   credentialposture.OriginNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, got, err := credentialposture.ResolveCredential(
				context.Background(), tt.config, noFallbackDescriptor())

			if tt.want != credentialposture.OriginNone {
				require.NoError(t, err)
			}

			require.NotErrorIs(t, err, credentialposture.ErrSecureStoreRegressed)
			assert.Equal(t, tt.want, got.Origin)
		})
	}
}

func TestResolveCredential_AHigherRungMeansTheKeychainIsNeverConsulted(t *testing.T) {
	t.Setenv("INVARIANT_TEST_VAR", "s3cret")

	// The env reference wins before the keychain is reached, so a broken
	// keychain and a literal below it are both irrelevant — the invariant must
	// not fire on a configuration that is working.
	cfg := fakeReader{
		"github.auth.env":      "INVARIANT_TEST_VAR",
		"github.auth.keychain": "svc/acct",
		"github.auth.value":    "a-plaintext-token",
	}

	value, got, err := credentialposture.ResolveCredential(context.Background(), cfg, noFallbackDescriptor())

	require.NoError(t, err)
	assert.Equal(t, credentialposture.OriginEnvRef, got.Origin)
	assert.Equal(t, "s3cret", value)
}
