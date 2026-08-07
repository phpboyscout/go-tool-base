package generator

// Regression coverage for GitLab issue #13 — `regenerate project` could not
// complete on a project containing any hand-modified command file, and neither
// documented escape hatch worked. Implements spec 0187's testing section.
//
// The shape every write case shares: `alpha` has a hand-edited cmd.go whose
// content no longer matches its recorded manifest hash, and `beta` sits behind
// it in the manifest. `beta` regenerating is the assertion that matters — it is
// what "the run completes" means.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/internal/testutil"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
)

const (
	issue13HandEdited = "package alpha\n\n// hand-edited, do not clobber\n"
	// A hash that matches nothing on disk, so alpha always reads as diverged.
	issue13StaleHash = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
)

const issue13Manifest = "properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n" +
	"  - name: alpha\n    hashes:\n      cmd.go: " + issue13StaleHash + "\n" +
	"  - name: beta\n"

// newIssue13Project scaffolds the drifted two-command project and returns the
// generator, its filesystem and its capturing logger.
func newIssue13Project(t *testing.T, overwrite, ignoreBody string) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	g, fs, buf := newPerimeterTestProject(t, issue13Manifest)
	g.config.Overwrite = overwrite

	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/alpha", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/alpha/cmd.go", []byte(issue13HandEdited), 0o644))

	if ignoreBody != "" {
		require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(ignoreBody), 0o644))
	}

	return g, fs, buf
}

// readIssue13Manifest returns the manifest as written back to disk.
func readIssue13Manifest(t *testing.T, fs afero.Fs) Manifest {
	t.Helper()

	raw, err := afero.ReadFile(fs, "/work/.gtb/manifest.yaml")
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(raw, &m))

	return m
}

// alphaRecordedHash returns the cmd.go hash the manifest carries for alpha.
func alphaRecordedHash(t *testing.T, fs afero.Fs) string {
	t.Helper()

	for _, cmd := range readIssue13Manifest(t, fs).Commands {
		if cmd.Name == "alpha" {
			if h := cmd.Hashes["cmd.go"]; h != "" {
				return h
			}

			return cmd.Hash
		}
	}

	t.Fatal("alpha missing from the regenerated manifest")

	return ""
}

// D2 — declining one file must not abort the run, and D3 — the declined file's
// stored hash must survive so it still conflicts next time.
func TestIssue13_DeclinedFileSkipsAndRunContinues(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "ask", "")

	require.NoError(t, g.RegenerateProject(context.Background()),
		"a declined file must not fail the whole regeneration")

	kept, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, issue13HandEdited, string(kept), "the declined file must be left exactly as it was")

	exists, _ := afero.Exists(fs, "/work/pkg/cmd/beta/cmd.go")
	assert.True(t, exists, "commands after the conflict must still regenerate")

	assert.Equal(t, issue13StaleHash, alphaRecordedHash(t, fs),
		"a kept file keeps its stored hash, so it conflicts again rather than the edit becoming the new baseline")
}

// D5 — --overwrite allow must reach the command path, not be reset to "ask" by
// the per-command config swap.
func TestIssue13_OverwriteAllowReachesCommandFiles(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, _ := newIssue13Project(t, "allow", "")

	require.NoError(t, g.RegenerateProject(context.Background()))

	written, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.NotEqual(t, issue13HandEdited, string(written), "--overwrite allow must overwrite the diverged file")
	assert.Contains(t, string(written), "NewCmdAlpha", "the overwritten file must be the regenerated one")

	exists, _ := afero.Exists(fs, "/work/pkg/cmd/beta/cmd.go")
	assert.True(t, exists)
}

// D5 — --overwrite deny is honoured as a deliberate keep rather than reaching
// the prompt, and still does not abort.
func TestIssue13_OverwriteDenyKeepsAndContinues(t *testing.T) {
	g, fs, _ := newIssue13Project(t, "deny", "")

	require.NoError(t, g.RegenerateProject(context.Background()))

	kept, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, issue13HandEdited, string(kept))

	exists, _ := afero.Exists(fs, "/work/pkg/cmd/beta/cmd.go")
	assert.True(t, exists)
}

// D4 — an ignore rule covering a command file suppresses the conflict entirely,
// and the recorded hash tracks what is on disk so removing the rule later
// resumes detection against current content.
func TestIssue13_IgnoreRuleGatesCommandFiles(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, buf := newIssue13Project(t, "ask", "pkg/cmd/alpha/cmd.go\n")

	require.NoError(t, g.RegenerateProject(context.Background()))

	kept, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, issue13HandEdited, string(kept))

	assert.False(t, buf.ContainsLevel(logger.WarnLevel, "conflict detected"),
		"an ignored file must not raise a conflict at all, got: %v", buf.Messages())

	assert.Equal(t, CalculateHash([]byte(issue13HandEdited)), alphaRecordedHash(t, fs),
		"an ignored file's recorded hash tracks disk, so removing the rule resumes detection from current content")

	exists, _ := afero.Exists(fs, "/work/pkg/cmd/beta/cmd.go")
	assert.True(t, exists)
}

