package github

import (
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/config"
)

var integrationConfigGithub = `github:
  url:
    api: https://api.github.com
    upload: https://uploads.github.com
  auth:
    env: GITHUB_TOKEN
train:
  source:
    org: mcockayne
    repo: als-test
    branch: main
`

const (
	GitHubOrg  = "phpboyscout"
	GitHubRepo = "gtb"
)

func TestNewGitHubClientInstantiation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	cfg := config.NewReaderContainer(afero.NewOsFs(), config.WithConfigFormat("yaml"), config.WithConfigReaders(strings.NewReader(integrationConfigGithub)))
	client, err := NewGitHubClient(cfg.Sub("github"))
	require.NoError(t, err)
	assert.NotNil(t, client)
}
