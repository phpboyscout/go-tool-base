package vcs_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go/config"
	"gitlab.com/phpboyscout/go/credentials"
	credtest "gitlab.com/phpboyscout/go/credentials/test"
	"gitlab.com/phpboyscout/go/forge"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

// go/forge v0.8.0 moved credential ordering out of the module and into the
// consumer's config stack. These assert GTB's stack reproduces the documented
// five-rung precedence exactly — the behaviour the bump would otherwise have
// changed silently, since making it compile is easy and making it behave is not.

const (
	envRefName   = "GTB_TEST_FORGE_TOKEN"
	fallbackName = "GITHUB_TOKEN"
	service      = "testtool"
	account      = "github.auth"
)

// sub scopes a YAML document to the github subtree, exactly as the call sites do
// via vcs.ConfigFromReader(cfg).Sub(forgeName).
func sub(t *testing.T, yamlDoc string) forge.Config {
	t.Helper()

	return vcs.ConfigFromReader(testutil.ViewFromYAML(t, yamlDoc)).Sub("github")
}

// countingBackend records how many times the keychain was actually reached, so
// laziness can be asserted rather than assumed.
type countingBackend struct {
	retrieves int
}

func (b *countingBackend) Store(context.Context, string, string, string) error { return nil }

func (b *countingBackend) Retrieve(context.Context, string, string) (string, error) {
	b.retrieves++

	return "from-keychain", nil
}

func (b *countingBackend) Delete(context.Context, string, string) error { return nil }
func (b *countingBackend) Available() bool                              { return true }

// unavailableBackend restores the default stub after a test swaps one in;
// RegisterBackend has no undo and the real stub type is unexported.
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

