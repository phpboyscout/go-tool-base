package ignore

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func newTestProps(fs afero.Fs) *props.Props {
	return &props.Props{FS: fs, Logger: logger.NewNoop(), Version: version.NewInfo("v1.0.0", "", "")}
}

func runSub(t *testing.T, cmd *setup.Command, root string, args ...string) string {
	t.Helper()

	c := cmd.Command
	require.NoError(t, c.Flags().Set("path", root))

	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, args))

	return out.String()
}

func TestNewCmdIgnore_WiresSubcommands(t *testing.T) {
	t.Parallel()

	p := newTestProps(afero.NewMemMapFs())
	cmd := NewCmdIgnore(p).Command

	require.Equal(t, "ignore", cmd.Name())

	got := map[string]bool{}
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}

	for _, want := range []string{"add", "remove", "list", "check"} {
		assert.Truef(t, got[want], "expected subcommand %q", want)
	}
}

func TestIgnoreAdd_CreatesWithHeaderAndIsIdempotent(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	c := newCmdIgnoreAdd(p).Command
	require.NoError(t, c.Flags().Set("path", root))

	var out bytes.Buffer
	c.SetOut(&out)

	// First add creates the file with a header and reports the addition.
	require.NoError(t, c.RunE(c, []string{"justfile"}))
	assert.Contains(t, out.String(), "added: justfile")

	body, err := afero.ReadFile(fs, filepath.Join(root, ".gtb", "ignore"))
	require.NoError(t, err)
	assert.True(t, len(body) > 0 && body[0] == '#', "created file should open with a header comment")
	assert.Contains(t, string(body), "\njustfile\n")

	// Re-adding the same pattern is a reported no-op.
	out.Reset()
	require.NoError(t, c.RunE(c, []string{"justfile"}))
	assert.Contains(t, out.String(), "already present (no-op): justfile")
}

func TestIgnoreAdd_DryRunDoesNotWrite(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	c := newCmdIgnoreAdd(p).Command
	require.NoError(t, c.Flags().Set("path", root))
	require.NoError(t, c.Flags().Set("dry-run", "true"))

	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{"justfile", "Dockerfile"}))

	assert.Contains(t, out.String(), "justfile")
	assert.Contains(t, out.String(), "Dockerfile")

	exists, err := afero.Exists(fs, filepath.Join(root, ".gtb", "ignore"))
	require.NoError(t, err)
	assert.False(t, exists, "--dry-run must not write the file")
}

func TestIgnoreRemove_DropsLineAndErrorsWhenAbsent(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "ignore"),
		[]byte("# rules\njustfile\n*.yml\n"), 0o644))

	out := runSub(t, newCmdIgnoreRemove(p), root, "justfile")
	assert.Contains(t, out, "removed: justfile")

	body, err := afero.ReadFile(fs, filepath.Join(root, ".gtb", "ignore"))
	require.NoError(t, err)
	assert.NotContains(t, string(body), "justfile")
	assert.Contains(t, string(body), "*.yml", "removing one literal line must not touch overlapping globs")

	// Removing an absent rule errors.
	c := newCmdIgnoreRemove(p).Command
	require.NoError(t, c.Flags().Set("path", root))
	c.SetOut(&bytes.Buffer{})
	require.Error(t, c.RunE(c, []string{"nope"}))
}

func TestIgnoreCheck_NamesWinningRule(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "ignore"),
		[]byte(".github/workflows/**\n!.github/workflows/release.yml\n"), 0o644))

	out := runSub(t, newCmdIgnoreCheck(p), root,
		".github/workflows/test.yml", ".github/workflows/release.yml", "cmd/tool/main.go")

	assert.Contains(t, out, ".github/workflows/**")
	assert.Contains(t, out, "!.github/workflows/release.yml")
	assert.Contains(t, out, "no matching rule")
}

func TestIgnoreList_ResolvesAgainstManifest(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	m := &generator.Manifest{
		Properties: generator.ManifestProperties{Name: "mytool"},
		Version:    generator.ManifestVersion{GoToolBase: "v1.0.0"},
		Hashes: map[string]string{
			"justfile":                "hash-a",
			"cmd/mytool/main.go":      "hash-b",
			".github/workflows/x.yml": "hash-c",
		},
	}
	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, generator.EncodeManifestFile(fs, generator.ManifestPathFor(root), m))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "ignore"),
		[]byte("justfile\nDockerfile\n"), 0o644))

	out := runSub(t, newCmdIgnoreList(p), root)

	assert.Contains(t, out, "justfile")
	assert.Contains(t, out, "ignored")
	// Dockerfile matches no tracked file -> flagged stale.
	assert.Contains(t, out, "stale rule (matches no tracked file): Dockerfile")
}

