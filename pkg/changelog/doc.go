// Package changelog generates and parses changelogs from Conventional
// Commits, in pure Go.
//
// It serves two directions:
//
//   - Generation. [GenerateFromRepo] walks a repository's git history
//     (via go-git), parses each Conventional Commit, groups the entries
//     by release and [Category], and renders a changelog — replacing an
//     external tool such as git-cliff with an in-process, dependency-free
//     implementation. Options ([WithSinceTag], [WithMaxReleases],
//     [WithIncludeAll]) bound and shape the output.
//   - Parsing. [Parse] reads existing changelog notes back into a
//     structured [Changelog]; [ParseFromArchive] extracts and parses a
//     changelog bundled in a release archive; [FormatSummary] renders a
//     concise summary of a parsed changelog.
//
// Commit classification follows the Conventional Commits specification:
// `feat`/`fix`/`perf`/etc. map to a [Category], and both the
// `BREAKING CHANGE:` footer and its `BREAKING-CHANGE:` hyphenated synonym
// mark a breaking change.
//
// The built-in `changelog` command (see pkg/cmd) embeds and displays the
// generated changelog at runtime.
package changelog