// D4 — .gtb/ignore outranks --force and --overwrite allow, matching the
// precedence signing_goreleaser.go already states.
func TestIssue13_IgnoreOutranksForceAndOverwriteAllow(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	for _, tc := range []struct {
		name      string
		force     bool
		overwrite string
	}{
		{name: "force", force: true, overwrite: "ask"},
		{name: "overwrite allow", overwrite: "allow"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g, fs, _ := newIssue13Project(t, tc.overwrite, "pkg/cmd/alpha/cmd.go\n")
			g.config.Force = tc.force

			require.NoError(t, g.RegenerateProject(context.Background()))

			kept, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
			require.NoError(t, err)
			assert.Equal(t, issue13HandEdited, string(kept),
				"an ignore rule must protect the file even under %s", tc.name)
		})
	}
}

// D6 — the hint emitted for a conflicting command file must name a path that a
// rule written from it actually matches. It used to print the absolute path,
// which matchesRule can never match, so following the tool's own advice failed.
func TestIssue13_ConflictHintNamesAMatchablePath(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, fs, buf := newIssue13Project(t, "ask", "")

	require.NoError(t, g.RegenerateProject(context.Background()))

	hinted := hintedIgnorePattern(t, buf)
	require.NotEmpty(t, hinted, "expected a conflict hint naming an ignore pattern, got: %v", buf.Messages())

	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(hinted+"\n"), 0o644))

	rules := LoadIgnoreRules(fs, "/work")
	assert.True(t, rules.IsIgnored("pkg/cmd/alpha/cmd.go"),
		"a rule written from the hint (%q) must cover the file the hint was emitted for", hinted)
}

// entryLogger is the attribute-level read surface of the buffer logger. The
// hint, the remedy and the structural note are all structured attributes, so
// asserting on Messages() alone would silently pass whatever they contained.
type entryLogger interface {
	Entries() []logger.Entry
}

// attrValue returns the first value logged under key on an entry whose message
// contains msgSubstr.
func attrValue(t *testing.T, buf logBuffer, msgSubstr, key string) string {
	t.Helper()

	entries, ok := buf.(entryLogger)
	require.True(t, ok, "test logger must expose Entries()")

	for _, e := range entries.Entries() {
		if !strings.Contains(e.Message, msgSubstr) {
			continue
		}

		for i := 0; i+1 < len(e.Keyvals); i += 2 {
			if k, isString := e.Keyvals[i].(string); isString && k == key {
				return fmt.Sprint(e.Keyvals[i+1])
			}
		}
	}

	return ""
}

// hintedIgnorePattern pulls the pattern out of the "gtb ignore add <pattern>"
// clause of the conflict hint.
func hintedIgnorePattern(t *testing.T, buf logBuffer) string {
	t.Helper()

	_, after, found := strings.Cut(attrValue(t, buf, "conflict detected", "hint"), "gtb ignore add ")
	if !found {
		return ""
	}

	pattern, _, _ := strings.Cut(after, ")")

	return strings.TrimSpace(pattern)
}

// D8 — the run ends with a summary naming what it kept, with a working remedy.
func TestIssue13_SummaryNamesKeptFilesAndRemedy(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, _, buf := newIssue13Project(t, "ask", "")

	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.True(t, buf.ContainsLevel(logger.WarnLevel, "kept your version"),
		"expected the summary to name the kept file, got: %v", buf.Messages())
	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "1 file kept"),
		"expected a summary count, got: %v", buf.Messages())
	assert.Equal(t, "gtb ignore add pkg/cmd/alpha/cmd.go", attrValue(t, buf, "kept your version", "remedy"),
		"expected the summary to carry the working remedy")
	assert.Equal(t, "pkg/cmd/alpha/cmd.go", attrValue(t, buf, "kept your version", "path"))
}

// D8 — ignored files are counted, not listed as skips.
func TestIssue13_SummaryCountsIgnoredSeparatelyFromKept(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, _, buf := newIssue13Project(t, "ask", "pkg/cmd/alpha/cmd.go\n")

	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.True(t, buf.ContainsLevel(logger.InfoLevel, "ignored"),
		"expected an ignored count in the summary, got: %v", buf.Messages())
	assert.False(t, buf.ContainsLevel(logger.WarnLevel, "kept your version"),
		"an ignored file was declared, not encountered — it must not be reported as kept")
}

