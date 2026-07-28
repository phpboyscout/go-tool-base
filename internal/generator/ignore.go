package generator

import (
	"bufio"
	"path/filepath"
	"strings"

	"github.com/spf13/afero"
)

const ignoreFileName = "ignore"

// ignoreConflictHint names .gtb/ignore as the remedy when a manually-modified
// file conflicts on regenerate, putting the answer where the question arises.
func ignoreConflictHint(relPath string) string {
	return "add '" + filepath.ToSlash(relPath) + "' to .gtb/ignore (or run: gtb ignore add " +
		filepath.ToSlash(relPath) + ") to keep your changes"
}

// ignoreRule is a single pattern from the .gtb/ignore file.
type ignoreRule struct {
	raw      string // the original rule line as written (trimmed), including any ! or trailing /
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

// Explain evaluates all rules top-to-bottom and returns the winning rule for
// the given relative path: the last rule that matched (last-match-wins), the
// original rule line as written (with any ! or trailing /), whether that rule
// was a negation, and whether any rule matched at all. When matched is false
// the path is governed by no rule (and is therefore not ignored). This backs
// `gtb ignore check`, which must name the deciding rule — a question the flat
// file cannot answer, because negation means the winner can be a later ! line.
func (r *IgnoreRules) Explain(relPath string) (rule string, negated, matched bool) {
	relPath = filepath.ToSlash(relPath)

	for _, candidate := range r.rules {
		if matchesRule(relPath, candidate) {
			rule = candidate.raw
			negated = candidate.negate
			matched = true
		}
	}

	return rule, negated, matched
}

// Rules returns the original rule lines in file order (with any ! or trailing
// / preserved). It backs `gtb ignore list`, which must surface every active
// rule and flag those that currently match no tracked file.
func (r *IgnoreRules) Rules() []string {
	lines := make([]string, 0, len(r.rules))
	for _, rule := range r.rules {
		lines = append(lines, rule.raw)
	}

	return lines
}

// ignoreFileHeader is written when `ignore add` (or the scaffold) first creates
// a project's .gtb/ignore. It is comment-only, so it ignores nothing on its own
// (see IsIgnored), while making the mechanism discoverable in place.
const ignoreFileHeader = `# .gtb/ignore — mark generated files hands-off for 'gtb regenerate'.
#
# One pattern per line. Patterns are evaluated top-to-bottom; a later rule wins,
# and a leading '!' re-includes a file an earlier rule excluded.
#
#   justfile                      a filename, matched in any directory
#   *.yml                         a glob, matched in any directory
#   .github/**                    everything under a directory
#   .github/workflows/ci.yml      an exact path
#   !.github/workflows/release.yml  keep this one managed by the generator
#
# Manage it with 'gtb ignore add|remove|list|check', or edit it by hand.
# See https://gtb.phpboyscout.uk/how-to/configure-generator-ignore/
`

// ignorePath returns the .gtb/ignore path for a project root.
func ignorePath(projectPath string) string {
	return filepath.Join(projectPath, ".gtb", ignoreFileName)
}

// ScaffoldIgnoreFile writes a fresh, comment-only .gtb/ignore (the header and
// nothing else) into a project, creating the .gtb directory as needed. It is a
// no-op when the file already exists, so it never clobbers a user's rules. A
// comment-only file ignores nothing, so scaffolding it is behaviourally inert —
// its whole value is discoverability.
func ScaffoldIgnoreFile(fs afero.Fs, projectPath string) error {
	path := ignorePath(projectPath)

	if exists, _ := afero.Exists(fs, path); exists {
		return nil
	}

	if err := fs.MkdirAll(filepath.Dir(path), DefaultDirMode); err != nil {
		return err
	}

	return afero.WriteFile(fs, path, []byte(ignoreFileHeader), DefaultFileMode)
}

// AppendIgnorePattern appends a single pattern to a project's .gtb/ignore,
// creating the file (with an explanatory header) when it is absent. It is
// idempotent: re-adding a pattern already present as a rule line leaves the
// file byte-identical and reports changed=false. Existing comments, blank
// lines, and ordering are preserved — the pattern is appended after them.
func AppendIgnorePattern(fs afero.Fs, projectPath, pattern string) (changed bool, err error) {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false, nil
	}

	existing, existed, err := readIgnoreBytes(fs, projectPath)
	if err != nil {
		return false, err
	}

	next, changed := appendIgnoreLine(existing, existed, pattern)
	if !changed {
		return false, nil
	}

	if err := writeIgnoreBytes(fs, projectPath, next); err != nil {
		return false, err
	}

	return true, nil
}

