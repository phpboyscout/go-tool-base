// Package release provides the shared release abstractions ([Release],
// [ReleaseAsset]) and a pluggable [Provider] registry and factory. The factory
// resolves any registered backend — GitHub, GitLab, Bitbucket, Gitea/Codeberg, or
// a direct-HTTP source — from tool configuration; custom backends register via
// [Register]. Used by the self-update system.
package release
