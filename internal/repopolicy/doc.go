// Package repopolicy holds tests over the repository's own policy files
// (.coverage-policy.yaml, .golangci.yaml) so an entry that names a path
// which no longer exists fails the suite instead of silently excluding
// nothing.
package repopolicy
