package generator

import (
	"bytes"
	"io"
	"reflect"
	"slices"
	"strings"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go/errors"
)

// releaseRotationVerifyHook and releaseRotationGlob are the two lines a
// release that attaches a committed rotation manifest carries (#197). The
// blocks below are the skeleton's text for them, so a splice matches a render.
const (
	releaseRotationVerifyHook = "go tool releasemanifest --verify-only --rotation release/rotation --rotation-authority internal/trustkeys/rotation"
	releaseRotationGlob       = "release/rotation/rotate-keys.json{,.minisig}"

	rotationHookBlock = "    # A key-rotation manifest committed to release/rotation/ is checked against\n" +
		"    # the rotation authority before anything is built, then attached to the\n" +
		"    # release below (go-tool-base spec 0101 D12).\n" +
		"    - " + releaseRotationVerifyHook + "\n"
	rotationGlobLines = "    # Matches nothing until a key-rotation manifest is in release/rotation/.\n" +
		"    - glob: " + releaseRotationGlob + "\n"
	rotationGlobBlock = "  extra_files:\n" + rotationGlobLines
)

// anyIndent lets a removal stand whatever follows it.
const anyIndent = -1

// removal is a span gtb wrote that may come out whole, when every content line
// after it is indented no deeper than maxIndent, so nothing of the author's
// is left without its parent key.
type removal struct {
	span      string
	maxIndent int
}

// applyRotationWiring brings an existing release config's rotation lines into
// line with the signing posture, so enabling signing on a GitHub project adds
// them and disabling removes them. Everything else stays byte-for-byte. A file
// gtb may not edit, or whose shape the splice cannot place them in, is advised.
func (g *Generator) applyRotationWiring(fullPath, relPath string, content []byte, m *Manifest, mayEdit bool) ([]byte, error) {
	want := attachesRotationToRelease(m.ReleaseSource.Type, m.Properties.Signing)

	switch rotationWiringState(content, want) {
	case rotationWired:
		return content, nil
	case rotationUnplaceable:
		g.adviseRotationWiring(relPath, want)

		return content, nil
	case rotationToEdit:
	}

	splice := unspliceRotationWiring
	if want {
		splice = spliceRotationWiring
	}

	updated, ok := splice(content)
	if !ok || !mayEdit {
		g.adviseRotationWiring(relPath, want)

		return content, nil
	}

	if err := afero.WriteFile(g.props.FS, fullPath, updated, DefaultFileMode); err != nil {
		return nil, errors.Newf("failed to write %s: %w", relPath, err)
	}

	g.recordGoreleaserEdit(relPath, content, updated, m)
	g.props.Logger.Info("Updated the release config's rotation manifest lines.", "path", relPath, "attached", want)

	return updated, nil
}

type rotationState int

const (
	rotationWired rotationState = iota
	rotationUnplaceable
	rotationToEdit
)

// rotationWiringState compares the file with the posture: already as wanted,
// in a state gtb will not edit (one line without the other, or the text
// present but not where goreleaser reads it), or ready for the splice.
func rotationWiringState(content []byte, want bool) rotationState {
	hasHook := bytes.Contains(content, []byte(releaseRotationVerifyHook))
	hasGlob := bytes.Contains(content, []byte(releaseRotationGlob))

	switch {
	case hasHook != hasGlob:
		return rotationUnplaceable
	case hasHook != want:
		return rotationToEdit
	case want && !rotationPlaced(content):
		return rotationUnplaceable
	default:
		return rotationWired
	}
}

// adviseRotationWiring names the lines a release config needs by hand.
func (g *Generator) adviseRotationWiring(relPath string, want bool) {
	if !want {
		g.props.Logger.Warn("Release config not modified: remove the rotation manifest's before hook and release.extra_files entry by hand.",
			"path", relPath, "hook", releaseRotationVerifyHook, "glob", releaseRotationGlob)

		return
	}

	g.props.Logger.Warn("Release config not modified: add the rotation manifest's two lines by hand, or a revoked key never reaches your users.",
		"path", relPath, "before.hooks", releaseRotationVerifyHook, "release.extra_files.glob", releaseRotationGlob)
}

