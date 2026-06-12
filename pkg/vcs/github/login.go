package github

import (
	"context"
	"fmt"
	"os"

	"github.com/cli/oauth"

	"gitlab.com/phpboyscout/go-tool-base/pkg/browser"
)

var (
	ghClientID string
)

func init() {
	if ghClientID == "" {
		ghClientID = os.Getenv("GITHUB_CLIENT_ID")
	}
}

// newLoginFlow builds the GitHub device-flow configuration. The browser
// opener is routed through pkg/browser.OpenURL so the verification URL is
// validated against the scheme allowlist; leaving BrowseURL nil would fall
// back to cli/browser, bypassing that gate.
func newLoginFlow(hostname string) (*oauth.Flow, error) {
	host, err := oauth.NewGitHubHost(fmt.Sprintf("https://%s", hostname))
	if err != nil {
		return nil, err
	}

	return &oauth.Flow{
		Host:     host,
		ClientID: ghClientID,
		// ClientSecret: os.Getenv("OAUTH_CLIENT_SECRET"), // only applicable to web app flow
		// CallbackURI:  "http://127.0.0.1/callback",      // only applicable to web app flow
		Scopes: []string{"repo", "read:org", "gist"},
		BrowseURL: func(u string) error {
			return browser.OpenURL(context.Background(), u)
		},
	}, nil
}

// GHLogin authenticates with GitHub using the gh CLI device flow.
func GHLogin(hostname string) (string, error) {
	flow, err := newLoginFlow(hostname)
	if err != nil {
		return "", err
	}

	accessToken, err := flow.DetectFlow()
	if err != nil {
		return "", err
	}

	return accessToken.Token, nil
}