// TestForgeCredential_Precedence is the ordered table the spec asks for: one
// case per position in the chain, each proving the rung above it wins.
func TestForgeCredential_Precedence(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		// env values set for the duration of the case
		envRef   string
		fallback string
		keychain string
		want     string
	}{
		{
			name: "nothing configured resolves to nothing, and is not an error",
			yaml: "",
			want: "",
		},
		{
			name:     "fallback env alone",
			yaml:     "github:\n  auth:\n    value: \"\"\n",
			fallback: "from-fallback",
			want:     "from-fallback",
		},
		{
			// The baseline the spec asks be pinned explicitly: the oldest and
			// most common shape must keep working with nothing else present.
			name: "literal alone, with no fallback set",
			yaml: "github:\n  auth:\n    value: from-literal\n",
			want: "from-literal",
		},
		{
			name:     "literal beats the fallback env",
			yaml:     "github:\n  auth:\n    value: from-literal\n",
			fallback: "from-fallback",
			want:     "from-literal",
		},
		{
			name:     "keychain beats the literal",
			yaml:     "github:\n  auth:\n    keychain: testtool/github.auth\n    value: from-literal\n",
			keychain: "from-keychain",
			fallback: "from-fallback",
			want:     "from-keychain",
		},
		{
			name:     "env reference beats everything below it",
			yaml:     "github:\n  auth:\n    env: " + envRefName + "\n    keychain: testtool/github.auth\n    value: from-literal\n",
			envRef:   "from-env-ref",
			keychain: "from-keychain",
			fallback: "from-fallback",
			want:     "from-env-ref",
		},
		{
			// The rung is a pointer: naming a variable that is unset must fall
			// through rather than resolving to empty, which is how one config
			// file serves both a developer machine and a CI runner.
			name:     "a named env var that is unset falls through",
			yaml:     "github:\n  auth:\n    env: " + envRefName + "\n    value: from-literal\n",
			envRef:   "",
			fallback: "from-fallback",
			want:     "from-literal",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envRefName, tc.envRef)
			t.Setenv(fallbackName, tc.fallback)

			if tc.keychain != "" {
				credtest.Install(t)
				require.NoError(t, credentials.Store(t.Context(), service, account, tc.keychain))
			}

			got, err := vcs.ForgeCredential(sub(t, tc.yaml), fallbackName)(t.Context())
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestForgeCredential_AbsentSectionStillResolvesFromEnv covers the D10 warning:
// Sub returns nil when the section is absent, and a tool configured purely by
// environment may legitimately have no section at all. A nil subtree must not
// swallow the fallback.
func TestForgeCredential_AbsentSectionStillResolvesFromEnv(t *testing.T) {
	t.Setenv(fallbackName, "from-fallback")

	// No github: block anywhere, so Sub returns nil.
	scoped := sub(t, "other:\n  key: value\n")
	require.Nil(t, scoped, "fixture assumption: Sub returns nil for an absent section")

	got, err := vcs.ForgeCredential(scoped, fallbackName)(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-fallback", got)
}

// TestForgeCredential_SectionFromEnvLayerAlone is the CI redirection case, and
// the one D10 singles out as load-bearing: if it fails, the scoping contract
// needs revisiting before anything else in this spec stands up.
//
// An operator sets GTB_GITHUB_AUTH_ENV=CI_JOB_TOKEN in the job environment and
// ships no `github:` block in any file. The section then exists ONLY in the env
// layer — so Sub("github") must still return a non-nil subtree, and the
// dereference must follow through to the named variable. That indirection is
// the whole reason GTB keeps auth.env rather than letting a prefixed env layer
// supply the credential directly: a layer can carry a value, but it cannot say
// "read whichever variable this deployment names".
func TestForgeCredential_SectionFromEnvLayerAlone(t *testing.T) {
	t.Setenv("GTBTEST_GITHUB_AUTH_ENV", "CI_JOB_TOKEN")
	t.Setenv("CI_JOB_TOKEN", "from-ci-job")
	t.Setenv(fallbackName, "from-fallback")

	store, err := config.NewStore(t.Context(), config.WithEnv("GTBTEST"))
	require.NoError(t, err)

	scoped := vcs.ConfigFromReader(store.View()).Sub("github")
	require.NotNil(t, scoped,
		"a section present only in the env layer must still scope — otherwise "+
			"env-only configuration silently resolves nothing")

	got, err := vcs.ForgeCredential(scoped, fallbackName)(t.Context())
	require.NoError(t, err)
	assert.Equal(t, "from-ci-job", got,
		"auth.env must dereference the named variable, not fall through to the fallback")
}

// TestConfigCredentialIsNotUsed guards D10 as a property rather than a fact.
//
// forge.ConfigCredential's stale-key report exempts only the single key it was
// pointed at and probes the relative auth.env / auth.keychain — precisely the
// keys GTB ships defaults for. Pointed at auth.value while auth.env is set, it
// reports a working configuration as stale. Reintroducing it anywhere would
// bring that false positive back on the shipped default, for every user whose
// credential fails to resolve.
//
// The scan tolerates the word in a comment — pkg/vcs/credential.go explains at
// length why it is absent — and looks for a call.
//
// It scans GTB's OWN source only. CI sets GOMODCACHE inside the build directory
// (.go/pkg/mod), so a naive walk from the repo root descends into the forge
// modules themselves and reports their legitimate calls — green locally, red in
// CI, for a reason that has nothing to do with GTB. Dependency source is not
// this guard's business: the property is that GTB does not call it.
func TestConfigCredentialIsNotUsed(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs("../..")
	require.NoError(t, err)

	// Assembled from parts so the scan does not match its own needle. Spelling
	// it literally here made this test fail on itself.
	needle := "ConfigCredential" + "("

	var (
		found   []string
		scanned int
	)

	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err //nolint:wrapcheck // walk error is returned verbatim by contract
		}

		if d.IsDir() {
			// Skip dot-directories (.git, and CI's .go module cache) and any
			// vendored dependency tree. Everything left is GTB's own source.
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "vendor") {
				return filepath.SkipDir
			}

			return nil
		}

		if !strings.HasSuffix(path, ".go") {
			return nil
		}

		src, err := os.ReadFile(path) //nolint:gosec // path comes from walking this repo
		if err != nil {
			return errors.WithStack(err)
		}

		scanned++

		for i, line := range strings.Split(string(src), "\n") {
			code, _, _ := strings.Cut(line, "//")
			if strings.Contains(code, needle) {
				rel, _ := filepath.Rel(root, path)
				found = append(found, fmt.Sprintf("%s:%d", rel, i+1))
			}
		}

		return nil
	}))

	// A skip rule that excluded everything would make the assertion below pass
	// while checking nothing. GTB has hundreds of Go files; this only has to be
	// high enough to prove the walk still reaches the tree.
	require.Greaterf(t, scanned, 100,
		"only %d Go files scanned — the skip rules have excluded GTB's own source, "+
			"so this guard is no longer checking anything", scanned)

	assert.Emptyf(t, found,
		"forge.ConfigCredential is called at %v — see spec 0183 D10: its stale-key "+
			"report fires on GTB's own shipped defaults, reporting working config as stale",
		found)
}

