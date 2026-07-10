package setup

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release/releasetest"
)

// injectProps builds a minimal Props sufficient for NewUpdater's release-client
// resolution (a loaded Config; Version intentionally nil — guarded).
func injectProps(tool props.Tool) *props.Props {
	return &props.Props{
		Logger: logger.NewNoop(),
		Config: config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), viper.New()),
		Tool:   tool,
	}
}

func TestNewUpdater_ReleaseClientPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("WithReleaseProvider option injects the client and skips the registry", func(t *testing.T) {
		t.Parallel()

		src := releasetest.New(releasetest.WithRelease("v1.0.0"))
		// An unknown ReleaseSource.Type would fail registry Lookup — proving the
		// option short-circuits it when resolution succeeds anyway.
		p := injectProps(props.Tool{ReleaseSource: props.ReleaseSource{Type: "no-such-provider"}})

		u, err := NewUpdater(context.Background(), p, "", false, WithReleaseProvider(src))
		require.NoError(t, err)
		assert.Same(t, src, u.releaseClient)
	})

	t.Run("props.Tool.ReleaseProvider field is used when no option is given", func(t *testing.T) {
		t.Parallel()

		src := releasetest.New(releasetest.WithRelease("v1.0.0"))
		p := injectProps(props.Tool{
			ReleaseSource:   props.ReleaseSource{Type: "no-such-provider"},
			ReleaseProvider: src,
		})

		u, err := NewUpdater(context.Background(), p, "", false)
		require.NoError(t, err)
		assert.Same(t, src, u.releaseClient)
	})

	t.Run("option takes precedence over the props field", func(t *testing.T) {
		t.Parallel()

		field := releasetest.New(releasetest.WithRelease("v1.0.0"))
		option := releasetest.New(releasetest.WithRelease("v2.0.0"))
		p := injectProps(props.Tool{ReleaseProvider: field})

		u, err := NewUpdater(context.Background(), p, "", false, WithReleaseProvider(option))
		require.NoError(t, err)
		assert.Same(t, option, u.releaseClient, "WithReleaseProvider must win over the field")
	})

	t.Run("no injection falls through to the registry lookup", func(t *testing.T) {
		t.Parallel()

		// No provider injected and an unregistered type → the registry path is
		// taken and errors, proving we did not silently skip it.
		p := injectProps(props.Tool{ReleaseSource: props.ReleaseSource{Type: "no-such-provider", Owner: "o", Repo: "r"}})

		_, err := NewUpdater(context.Background(), p, "", false)
		require.Error(t, err)
	})
}

// TestNewUpdater_InjectedProviderSkipsTokenGate proves an injected provider
// bypasses the private-repository token gate: a Private source with no token in
// the environment would normally fail requireReleaseToken, but an injected
// provider owns its own auth and must succeed.
func TestNewUpdater_InjectedProviderSkipsTokenGate(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "") // ensure no ambient token satisfies the gate

	src := releasetest.New(releasetest.WithRelease("v1.0.0"))
	p := injectProps(props.Tool{
		ReleaseSource: props.ReleaseSource{Type: "github", Owner: "o", Repo: "r", Private: true},
	})

	u, err := NewUpdater(context.Background(), p, "", false, WithReleaseProvider(src))
	require.NoError(t, err, "an injected provider must skip the private-repo token gate")
	assert.Same(t, src, u.releaseClient)
}