func TestIgnoreRemove_DryRunPreviewsWithoutWriting(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	require.NoError(t, fs.MkdirAll(filepath.Join(root, ".gtb"), 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(root, ".gtb", "ignore"),
		[]byte("# rules\njustfile\n*.yml\n"), 0o644))

	// --dry-run prints the resulting file (without the removed line) and writes nothing.
	c := newCmdIgnoreRemove(p).Command
	require.NoError(t, c.Flags().Set("path", root))
	require.NoError(t, c.Flags().Set("dry-run", "true"))

	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{"justfile"}))

	assert.NotContains(t, out.String(), "\njustfile\n")
	assert.Contains(t, out.String(), "*.yml")

	body, err := afero.ReadFile(fs, filepath.Join(root, ".gtb", "ignore"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "justfile", "--dry-run must not write the file")

	// --dry-run for an absent rule errors before printing.
	c2 := newCmdIgnoreRemove(p).Command
	require.NoError(t, c2.Flags().Set("path", root))
	require.NoError(t, c2.Flags().Set("dry-run", "true"))
	c2.SetOut(&bytes.Buffer{})
	require.Error(t, c2.RunE(c2, []string{"nope"}))
}

func TestPrintListing_EmptyRulesAndNoGovernedEntries(t *testing.T) {
	t.Parallel()

	// No rules at all.
	var out bytes.Buffer
	printListing(&out, &generator.IgnoreListing{})
	assert.Contains(t, out.String(), "No ignore rules configured.")

	// Rules present but nothing governed; a stale rule is reported.
	out.Reset()
	printListing(&out, &generator.IgnoreListing{
		Rules:      []string{"Dockerfile"},
		Entries:    nil,
		StaleRules: []string{"Dockerfile"},
	})
	assert.Contains(t, out.String(), "No tracked files are governed by an ignore rule.")
	assert.Contains(t, out.String(), "stale rule (matches no tracked file): Dockerfile")
}

// Spec 0188 D7 — check must report the tier, since "not ignored" and "ignored"
// can no longer describe the three states a path can be in.
func TestPrintCheck_ReportsTheTier(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	printCheck(&buf, []generator.IgnoreCheckResult{
		{Path: "a.go", State: generator.StateManaged},
		{Path: "b.go", State: generator.StateIgnored, Matched: true, Rule: "b.go"},
		{Path: "c.go", State: generator.StateSealed, Matched: true, Rule: "c.go sealed"},
	})

	out := buf.String()
	assert.Contains(t, out, "a.go")
	assert.Contains(t, out, "managed")
	assert.Contains(t, out, "ignored")
	assert.Contains(t, out, "sealed")
	assert.Contains(t, out, "(rule: c.go sealed)")
}

// Spec 0188 D8 — seal writes the attributed rule, is idempotent, and states the
// version floor at the point the hazard is created.
func TestIgnoreSeal_WritesTheAttributeAndStatesTheFloor(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	out := runSub(t, newCmdIgnoreSeal(p), root, "pkg/cmd/p/cmd.go")
	assert.Contains(t, out, "sealed: pkg/cmd/p/cmd.go")
	assert.Contains(t, out, sealedRuleMinVersion,
		"the silent-on-older-gtb hazard must be stated where the seal is written")

	body, err := afero.ReadFile(fs, filepath.Join(root, ".gtb", "ignore"))
	require.NoError(t, err)
	assert.Contains(t, string(body), "pkg/cmd/p/cmd.go sealed")

	assert.Equal(t, generator.StateSealed,
		generator.LoadIgnoreRules(fs, root).State("pkg/cmd/p/cmd.go"))

	out = runSub(t, newCmdIgnoreSeal(p), root, "pkg/cmd/p/cmd.go")
	assert.Contains(t, out, "already sealed (no-op)")
}

// D8 — unseal leaves the path ignored rather than handing it back.
func TestIgnoreUnseal_LeavesThePathIgnored(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	runSub(t, newCmdIgnoreSeal(p), root, "pkg/cmd/p/cmd.go")

	out := runSub(t, newCmdIgnoreUnseal(p), root, "pkg/cmd/p/cmd.go")
	assert.Contains(t, out, "unsealed (still ignored): pkg/cmd/p/cmd.go")

	assert.Equal(t, generator.StateIgnored,
		generator.LoadIgnoreRules(fs, root).State("pkg/cmd/p/cmd.go"),
		"unsealing must not silently re-manage the file")

	out = runSub(t, newCmdIgnoreUnseal(p), root, "pkg/cmd/p/cmd.go")
	assert.Contains(t, out, "not sealed (no-op)")
}

// D8 — `add --sealed` is the same operation for people who reach for add first.
func TestIgnoreAdd_SealedFlagSealsInstead(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	c := newCmdIgnoreAdd(p).Command
	require.NoError(t, c.Flags().Set("path", root))
	require.NoError(t, c.Flags().Set("sealed", "true"))

	var out bytes.Buffer
	c.SetOut(&out)
	require.NoError(t, c.RunE(c, []string{"pkg/cmd/p/cmd.go"}))

	assert.Contains(t, out.String(), "sealed: pkg/cmd/p/cmd.go")
	assert.Equal(t, generator.StateSealed,
		generator.LoadIgnoreRules(fs, root).State("pkg/cmd/p/cmd.go"))
}

// Sealing several paths in one invocation reports each and prints the floor once.
func TestIgnoreSeal_MultiplePathsPrintTheFloorOnce(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	p := newTestProps(fs)
	root := "/project"

	out := runSub(t, newCmdIgnoreSeal(p), root, "a.go", "b.go")

	assert.Contains(t, out, "sealed: a.go")
	assert.Contains(t, out, "sealed: b.go")
	assert.Equal(t, 1, strings.Count(out, sealedRuleMinVersion))
}
