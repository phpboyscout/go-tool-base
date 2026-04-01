package github

import (
	"fmt"
	"os"

	"github.com/cli/oauth"
)

var (
	ghClientID string
)

func init() {
	if ghClientID == "" {
		ghClientID = os.Getenv("GITHUB_CLIENT_ID")
	}
}

// GHLogin authenticates with GitHub using the gh CLI device flow.
func GHLogin(hostname string) (string, error) {
	host, err := oauth.NewGitHubHost(fmt.Sprintf("https://%s", hostname))
	if err != nil {
		return "", err
	}

	flow := &oauth.Flow{
		Host:     host,
		ClientID: ghClientID,
		// ClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"), // only applicable to web app flow
		// CallbackURI:  "http://127.0.0.1/callback",      // only applicable to web app flow
		Scopes: []string{"repo", "read:org", "gist"},
	}

	accessToken, err := flow.DetectFlow()
	if err != nil {
		return "", err
	}

	return accessToken.Token, nil
}
