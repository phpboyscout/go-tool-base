package generator

import (
	"path/filepath"
	"sort"

	"github.com/spf13/afero"
)

// readTrackedFile reads a manifest-tracked file by its project-relative path,
// returning nil content (and no error) when the file is absent on disk.
func readTrackedFile(fs afero.Fs, projectPath, relPath string) ([]byte, error) {
	full := filepath.Join(projectPath, relPath)

	exists, err := afero.Exists(fs, full)
	if err != nil || !exists {
		return nil, err
	}

	return afero.ReadFile(fs, full)
}

// IgnoreCheckResult reports the ignore decision for a single path, naming the
// rule that decided it. Backs `gtb ignore check`.
type IgnoreCheckResult struct {
	Path    string // the queried path
	Ignored bool   // final decision under last-match-wins + ! negation
	Matched bool   // whether any rule matched at all
	Rule    string // the winning rule line as written (empty when Matched is false)
	Negated bool   // whether the winning rule was a negation
}

// IgnoreListEntry is one tracked file and its ignore status, attributed to the
// winning rule. Backs `gtb ignore list`.
type IgnoreListEntry struct {
	Path    string
	Ignored bool
	Rule    string // winning rule line (empty when no rule matched)
}

// IgnoreListing is the resolved view `gtb ignore list` prints: every active
// rule, the tracked files each governs, and any rule matching no tracked file.
type IgnoreListing struct {
	Rules      []string          // active rule lines, in file order
	Entries    []IgnoreListEntry // tracked files governed by a rule, sorted by path
	StaleRules []string          // rules that match no tracked file
}

// CheckIgnorePaths resolves each path against the project's .gtb/ignore rules
// and returns the winning rule for each. It reads the ignore file fresh; it
// does not touch the manifest, so it works even before the first regenerate.
func (g *Generator) CheckIgnorePaths(paths []string) []IgnoreCheckResult {
	rules := LoadIgnoreRules(g.props.FS, g.config.Path)

	results := make([]IgnoreCheckResult, 0, len(paths))

	for _, path := range paths {
		rule, negated, matched := rules.Explain(path)
		results = append(results, IgnoreCheckResult{
			Path:    path,
			Ignored: rules.IsIgnored(path),
			Matched: matched,
			Rule:    rule,
			Negated: negated,
		})
	}

	return results
}

// ListIgnoreRules resolves the project's ignore rules against the files tracked
// in the manifest: each tracked file is attributed to its winning rule, and any
// rule matching no tracked file is surfaced as stale. It errors when no
// manifest exists (there is nothing to resolve against).
func (g *Generator) ListIgnoreRules() (*IgnoreListing, error) {
	manifest, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	rules := LoadIgnoreRules(g.props.FS, g.config.Path)

	listing := &IgnoreListing{Rules: rules.Rules()}

	matchedRule := make(map[string]bool, len(listing.Rules))

	tracked := make([]string, 0, len(manifest.Hashes))
	for path := range manifest.Hashes {
		tracked = append(tracked, path)
	}

	sort.Strings(tracked)

	for _, path := range tracked {
		rule, _, matched := rules.Explain(path)
		if !matched {
			continue
		}

		matchedRule[rule] = true

		listing.Entries = append(listing.Entries, IgnoreListEntry{
			Path:    path,
			Ignored: rules.IsIgnored(path),
			Rule:    rule,
		})
	}

	for _, rule := range listing.Rules {
		if !matchedRule[rule] {
			listing.StaleRules = append(listing.StaleRules, rule)
		}
	}

	return listing, nil
}

// DivergedUnignoredFiles returns the tracked files whose on-disk content no
// longer matches the hash recorded in the manifest AND which no ignore rule
// covers — precisely the set that will raise a conflict prompt on the next
// regenerate. Files that are ignored, or missing on disk, are excluded. It
// errors when no manifest exists. Backs the `doctor` diverged-files check.
func (g *Generator) DivergedUnignoredFiles() ([]string, error) {
	manifest, err := g.loadManifest()
	if err != nil {
		return nil, err
	}

	rules := LoadIgnoreRules(g.props.FS, g.config.Path)

	var diverged []string

	for relPath, storedHash := range manifest.Hashes {
		if storedHash == "" || rules.IsIgnored(relPath) {
			continue
		}

		content, err := readTrackedFile(g.props.FS, g.config.Path, relPath)
		if err != nil || content == nil {
			continue // missing on disk — nothing to overwrite, so no prompt
		}

		if calculateHash(content) != storedHash {
			diverged = append(diverged, relPath)
		}
	}

	sort.Strings(diverged)

	return diverged, nil
}
