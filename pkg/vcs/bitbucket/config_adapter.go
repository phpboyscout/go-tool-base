package bitbucket

import (
	"context"

	"gitlab.com/phpboyscout/go-tool-base/pkg/vcs/release"
)

// SettingsFromConfig adapts the bitbucket config subtree into typed provider
// settings. It preserves the existing credential resolution chain and
// filename_pattern release-source param.
func SettingsFromConfig(src release.ReleaseSourceConfig, cfg release.Config) (Settings, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bitbucketKeychainTimeout)
	defer cancel()

	username, appPassword, err := resolveCredentials(ctx, cfg)
	if err != nil {
		return Settings{}, err
	}

	return Settings{
		ReleaseSource:   src,
		Username:        username,
		AppPassword:     appPassword,
		FilenamePattern: src.Params["filename_pattern"],
	}, nil
}