// TestResolveForgeCredentialOrigin reports WHICH rung won, and is what makes
// resolution observable to `doctor` without printing a secret. It shares one
// rung list with [vcs.ForgeCredential], so these cases double as an assertion
// that the reporter and the resolver cannot disagree about precedence.
func TestResolveForgeCredentialOrigin(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		envRef   string
		fallback string
		keychain string
		want     vcs.CredentialOrigin
	}{
		{
			name: "nothing configured",
			yaml: "",
			want: vcs.OriginNone,
		},
		{
			name:     "the fallback variable",
			yaml:     "github: {}\n",
			fallback: "from-fallback",
			want:     vcs.OriginFallbackEnv,
		},
		{
			name: "the literal",
			yaml: "github:\n  auth:\n    value: from-literal\n",
			want: vcs.OriginLiteral,
		},
		{
			name:     "the keychain reference beats the literal",
			yaml:     "github:\n  auth:\n    keychain: testtool/github.auth\n    value: from-literal\n",
			keychain: "from-keychain",
			want:     vcs.OriginKeychain,
		},
		{
			name:   "the env reference beats everything below it",
			yaml:   "github:\n  auth:\n    env: " + envRefName + "\n    value: from-literal\n",
			envRef: "from-env-ref",
			want:   vcs.OriginEnvRef,
		},
		{
			// The pointer semantics again: a named variable that is unset must
			// fall through, and the report must name the rung that actually won.
			name:   "a named env var that is unset falls through to the literal",
			yaml:   "github:\n  auth:\n    env: " + envRefName + "\n    value: from-literal\n",
			envRef: "",
			want:   vcs.OriginLiteral,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(envRefName, tc.envRef)
			t.Setenv(fallbackName, tc.fallback)

			if tc.keychain != "" {
				credtest.Install(t)
				require.NoError(t, credentials.Store(t.Context(), service, account, tc.keychain))
			}

			got, err := vcs.ResolveForgeCredentialOrigin(t.Context(), sub(t, tc.yaml), fallbackName)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestResolveForgeCredentialOrigin_BrokenRungIsReportedOnlyWhenNothingResolves
// pins the error semantics, which mirror forge.FirstCredential deliberately: a
// broken rung does not stop the walk, because a later rung may still supply a
// working credential — and then the configuration genuinely does work, so
// reporting a failure would be wrong.
func TestResolveForgeCredentialOrigin_BrokenRungIsReportedOnlyWhenNothingResolves(t *testing.T) {
	const yaml = "github:\n  auth:\n    keychain: no-slash-here\n    value: from-literal\n"

	t.Run("a lower rung rescues it", func(t *testing.T) {
		t.Setenv(envRefName, "")
		t.Setenv(fallbackName, "")

		got, err := vcs.ResolveForgeCredentialOrigin(t.Context(), sub(t, yaml), fallbackName)
		require.NoError(t, err, "a working literal means the configuration works")
		assert.Equal(t, vcs.OriginLiteral, got)
	})

	t.Run("nothing rescues it, so the reason surfaces", func(t *testing.T) {
		t.Setenv(envRefName, "")
		t.Setenv(fallbackName, "")

		_, err := vcs.ResolveForgeCredentialOrigin(
			t.Context(), sub(t, "github:\n  auth:\n    keychain: no-slash-here\n"), fallbackName)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "malformed keychain reference",
			"a bare 'no credential' would hide why")
	})
}

// TestForgeCredential_CancelledContextStopsEveryRung covers the ctx guard each
// rung opens with. A cancelled context must abort resolution rather than read
// an environment variable or reach a keychain.
func TestForgeCredential_CancelledContextStopsEveryRung(t *testing.T) {
	t.Parallel()

	// Scoped up front rather than inside each subtest: building a store takes
	// the testing context, and contextcheck reads a call made in a scope that
	// already holds a ctx as one that should have been given it.
	cases := map[string]forge.Config{
		"env reference": sub(t, "github:\n  auth:\n    env: "+envRefName+"\n"),
		"keychain":      sub(t, "github:\n  auth:\n    keychain: testtool/github.auth\n"),
		"literal":       sub(t, "github:\n  auth:\n    value: from-literal\n"),
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for name, scoped := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := vcs.ForgeCredential(scoped, fallbackName)(ctx)
			require.ErrorIs(t, err, context.Canceled)
		})
	}
}

// TestForgeCredential_IsLazy is the regression this spec is most likely to
// introduce: the layer API leads towards eager resolution, and an
// SSH-authenticating repository must never trigger a keychain lookup it does not
// need. Composing the source must touch nothing until it is called.
func TestForgeCredential_IsLazy(t *testing.T) {
	probe := &countingBackend{}
	credentials.RegisterBackend(probe)

	t.Cleanup(func() { credentials.RegisterBackend(unavailableBackend{}) })

	source := vcs.ForgeCredential(
		sub(t, "github:\n  auth:\n    keychain: testtool/github.auth\n"), fallbackName)

	assert.Zero(t, probe.retrieves, "composing the source must not reach the keychain")

	_, _ = source(t.Context())
	assert.Equal(t, 1, probe.retrieves, "calling it must reach the keychain exactly once")
}

// TestForgeCredential_MalformedKeychainRefIsDiagnosed covers the "diagnosis, not
// a bare 401" checklist item: a reference that cannot be parsed must say so
// rather than looking identical to having no credential.
func TestForgeCredential_MalformedKeychainRefIsDiagnosed(t *testing.T) {
	t.Setenv(fallbackName, "")

	_, err := vcs.ForgeCredential(
		sub(t, "github:\n  auth:\n    keychain: no-slash-here\n"), fallbackName)(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "malformed keychain reference")
}
