package setup

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/config"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestSelfUpdater_GetCurrentVersion(t *testing.T) {
	t.Parallel()

	s := &SelfUpdater{CurrentVersion: "v1.2.3"}
	assert.Equal(t, "v1.2.3", s.GetCurrentVersion())
}

// TestSelfUpdater_resolveTargetPath covers the non-interactive branches of the
// install-target resolver. The interactive huh.Select path needs a TTY and is
// left to manual/E2E.
func TestSelfUpdater_resolveTargetPath(t *testing.T) {
	t.Parallel()

	t.Run("osExecutable error propagates", func(t *testing.T) {
		t.Parallel()

		s := &SelfUpdater{
			logger:       logger.NewNoop(),
			Tool:         props.Tool{Name: "tool"},
			osExecutable: func() (string, error) { return "", errors.New("no executable") },
		}

		_, err := s.resolveTargetPath()
		require.Error(t, err)
	})

	t.Run("matching paths return the running executable", func(t *testing.T) {
		t.Parallel()

		s := &SelfUpdater{
			logger:       logger.NewNoop(),
			Tool:         props.Tool{Name: "tool"},
			osExecutable: func() (string, error) { return "/usr/local/bin/tool", nil },
			execLookPath: func(string) (string, error) { return "/usr/local/bin/tool", nil },
		}

		got, err := s.resolveTargetPath()
		require.NoError(t, err)
		assert.Equal(t, "/usr/local/bin/tool", got)
	})

	t.Run("differing paths, non-interactive, defaults to the running executable", func(t *testing.T) {
		t.Parallel()

		s := &SelfUpdater{
			logger:        logger.NewNoop(),
			Tool:          props.Tool{Name: "tool"},
			osExecutable:  func() (string, error) { return "/run/tool", nil },
			execLookPath:  func(string) (string, error) { return "/on/path/tool", nil },
			isInteractive: func() bool { return false },
		}

		got, err := s.resolveTargetPath()
		require.NoError(t, err)
		assert.Equal(t, "/run/tool", got, "non-interactive must update the running executable, not block on a prompt")
	})
}

func TestNewUpdater_NilConfigErrors(t *testing.T) {
	t.Parallel()

	_, err := NewUpdater(context.Background(), &props.Props{}, "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "configuration is not loaded")
}

func TestNewUpdater_UnknownProviderErrors(t *testing.T) {
	t.Parallel()

	v := viper.New()
	v.Set("vcs.provider", "bogus-provider")

	p := &props.Props{
		Logger: logger.NewNoop(),
		Config: config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), v),
		Tool:   props.Tool{ReleaseSource: props.ReleaseSource{Type: "github", Owner: "o", Repo: "r"}},
	}

	_, err := NewUpdater(context.Background(), p, "", false)
	require.Error(t, err, "an unknown vcs.provider must fail before constructing a release client")
}

// TestRequireReleaseToken drives the private-repo token gate across providers.
// It mutates process env via t.Setenv, so it is not parallel.
func TestRequireReleaseToken(t *testing.T) {
	ctx := context.Background()
	p := &props.Props{Logger: logger.NewNoop(), Config: config.NewContainerFromViper(logger.ToSlog(logger.NewNoop()), viper.New())}

	t.Run("bitbucket needs no single token", func(t *testing.T) {
		require.NoError(t, requireReleaseToken(ctx, "bitbucket", p))
	})

	t.Run("github without a token errors with a hint", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "")

		err := requireReleaseToken(ctx, "github", p)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no GITHUB token")
	})

	t.Run("github with an env token succeeds", func(t *testing.T) {
		t.Setenv("GITHUB_TOKEN", "ghp_secret")
		require.NoError(t, requireReleaseToken(ctx, "github", p))
	})

	t.Run("gitlab without a token errors", func(t *testing.T) {
		t.Setenv("GITLAB_TOKEN", "")
		require.Error(t, requireReleaseToken(ctx, "gitlab", p))
	})
}