// spliceRotationWiring appends the hook to before.hooks and the glob to the
// release block, adding either block when the file has none. It refuses any
// shape it cannot place them in by line: CRLF, a missing final newline, a
// before: without hooks first, a release: that already lists extra_files, and
// any key line carrying more than the key, which checkedRotationEdit catches
// as the second key the splice would add.
func spliceRotationWiring(content []byte) ([]byte, bool) {
	if !spliceable(content) {
		return nil, false
	}

	lines := strings.SplitAfter(string(content), "\n")

	lines, ok := insertIntoBlock(lines, "before:", "  hooks:", rotationHookBlock, "before:\n  hooks:\n")
	if !ok {
		return nil, false
	}

	lines, ok = insertIntoBlock(lines, "release:", "", rotationGlobBlock, "release:\n")
	if !ok {
		return nil, false
	}

	return checkedRotationEdit(content, strings.Join(lines, ""), true)
}

// unspliceRotationWiring removes the lines spliceRotationWiring or the
// skeleton wrote, and refuses when any trace of them is left.
func unspliceRotationWiring(content []byte) ([]byte, bool) {
	if !spliceable(content) {
		return nil, false
	}

	s := string(content)

	// Both spans are found in the author's file and cut from the end first, so
	// each blank-line check sees the bytes as the author left them.
	var cuts []cut

	for _, candidates := range [][]removal{
		{{"before:\n  hooks:\n" + rotationHookBlock, 0}, {"  hooks:\n" + rotationHookBlock, 2}, {rotationHookBlock, anyIndent}},
		{{"release:\n" + rotationGlobBlock, 0}, {rotationGlobBlock, 2}, {rotationGlobLines, anyIndent}},
	} {
		for _, r := range candidates {
			if at := indexAtLineStart(s, r.span); at >= 0 && nextIndentAtMost(s[at+len(r.span):], r.maxIndent) {
				cuts = append(cuts, cut{at, len(r.span), r.maxIndent == 0})

				break
			}
		}
	}

	slices.SortFunc(cuts, func(a, b cut) int { return b.at - a.at })

	for _, c := range cuts {
		s = removeSpan(s, c.at, c.n, c.whole)
	}

	return checkedRotationEdit(content, s, false)
}

type cut struct {
	at, n int
	whole bool
}

// indexAtLineStart finds span where it starts a line, never mid-line.
func indexAtLineStart(s, span string) int {
	for from := 0; from <= len(s); {
		at := strings.Index(s[from:], span)
		if at < 0 {
			return -1
		}

		at += from
		if at == 0 || s[at-1] == '\n' {
			return at
		}

		from = at + 1
	}

	return -1
}

// removeSpan cuts n bytes at at. A whole top-level block also takes the blank
// line appendBlock put before it, and only that line.
func removeSpan(s string, at, n int, whole bool) string {
	if whole && at >= 2 && s[at-2:at] == "\n\n" {
		at--
		n++
	}

	return s[:at] + s[at+n:]
}

// rotationPlaced reports whether the file goreleaser reads holds the hook in
// before.hooks and the glob in release.extra_files, not merely the text.
func rotationPlaced(content []byte) bool {
	doc, ok := decodeOne(content)
	if !ok {
		return false
	}

	before, _ := doc["before"].(map[string]any)
	hooks, _ := before["hooks"].([]any)
	release, _ := doc["release"].(map[string]any)
	files, _ := release["extra_files"].([]any)

	return slices.Contains(hooks, any(releaseRotationVerifyHook)) && slices.ContainsFunc(files, func(f any) bool {
		entry, _ := f.(map[string]any)

		return entry["glob"] == releaseRotationGlob
	})
}

// nextIndentAtMost reports whether the first content line of rest, skipping
// blank and comment lines, is indented no deeper than limit.
func nextIndentAtMost(rest string, limit int) bool {
	if limit == anyIndent {
		return true
	}

	for l := range strings.SplitSeq(rest, "\n") {
		trimmed := strings.TrimLeft(l, " ")
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		return len(l)-len(trimmed) <= limit
	}

	return true
}

// spliceable refuses what the line splice cannot edit safely: CRLF and a
// missing final newline.
func spliceable(content []byte) bool {
	return bytes.HasSuffix(content, []byte("\n")) && !bytes.Contains(content, []byte("\r"))
}

// decodeOne decodes the single YAML document goreleaser reads, refusing a
// second one, whose lines goreleaser would never see.
func decodeOne(src []byte) (map[string]any, bool) {
	dec := yaml.NewDecoder(bytes.NewReader(src))

	var doc map[string]any
	if err := dec.Decode(&doc); err != nil && !errors.Is(err, io.EOF) {
		return nil, false
	}

	var next any

	return doc, errors.Is(dec.Decode(&next), io.EOF)
}