// RemoveIgnorePattern drops the exact literal rule line matching pattern from a
// project's .gtb/ignore, preserving every other line (comments, blanks, and
// ordering). Matching is on the literal rule line, not on a path the pattern
// happens to glob, so `remove justfile` never touches an overlapping `*.yml`.
// It reports changed=false when no such line exists.
func RemoveIgnorePattern(fs afero.Fs, projectPath, pattern string) (changed bool, err error) {
	pattern = strings.TrimSpace(pattern)

	existing, existed, err := readIgnoreBytes(fs, projectPath)
	if err != nil || !existed {
		return false, err
	}

	next, changed := removeIgnoreLine(existing, pattern)
	if !changed {
		return false, nil
	}

	if err := writeIgnoreBytes(fs, projectPath, next); err != nil {
		return false, err
	}

	return true, nil
}

// PreviewAppendIgnorePatterns returns what .gtb/ignore would contain after
// appending patterns in sequence, composing them in memory without writing
// anything. It backs `--dry-run` on `gtb ignore add` with multiple patterns.
func PreviewAppendIgnorePatterns(fs afero.Fs, projectPath string, patterns []string) (content string, err error) {
	current, existed, err := readIgnoreBytes(fs, projectPath)
	if err != nil {
		return "", err
	}

	for _, pattern := range patterns {
		next, changed := appendIgnoreLine(current, existed, strings.TrimSpace(pattern))
		if changed {
			current = next
			existed = true
		}
	}

	return string(current), nil
}

// PreviewRemoveIgnorePattern returns what .gtb/ignore would contain after
// removing pattern, without writing anything. It backs `--dry-run` on
// `gtb ignore remove`.
func PreviewRemoveIgnorePattern(fs afero.Fs, projectPath, pattern string) (content string, changed bool, err error) {
	existing, existed, err := readIgnoreBytes(fs, projectPath)
	if err != nil || !existed {
		return string(existing), false, err
	}

	next, changed := removeIgnoreLine(existing, strings.TrimSpace(pattern))

	return string(next), changed, nil
}

// readIgnoreBytes reads a project's .gtb/ignore, reporting whether it existed.
// A missing file is not an error: it returns nil bytes and existed=false.
func readIgnoreBytes(fs afero.Fs, projectPath string) (content []byte, existed bool, err error) {
	path := ignorePath(projectPath)

	exists, err := afero.Exists(fs, path)
	if err != nil {
		return nil, false, err
	}

	if !exists {
		return nil, false, nil
	}

	b, err := afero.ReadFile(fs, path)

	return b, true, err
}

// writeIgnoreBytes writes content to a project's .gtb/ignore, creating .gtb.
func writeIgnoreBytes(fs afero.Fs, projectPath string, content []byte) error {
	path := ignorePath(projectPath)
	if err := fs.MkdirAll(filepath.Dir(path), DefaultDirMode); err != nil {
		return err
	}

	return afero.WriteFile(fs, path, content, DefaultFileMode)
}

// appendIgnoreLine computes the .gtb/ignore body after appending pattern. When
// the file did not exist it seeds the explanatory header first. It returns the
// existing bytes unchanged (changed=false) when pattern is already present as a
// rule line, so callers can report an idempotent no-op.
func appendIgnoreLine(existing []byte, existed bool, pattern string) (next []byte, changed bool) {
	if pattern == "" {
		return existing, false
	}

	if ignoreLinePresent(existing, pattern) {
		return existing, false
	}

	var b strings.Builder

	switch {
	case !existed || len(existing) == 0:
		b.WriteString(ignoreFileHeader)
	default:
		b.Write(existing)

		if existing[len(existing)-1] != '\n' {
			b.WriteByte('\n')
		}
	}

	b.WriteString(pattern)
	b.WriteByte('\n')

	return []byte(b.String()), true
}

// removeIgnoreLine computes the .gtb/ignore body after dropping every line whose
// trimmed content equals pattern. Comments and blanks are preserved verbatim.
func removeIgnoreLine(existing []byte, pattern string) (next []byte, changed bool) {
	if pattern == "" {
		return existing, false
	}

	lines := strings.Split(string(existing), "\n")
	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			changed = true

			continue
		}

		kept = append(kept, line)
	}

	if !changed {
		return existing, false
	}

	return []byte(strings.Join(kept, "\n")), true
}

// ignoreLinePresent reports whether pattern already appears as a rule line
// (a non-comment, non-blank line whose trimmed content equals pattern).
func ignoreLinePresent(existing []byte, pattern string) bool {
	for _, line := range strings.Split(string(existing), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if trimmed == pattern {
			return true
		}
	}

	return false
}

func parseIgnoreRule(line string) ignoreRule {
	rule := ignoreRule{raw: line}

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
