// Package github implements the VCS release provider and API client for GitHub
// repositories, supporting both public and token-authenticated access. It
// provides repository management, pull requests, release listing, asset
// downloads, and file retrieval through a testable [GitHubClient] interface.
// Client construction uses package-owned [ClientSettings]; GTB config
// integration lives in [ClientSettingsFromConfig].
package github
