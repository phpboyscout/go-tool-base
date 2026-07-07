package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs"
)

func TestNewGitHubClientInstantiation(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-token")

	client, err := NewGitHubClient(ClientSettings{
		APIURL:    "https://api.github.com",
		UploadURL: "https://uploads.github.com",
		Auth:      vcs.AuthConfig{Env: "GITHUB_TOKEN"},
	})
	require.NoError(t, err)
	assert.NotNil(t, client)
}