// D12 — on the ordinary path a kept parent is not left broken: the child's own
// registration step patches the kept file, so the check reports nothing. This
// pins the behaviour the check must not contradict.
func TestIssue13_KeptParentIsRepairedByChildRegistration(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	manifest := "properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n" +
		"  - name: alpha\n    hashes:\n      cmd.go: " + issue13StaleHash + "\n" +
		"    commands:\n      - name: nested\n"

	g, fs, buf := newPerimeterTestProject(t, manifest)

	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/alpha", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/alpha/cmd.go",
		[]byte("package alpha\n\nfunc NewCmdAlpha() {}\n"), 0o644))

	require.NoError(t, g.RegenerateProject(context.Background()))

	kept, err := afero.ReadFile(fs, "/work/pkg/cmd/alpha/cmd.go")
	require.NoError(t, err)
	assert.Contains(t, string(kept), "NewCmdNested",
		"the child's registration step must wire itself into the kept parent")

	assert.Empty(t, attrValue(t, buf, "out of step with the manifest", "detail"),
		"a kept parent that ends up correctly wired must not be reported as adrift")
}

// D12 — when the kept parent genuinely does not register a manifest child, the
// summary names the subcommand rather than leaving it to the compiler.
func TestIssue13_UnregisteredChildIsNamedInSummary(t *testing.T) {
	g, fs, buf := newPerimeterTestProject(t, issue13Manifest)

	require.NoError(t, fs.MkdirAll("/work/pkg/cmd/alpha", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/work/pkg/cmd/alpha/cmd.go",
		[]byte("package alpha\n\nfunc NewCmdAlpha() {}\n"), 0o644))

	rel := "pkg/cmd/alpha/cmd.go"
	g.conflicts.recordKeep(rel, keepReasonDeclined)
	g.recordChildCheck("/work/pkg/cmd/alpha", ManifestCommand{
		Name:     "alpha",
		Commands: []ManifestCommand{{Name: "nested"}},
	})

	g.reportConflicts()

	assert.Contains(t, attrValue(t, buf, "out of step with the manifest", "detail"), "'nested'",
		"expected the summary to name the unregistered subcommand")
}

// D10 — both hash namespaces are one tracked-file view, so `ignore list` stops
// calling a live rule stale and `doctor` can see a diverged command file.
func TestIssue13_TrackedFilesSpansBothHashNamespaces(t *testing.T) {
	g, _, _ := newIssue13Project(t, "ask", "pkg/cmd/alpha/cmd.go\n")

	listing, err := g.ListIgnoreRules()
	require.NoError(t, err)

	assert.Empty(t, listing.StaleRules,
		"a rule that ignore check honours must not be reported stale by ignore list")
	require.Len(t, listing.Entries, 1)
	assert.Equal(t, "pkg/cmd/alpha/cmd.go", listing.Entries[0].Path)

	checked := g.CheckIgnorePaths([]string{"pkg/cmd/alpha/cmd.go"})
	assert.True(t, checked[0].Ignored, "ignore check and ignore list must agree")
}

func TestIssue13_DivergedUnignoredSeesCommandFiles(t *testing.T) {
	g, _, _ := newIssue13Project(t, "ask", "")

	diverged, err := g.DivergedUnignoredFiles()
	require.NoError(t, err)

	assert.Contains(t, diverged, "pkg/cmd/alpha/cmd.go",
		"doctor must see the command files that will block a regenerate")
}

func TestIssue13_DivergedUnignoredExcludesIgnoredCommandFiles(t *testing.T) {
	g, _, _ := newIssue13Project(t, "ask", "pkg/cmd/alpha/cmd.go\n")

	diverged, err := g.DivergedUnignoredFiles()
	require.NoError(t, err)

	assert.NotContains(t, diverged, "pkg/cmd/alpha/cmd.go",
		"a declared divergence is not a doctor finding")
}

// D7 — the --ci flag / `ci` config key implies non-interactive, as it does
// everywhere else in the toolchain. Reading only the CI environment variable
// meant --ci in a terminal still prompted.
func TestIssue13_CIConfigKeyImpliesNonInteractive(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "")
	t.Setenv("CI", "")

	g, _, _ := newIssue13Project(t, "ask", "")
	require.False(t, g.ciConfigured(), "the fixture store must not already report CI")

	g.props.Config = testutil.StoreFromYAML(t, "ci: true\n")

	assert.True(t, g.ciConfigured(), "the resolved ci config key must be visible to the generator")
	assert.True(t, g.isNonInteractive(), "--ci must suppress the conflict prompt")
}

// D11 — a dry run must not log a manifest write it does not perform.
func TestIssue13_DryRunDoesNotClaimAManifestWrite(t *testing.T) {
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, _, buf := newIssue13Project(t, "ask", "")
	g.config.DryRun = true

	require.NoError(t, g.RegenerateProject(context.Background()))

	assert.False(t, buf.ContainsLevel(logger.DebugLevel, "manifest updated successfully"),
		"a dry run must not report the manifest as updated, got: %v", buf.Messages())
}
