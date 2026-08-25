package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	forgeapi "gitlab.com/phpboyscout/go/forge"
)

// fakeConfig is a map-backed forgeapi.Config. Keys are stored fully qualified
// ("gitlab.auth.client_id") and Sub narrows the prefix, matching how the real
// config adapter behaves — including returning nil for an absent section, which
// is the contract forge's nil guards depend on.
type fakeConfig struct {
	values map[string]string
	prefix string
}

func newFakeConfig(values map[string]string) fakeConfig {
	return fakeConfig{values: values}
}

func (f fakeConfig) GetString(key string) string { return f.values[f.prefix+key] }

func (f fakeConfig) Sub(key string) forgeapi.Config {
	full := f.prefix + key

	for k := range f.values {
		if len(k) > len(full) && k[:len(full)+1] == full+"." {
			return fakeConfig{values: f.values, prefix: full + "."}
		}
	}

	return nil
}

// TestWithShippedClientID covers spec 0185 D8. The shipped client ID must reach
// the adapter for gitlab.com and must NOT reach a self-hosted instance, where a
// gitlab.com client ID fails as an invalid client rather than degrading to the
// manual-token path the user can actually complete.
func TestWithShippedClientID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values map[string]string
		want   string
	}{
		{
			name:   "gitlab.com by default host gets the shipped ID",
			values: map[string]string{"gitlab.auth.env": "GITLAB_TOKEN"},
			want:   gitLabOAuthClientID,
		},
		{
			name: "explicit gitlab.com API URL gets the shipped ID",
			values: map[string]string{
				"gitlab.url.api": "https://gitlab.com/api/v4",
			},
			want: gitLabOAuthClientID,
		},
		{
			name: "self-hosted instance does not get the shipped ID",
			values: map[string]string{
				"gitlab.url.api": "https://git.example.com/api/v4",
			},
			want: "",
		},
		{
			name: "an operator's own client ID always wins",
			values: map[string]string{
				"gitlab.url.api":        "https://gitlab.com/api/v4",
				"gitlab.auth.client_id": "operator-supplied",
			},
			want: "operator-supplied",
		},
		{
			name: "a self-hosted operator's own client ID is preserved",
			values: map[string]string{
				"gitlab.url.api":        "https://git.example.com/api/v4",
				"gitlab.auth.client_id": "self-hosted-id",
			},
			want: "self-hosted-id",
		},
		{
			name: "an unparseable API URL does not get the shipped ID",
			values: map[string]string{
				"gitlab.url.api": "://not a url",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			wrapped := withShippedClientID(newFakeConfig(tt.values), gitLabProfile)
			section := wrapped.Sub("gitlab")
			require.NotNil(t, section, "the gitlab section must exist for this fixture")

			assert.Equal(t, tt.want, section.GetString("auth.client_id"))
		})
	}
}

// TestWithShippedClientID_LeavesOtherForgesAlone guards the wrapper's blast
// radius: only the profile's own section is intercepted.
func TestWithShippedClientID_LeavesOtherForgesAlone(t *testing.T) {
	t.Parallel()

	cfg := newFakeConfig(map[string]string{
		"gitea.auth.env": "GITEA_TOKEN",
	})

	wrapped := withShippedClientID(cfg, gitLabProfile)

	section := wrapped.Sub("gitea")
	require.NotNil(t, section)

	assert.Empty(t, section.GetString("auth.client_id"),
		"a different forge's section must not receive GitLab's client ID")
	assert.Equal(t, "GITEA_TOKEN", section.GetString("auth.env"),
		"unrelated keys must pass through untouched")
}

// TestWithShippedClientID_NoOpWithoutAShippedID pins that a profile shipping no
// client ID — every forge but GitLab — is handed through unwrapped.
func TestWithShippedClientID_NoOpWithoutAShippedID(t *testing.T) {
	t.Parallel()

	cfg := newFakeConfig(map[string]string{"gitea.auth.env": "GITEA_TOKEN"})

	assert.Equal(t, cfg, withShippedClientID(cfg, giteaProfile),
		"a profile with no shipped client ID must not be wrapped at all")
	assert.Nil(t, withShippedClientID(nil, gitLabProfile),
		"a nil config must stay nil")
}

// TestWithShippedClientID_SurvivesANamedSource covers spec 0193 D4.
//
// forgeapi.Endpoint.Section walks Sub(Type) and then Sub(Name), so a named
// source is TWO hops from the root. clientIDSection embeds forgeapi.Config, so
// before D4 the second hop was answered by the embedded Sub and came back
// undecorated: a profile shipping an OAuth client ID behaved as though it
// shipped none, with nothing failing and nothing logged.
//
// GTB keeps Endpoint.Name empty today (spec 0192 D3), so this is a guard on
// something inert. It is worth having precisely because of that: the failure is
// silent, so nothing else would catch the day a named source appears.
func TestWithShippedClientID_SurvivesANamedSource(t *testing.T) {
	t.Parallel()

	cfg := newFakeConfig(map[string]string{
		"gitlab.secondary.url.api": "https://gitlab.com/api/v4",
	})

	decorated := withShippedClientID(cfg, gitLabProfile)

	// The walk Endpoint.Section performs for Endpoint{Type: "gitlab", Name: "secondary"}.
	section := decorated.Sub("gitlab")
	require.NotNil(t, section, "the type subtree must resolve")

	named := section.Sub("secondary")
	require.NotNil(t, named, "the named subtree must resolve")

	assert.Equal(t, gitLabOAuthClientID, named.GetString("auth.client_id"),
		"the shipped client ID must survive the second hop")
}

// The host guard must survive the extra hop too. A named source pointing at a
// self-hosted instance must not receive the gitlab.com client ID, which fails
// there as an invalid client rather than degrading to manual token entry.
func TestWithShippedClientID_NamedSourceOnASelfHostedInstanceGetsNothing(t *testing.T) {
	t.Parallel()

	cfg := newFakeConfig(map[string]string{
		"gitlab.secondary.url.api": "https://gitlab.internal.example.com/api/v4",
	})

	named := withShippedClientID(cfg, gitLabProfile).Sub("gitlab").Sub("secondary")
	require.NotNil(t, named)

	assert.Empty(t, named.GetString("auth.client_id"),
		"a self-hosted named source must not inherit the gitlab.com client ID")
}