// checkedRotationEdit accepts an edit only when both files decode as
// goreleaser reads them, with no key given twice at any depth, and decode
// equal once the hook in before.hooks and the glob in release.extra_files are
// taken out. Nothing else has then changed meaning, and with the text holding
// both lines exactly when want, they sit where goreleaser reads them.
func checkedRotationEdit(original []byte, s string, want bool) ([]byte, bool) {
	was, ok := decodeOne(original)
	if !ok {
		return nil, false
	}

	now, ok := decodeOne([]byte(s))
	if !ok {
		return nil, false
	}

	if !stripRotationEntries(was) || !stripRotationEntries(now) || strings.Contains(s, releaseRotationVerifyHook) != want || strings.Contains(s, releaseRotationGlob) != want {
		return nil, false
	}

	if !reflect.DeepEqual(was, now) {
		return nil, false
	}

	return []byte(s), true
}

// stripRotationEntries removes the rotation hook from before.hooks and the
// glob from release.extra_files, then drops whatever those removals leave
// empty. It refuses a hooks list holding anything but strings, which
// goreleaser would not load.
func stripRotationEntries(doc map[string]any) bool {
	if !stripFrom(doc, "before", "hooks", func(v any) (bool, bool) {
		hook, isString := v.(string)

		return hook == releaseRotationVerifyHook, isString
	}) {
		return false
	}

	return stripFrom(doc, "release", "extra_files", func(v any) (bool, bool) {
		entry, _ := v.(map[string]any)

		return entry != nil && entry["glob"] == releaseRotationGlob, true
	})
}

func without(items []any, match func(any) (matched, valid bool)) ([]any, bool) {
	kept := items[:0:0]

	for _, item := range items {
		matched, valid := match(item)
		if !valid {
			return nil, false
		}

		if !matched {
			kept = append(kept, item)
		}
	}

	return kept, true
}

func stripFrom(doc map[string]any, section, list string, match func(any) (matched, valid bool)) bool {
	block, isMap := doc[section].(map[string]any)
	if doc[section] != nil && !isMap {
		return true
	}

	items, isList := block[list].([]any)
	if block[list] != nil && !isList {
		return true
	}

	kept, valid := without(items, match)
	if !valid {
		return false
	}

	if len(kept) == 0 {
		delete(block, list)
	} else if block != nil {
		block[list] = kept
	}

	if len(block) == 0 {
		delete(doc, section)
	}

	return true
}

// insertIntoBlock places insert after the last indented line of the top-level
// key's block. A non-empty child must be the block's first entry and the
// insert goes after its list; with no such key, the block is appended whole.
func insertIntoBlock(lines []string, key, child, insert, header string) ([]string, bool) {
	at := topLevelLine(lines, key)
	if at < 0 {
		return appendBlock(lines, header+insert), true
	}

	if child == "" && blockHasLine(lines, at, "  extra_files:") {
		return nil, false
	}

	if child != "" && (at+1 >= len(lines) || strings.TrimRight(lines[at+1], " \n") != child) {
		return nil, false
	}

	end := blockEnd(lines, at)

	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:end]...)
	out = append(out, insert)

	return append(out, lines[end:]...), true
}

// blockEnd is the index just past the last non-blank line of the block that
// starts at lines[at].
func blockEnd(lines []string, at int) int {
	end := at + 1

	for i := at + 1; i < len(lines) && !isTopLevel(lines[i]); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			end = i + 1
		}
	}

	return end
}

func appendBlock(lines []string, block string) []string {
	return strings.SplitAfter(strings.Join(lines, "")+"\n"+block, "\n")
}

func topLevelLine(lines []string, key string) int {
	for i, l := range lines {
		if strings.TrimRight(l, " \n") == key {
			return i
		}
	}

	return -1
}

func blockHasLine(lines []string, at int, want string) bool {
	for i := at + 1; i < len(lines) && !isTopLevel(lines[i]); i++ {
		if strings.TrimRight(lines[i], " \n") == want {
			return true
		}
	}

	return false
}

// isTopLevel reports a line that starts a new top-level key; a comment or a
// blank line belongs to whichever block surrounds it.
func isTopLevel(l string) bool {
	return l != "" && l != "\n" && l[0] != ' ' && l[0] != '#'
}
