package changelog

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
	conventionalcommits "github.com/leodido/go-conventionalcommits"
	ccparser "github.com/leodido/go-conventionalcommits/parser"
	"golang.org/x/mod/semver"
)

// subjectParts is the number of parts when splitting a commit message into subject and body.
const subjectParts = 2

// generateConfig holds the options for changelog generation.
type generateConfig struct {
	sinceTag    string
	maxReleases int
	includeAll  bool
}

// GenerateOption configures changelog generation.
type GenerateOption func(*generateConfig)

// WithSinceTag limits generation to releases after the given tag.
func WithSinceTag(tag string) GenerateOption {
	return func(c *generateConfig) {
		c.sinceTag = tag
	}
}

// WithMaxReleases limits output to the N most recent releases.
func WithMaxReleases(n int) GenerateOption {
	return func(c *generateConfig) {
		c.maxReleases = n
	}
}

// WithIncludeAll includes non-conventional commits under "Other".
func WithIncludeAll() GenerateOption {
	return func(c *generateConfig) {
		c.includeAll = true
	}
}

// GenerateFromRepo reads git history from the repository at repoPath and
// produces a full CHANGELOG.md string. The output is compatible with Parse().
func GenerateFromRepo(repoPath string, opts ...GenerateOption) (string, error) {
	cfg := &generateConfig{}
	for _, o := range opts {
		o(cfg)
	}

	absPath, err := filepath.Abs(repoPath)
	if err != nil {
		return "", errors.Wrap(err, "resolving repository path")
	}

	repo, err := git.PlainOpenWithOptions(absPath, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return "", errors.Wrap(err, "opening git repository")
	}

	return generateFromRepo(repo, cfg)
}

// versionTag associates a semver tag with its commit hash.
type versionTag struct {
	name   string
	hash   plumbing.Hash
	semver string // canonical semver (with "v" prefix)
}

// releaseGroup collects parsed entries for a single release version.
type releaseGroup struct {
	version string
	entries []Entry
}

func generateFromRepo(repo *git.Repository, cfg *generateConfig) (string, error) {
	tags, err := resolveVersionTags(repo)
	if err != nil {
		return "", err
	}

	// Sort tags by semver, newest first.
	sort.Slice(tags, func(i, j int) bool {
		return semver.Compare(tags[i].semver, tags[j].semver) > 0
	})

	// Apply --since filter: drop tags older than (or equal to) the since tag.
	if cfg.sinceTag != "" {
		sinceSemver := canonicalSemver(cfg.sinceTag)
		filtered := make([]versionTag, 0, len(tags))

		for _, t := range tags {
			if semver.Compare(t.semver, sinceSemver) > 0 {
				filtered = append(filtered, t)
			}
		}

		tags = filtered
	}

	// Build a map of commit hash → tag name for quick lookup.
	tagByCommit := make(map[plumbing.Hash]string, len(tags))
	for _, t := range tags {
		tagByCommit[t.hash] = t.name
	}

	// Iterate all commits and bucket them by release.
	groups, err := bucketCommits(repo, tagByCommit, cfg)
	if err != nil {
		return "", err
	}

	// Apply --releases limit.
	if cfg.maxReleases > 0 && len(groups) > cfg.maxReleases {
		groups = groups[:cfg.maxReleases]
	}

	return formatGroups(groups), nil
}

// resolveVersionTags returns all annotated and lightweight tags matching semver.
func resolveVersionTags(repo *git.Repository) ([]versionTag, error) {
	tagRefs, err := repo.Tags()
	if err != nil {
		return nil, errors.Wrap(err, "listing tags")
	}

	var tags []versionTag

	err = tagRefs.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		sv := canonicalSemver(name)

		if !semver.IsValid(sv) {
			return nil
		}

		// Resolve annotated tags to their target commit; skip unresolvable tags.
		if hash, ok := tryResolveTagToCommit(repo, ref); ok {
			tags = append(tags, versionTag{
				name:   name,
				hash:   hash,
				semver: sv,
			})
		}

		return nil
	})
	if err != nil {
		return nil, errors.Wrap(err, "iterating tags")
	}

	return tags, nil
}

// tryResolveTagToCommit dereferences an annotated tag to its commit hash.
// For lightweight tags the reference target is already the commit.
// Returns false if the tag cannot be resolved.
func tryResolveTagToCommit(repo *git.Repository, ref *plumbing.Reference) (plumbing.Hash, bool) {
	obj, err := repo.TagObject(ref.Hash())
	if err != nil {
		// Lightweight tag — the ref points directly at the commit.
		return ref.Hash(), true
	}

	commit, err := obj.Commit()
	if err != nil {
		return plumbing.ZeroHash, false
	}

	return commit.Hash, true
}

