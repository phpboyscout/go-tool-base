// Package vcs defines the version-control abstraction layer for querying releases
// and repository metadata across multiple forge backends.
//
// The release-source registry supports GitHub, GitLab, Bitbucket,
// Gitea/Forgejo/Codeberg, and a direct-HTTP source. Sub-packages provide the
// concrete implementations (github, gitlab, bitbucket, gitea, direct), release for
// the pluggable provider registry and factory, and repo for repository URL parsing
// and metadata extraction.
package vcs
