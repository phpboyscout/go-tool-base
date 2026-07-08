// Package gitlab implements the VCS release provider for GitLab repositories,
// supporting both public and token-authenticated access; the owner may be a
// slash-separated group path passed through to the GitLab API. Provider
// construction uses package-owned [Settings]; GTB config integration lives in
// [SettingsFromConfig].
package gitlab