// bucketCommits walks all commits and assigns each to the appropriate release group.
func bucketCommits(repo *git.Repository, tagByCommit map[plumbing.Hash]string, cfg *generateConfig) ([]releaseGroup, error) {
	head, err := repo.Head()
	if err != nil {
		return nil, errors.Wrap(err, "resolving HEAD")
	}

	logIter, err := repo.Log(&git.LogOptions{
		From:  head.Hash(),
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, errors.Wrap(err, "reading commit log")
	}

	defer logIter.Close()

	// We build release groups by collecting commits until we hit a tag.
	// Since commits are in reverse chronological order, the first group
	// contains unreleased commits (if any), then each tag marks the boundary.
	parser := ccparser.NewMachine(
		ccparser.WithTypes(conventionalcommits.TypesConventional),
		ccparser.WithBestEffort(),
	)

	var (
		groups       []releaseGroup
		currentGroup = releaseGroup{version: "Unreleased"}
	)

	err = logIter.ForEach(func(c *object.Commit) error {
		// Skip merge commits (commits with more than one parent).
		if c.NumParents() > 1 {
			return nil
		}

		// If this commit is tagged, close the current group and start a new one.
		if tagName, ok := tagByCommit[c.Hash]; ok {
			if len(currentGroup.entries) > 0 {
				groups = append(groups, currentGroup)
			}

			currentGroup = releaseGroup{version: tagName}
		}

		entry, ok := parseCommitMessage(parser, c.Message, cfg.includeAll)
		if ok {
			currentGroup.entries = append(currentGroup.entries, entry)
		}

		return nil
	})

	if err != nil && !errors.Is(err, storer.ErrStop) {
		return nil, errors.Wrap(err, "iterating commits")
	}

	// Append the final group (the oldest release or unreleased).
	if len(currentGroup.entries) > 0 {
		groups = append(groups, currentGroup)
	}

	return groups, nil
}

// parseCommitMessage parses a single commit message using the conventional
// commits parser and returns a changelog Entry. It extracts the subject line,
// parses it for type/scope/description, checks for breaking changes, and
// filters skipped types.
func parseCommitMessage(parser conventionalcommits.Machine, message string, includeAll bool) (Entry, bool) {
	subject, isBreakingBody := extractSubject(message)
	if subject == "" {
		return Entry{}, false
	}

	msg, err := parser.Parse([]byte(subject))
	if err != nil || msg == nil || !msg.Ok() {
		if includeAll {
			return Entry{Category: CategoryOther, Description: subject}, true
		}

		return Entry{}, false
	}

	cc, ok := msg.(*conventionalcommits.ConventionalCommit)
	if !ok {
		return Entry{}, false
	}

	return buildEntry(cc, isBreakingBody)
}

// extractSubject returns the first line of the commit message and whether the
// body contains a BREAKING CHANGE footer.
func extractSubject(message string) (string, bool) {
	subject := strings.TrimSpace(strings.SplitN(message, "\n", subjectParts)[0])

	return subject, strings.Contains(message, "\nBREAKING CHANGE:")
}

// buildEntry constructs a changelog Entry from a parsed conventional commit.
func buildEntry(cc *conventionalcommits.ConventionalCommit, bodyBreaking bool) (Entry, bool) {
	entry := Entry{
		Category:    typeToCategory(cc.Type),
		Description: cc.Description,
	}

	if cc.Scope != nil {
		entry.Scope = *cc.Scope
	}

	if cc.IsBreakingChange() || bodyBreaking {
		entry.Category = CategoryBreaking
	}

	// Skip types that shouldn't appear in the changelog.
	if isSkippedType(cc.Type) && entry.Category != CategoryBreaking {
		return Entry{}, false
	}

	return entry, true
}

// typeToCategory maps conventional commit types to changelog categories.
func typeToCategory(commitType string) Category {
	switch commitType {
	case "feat":
		return CategoryFeature
	case "fix":
		return CategoryFix
	case "perf":
		return CategoryPerformance
	default:
		return CategoryOther
	}
}

// isSkippedType returns true for commit types excluded from the changelog.
func isSkippedType(commitType string) bool {
	switch commitType {
	case "test", "ci":
		return true
	default:
		return false
	}
}

// canonicalSemver ensures the version string has a "v" prefix for semver comparison.
func canonicalSemver(tag string) string {
	if !strings.HasPrefix(tag, "v") {
		return "v" + tag
	}

	return tag
}

// categoryHeading returns the markdown heading for a category.
func categoryHeading(cat Category) string {
	switch cat {
	case CategoryBreaking:
		return "Breaking Changes"
	case CategoryFeature:
		return "Features"
	case CategoryFix:
		return "Bug Fixes"
	case CategoryPerformance:
		return "Performance Improvements"
	case CategoryOther:
		return "Other"
	}

	return "Other"
}

// formatGroups renders release groups as markdown compatible with Parse().
func formatGroups(groups []releaseGroup) string {
	var b strings.Builder

	for i, g := range groups {
		if i > 0 {
			b.WriteString("\n")
		}

		b.WriteString("# ")
		b.WriteString(g.version)
		b.WriteString("\n")

		// Group entries by category, preserving order:
		// Breaking Changes → Features → Bug Fixes → Performance → Other.
		categoryOrder := []Category{
			CategoryBreaking,
			CategoryFeature,
			CategoryFix,
			CategoryPerformance,
			CategoryOther,
		}

		for _, cat := range categoryOrder {
			entries := entriesForCategory(g.entries, cat)
			if len(entries) == 0 {
				continue
			}

			b.WriteString("\n### ")
			b.WriteString(categoryHeading(cat))
			b.WriteString("\n\n")

			for _, e := range entries {
				if e.Scope != "" {
					fmt.Fprintf(&b, "* **%s:** %s\n", e.Scope, e.Description)
				} else {
					fmt.Fprintf(&b, "* %s\n", e.Description)
				}
			}
		}
	}

	return b.String()
}

// entriesForCategory filters entries matching the given category.
func entriesForCategory(entries []Entry, cat Category) []Entry {
	var result []Entry

	for _, e := range entries {
		if e.Category == cat {
			result = append(result, e)
		}
	}

	return result
}
