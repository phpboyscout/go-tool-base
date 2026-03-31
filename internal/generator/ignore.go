package generator

import (
	"bufio"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

const ignoreFileName = "ignore"

// ignoreRule is a single pattern from the .gtb/ignore file.
type ignoreRule struct {
	pattern  string
	negate   bool
	dirOnly  bool // trailing / means directory-only match
	hasSlash bool // contains path separator — anchored match
}

// IgnoreRules holds compiled ignore patterns from a .gtb/ignore file.
// Patterns are evaluated top-to-bottom; later patterns override earlier ones.
// Negation (!) re-includes a previously excluded file.
type IgnoreRules struct {
	rules []ignoreRule
}

// LoadIgnoreRules reads the .gtb/ignore file from the project directory.
// Returns empty rules (nothing ignored) if the file doesn't exist.
func LoadIgnoreRules(fs afero.Fs, projectPath string) *IgnoreRules {
	path := filepath.Join(projectPath, ".gtb", ignoreFileName)

	f, err := fs.Open(path)
	if err != nil {
		return &IgnoreRules{}
	}

	defer func() { _ = f.Close() }()

	var rules []ignoreRule

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		rule := parseIgnoreRule(line)
		rules = append(rules, rule)
	}

	return &IgnoreRules{rules: rules}
}

// IsIgnored evaluates all rules top-to-bottom and returns whether the
// given relative path should be ignored. Negation patterns (!) can
// re-include files excluded by earlier patterns.
func (r *IgnoreRules) IsIgnored(relPath string) bool {
	if len(r.rules) == 0 {
		return false
	}

	// Normalise to forward slashes for consistent matching
	relPath = filepath.ToSlash(relPath)

	ignored := false

	for _, rule := range r.rules {
		if matchesRule(relPath, rule) {
			ignored = !rule.negate
		}
	}

	return ignored
}

func parseIgnoreRule(line string) ignoreRule {
	rule := ignoreRule{}

	if strings.HasPrefix(line, "!") {
		rule.negate = true
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		rule.dirOnly = true
		line = strings.TrimSuffix(line, "/")
	}

	rule.hasSlash = strings.Contains(line, "/")
	rule.pattern = line

	return rule
}

// matchesRule checks if a relative path matches a single ignore rule.
// Supports:
//   - Simple globs: *.yml, justfile
//   - Directory globs: .github/** (matches everything under .github)
//   - Path-anchored patterns: .github/workflows/release.yml
//   - Basename-only patterns: *.yml matches foo/bar.yml
func matchesRule(relPath string, rule ignoreRule) bool {
	pattern := rule.pattern

	// Handle ** (recursive directory match)
	if prefix, ok := strings.CutSuffix(pattern, "/**"); ok {
		return relPath == prefix || strings.HasPrefix(relPath, prefix+"/")
	}

	if before, after, ok := strings.Cut(pattern, "/**/"); ok {
		return strings.HasPrefix(relPath, before+"/") &&
			matchSimpleGlob(filepath.Base(relPath), after)
	}

	// Anchored match: pattern contains a slash, so match against full path
	if rule.hasSlash {
		matched, _ := filepath.Match(pattern, relPath)

		return matched
	}

	// Basename match: no slash in pattern, match against filename only
	matched, _ := filepath.Match(pattern, filepath.Base(relPath))

	return matched
}

// matchSimpleGlob is a thin wrapper around filepath.Match that handles
// the case where the pattern itself may contain path separators.
func matchSimpleGlob(name, pattern string) bool {
	matched, _ := filepath.Match(pattern, name)

	return matched
}
