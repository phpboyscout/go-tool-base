package generator

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

var releaseSigning = ManifestSigning{Enabled: true, KeyID: "alias/release", KMSRegion: "eu-west-2", Backend: "aws-kms"}

func forgeSkeletonConfig(backend props.FeatureID, host string, signing ManifestSigning) SkeletonConfig {
	cfg := signingSkeletonConfig("/work", signing)
	cfg.ForgeBackend = backend
	cfg.Host = host

	return cfg
}

// A signed project on a forge channel goreleaser creates the release for
// verifies a committed rotation manifest before building and attaches it to
// the release (#197, #221).
func TestGenerateSkeleton_ForgeReleaseCarriesTheRotationManifest(t *testing.T) {
	t.Parallel()

	for name, cfg := range map[string]SkeletonConfig{
		"github":   signingSkeletonConfig("/work", releaseSigning),
		"gitea":    forgeSkeletonConfig("gitea", "git.example.org", releaseSigning),
		"codeberg": forgeSkeletonConfig("codeberg", "codeberg.org", releaseSigning),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := newSkeletonGeneratorForTest(t, fs)
			require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

			raw := readGenerated(t, fs, "/work/.goreleaser.yaml")
			assert.Contains(t, raw, rotationHookBlock, "the splice inserts the skeleton's own text")
			assert.Contains(t, raw, rotationGlobBlock)

			var doc struct {
				Before struct {
					Hooks []string `yaml:"hooks"`
				} `yaml:"before"`
				Release struct {
					ExtraFiles []struct {
						Glob string `yaml:"glob"`
					} `yaml:"extra_files"`
				} `yaml:"release"`
			}
			require.NoError(t, yaml.Unmarshal([]byte(raw), &doc))

			assert.Contains(t, doc.Before.Hooks, releaseRotationVerifyHook)
			require.Len(t, doc.Release.ExtraFiles, 1)
			assert.Equal(t, releaseRotationGlob, doc.Release.ExtraFiles[0].Glob,
				"a pattern, so goreleaser matches nothing rather than failing before the first rotation")
			assert.Contains(t, readGenerated(t, fs, "/work/go.mod"), releaseManifestTool)
		})
	}
}

func TestGenerateSkeleton_ForgeReleaseWithoutTheRotationManifest(t *testing.T) {
	t.Parallel()

	cases := map[string]SkeletonConfig{
		"an unsigned GitHub project has no authority to verify against": signingSkeletonConfig("/work", ManifestSigning{}),
		"a signed GitLab project waits on colophon to link it (colophon#76)": func() SkeletonConfig {
			cfg := gitlabReleaseConfig(ReleaseChannelForge)
			cfg.Signing = releaseSigning

			return cfg
		}(),
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fs := afero.NewMemMapFs()
			g := newSkeletonGeneratorForTest(t, fs)
			require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

			gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
			assert.NotContains(t, gr, "--verify-only")
			assert.NotContains(t, gr, "rotate-keys.json")
			assert.NotContains(t, readGenerated(t, fs, "/work/go.mod"), releaseManifestTool)
		})
	}
}

// newUnsignedProject generates a project without signing, as one that enables
// it later starts.
func newUnsignedProject(t *testing.T, cfg SkeletonConfig) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	fs := afero.NewMemMapFs()
	buf := logger.NewBuffer()
	g := New(&props.Props{FS: fs, Logger: buf, Config: emptyTestStore(t)}, &Config{Path: "/work"})
	g.runCommand = func(context.Context, string, string, ...string) ([]byte, error) { return nil, nil }
	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	return g, fs, buf
}

func newUnsignedGitHubProject(t *testing.T) (*Generator, afero.Fs, logBuffer) {
	t.Helper()

	return newUnsignedProject(t, signingSkeletonConfig("/work", ManifestSigning{}))
}

// Enabling signing later must wire the rotation manifest too, or a project
// that then revokes a key ships releases without it (#197).
func TestEnableSigning_ForgeProjectGainsTheRotationWiring(t *testing.T) {
	t.Parallel()

	cases := map[string]SkeletonConfig{
		"github":   signingSkeletonConfig("/work", ManifestSigning{}),
		"gitea":    forgeSkeletonConfig("gitea", "git.example.org", ManifestSigning{}),
		"codeberg": forgeSkeletonConfig("codeberg", "codeberg.org", ManifestSigning{}),
	}

	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assertSigningTogglesRotationWiring(t, cfg)
		})
	}
}

func assertSigningTogglesRotationWiring(t *testing.T, cfg SkeletonConfig) {
	t.Helper()

	g, fs, _ := newUnsignedProject(t, cfg)
	require.NotContains(t, readGenerated(t, fs, "/work/.goreleaser.yaml"), "--verify-only")

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, releaseRotationVerifyHook)
	assert.Contains(t, gr, releaseRotationGlob)
	assert.Contains(t, gr, "signs:")
	assert.Contains(t, readGenerated(t, fs, "/work/go.mod"), releaseManifestTool)

	require.NoError(t, g.DisableSigning(context.Background()))

	gr = readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.NotContains(t, gr, "--verify-only", "disabling removes the wiring from an unedited file")
	assert.NotContains(t, gr, "rotate-keys.json")
	assert.NotContains(t, readGenerated(t, fs, "/work/go.mod"), releaseManifestTool)
}

// The splice leaves an edited file's other lines alone, as the signs: block
// injection does.
func TestEnableSigning_EditedGitHubConfigKeepsItsEdits(t *testing.T) {
	t.Parallel()

	g, fs, _ := newUnsignedGitHubProject(t)
	before := readGenerated(t, fs, "/work/.goreleaser.yaml")
	edited := strings.Replace(before, "    - go generate ./...\n", "    - go generate ./...\n    - ./scripts/stage.sh # hand-edited\n", 1)
	require.NotEqual(t, before, edited)
	require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(edited), 0o644))

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.Contains(t, gr, "    - ./scripts/stage.sh # hand-edited\n"+rotationHookBlock, "the hook goes last in the list")
	assert.Contains(t, gr, rotationGlobBlock)
}

func TestEnableSigning_UnplaceableRotationWiringIsAdvised(t *testing.T) {
	t.Parallel()

	g, fs, buf := newUnsignedGitHubProject(t)
	own := strings.Replace(readGenerated(t, fs, "/work/.goreleaser.yaml"),
		"  mode: keep-existing\n", "  mode: keep-existing\n  extra_files:\n    - glob: ./NOTICE\n", 1)
	require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(own), 0o644))

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
	assert.NotContains(t, gr, releaseRotationVerifyHook, "half the wiring is worse than none")
	assert.Contains(t, gr, "    - glob: ./NOTICE\n")
	assert.Equal(t, releaseRotationVerifyHook, attrValue(t, buf, "add the rotation manifest's two lines", "before.hooks"))
}

func TestSpliceRotationWiring(t *testing.T) {
	t.Parallel()

	got, ok := spliceRotationWiring([]byte("project_name: x\n"))
	require.True(t, ok)
	assert.Equal(t, "project_name: x\n\nbefore:\n  hooks:\n"+rotationHookBlock+"\nrelease:\n"+rotationGlobBlock, string(got),
		"a file without either block gains both")

	back, ok := unspliceRotationWiring(got)
	require.True(t, ok)
	assert.NotContains(t, string(back), "releasemanifest")

	_, ok = spliceRotationWiring([]byte("before:\n  env: [A=1]\n"))
	assert.False(t, ok, "a before: block without hooks first")

	_, ok = unspliceRotationWiring([]byte("before:\n  hooks:\n    - " + releaseRotationVerifyHook + " --extra\n"))
	assert.False(t, ok, "an author's own variant of the hook is left for them")

	_, ok = unspliceRotationWiring([]byte("before:\n  hooks:\n    - " + releaseRotationVerifyHook + "\nrelease:\n" + rotationGlobBlock))
	assert.False(t, ok, "a hook typed without gtb's comment is the author's")
}

// Every shape the line splice cannot place the lines in is refused, so it is
// advised rather than written with a second key goreleaser rejects.
func TestSpliceRotationWiring_RefusesWhatItCannotPlace(t *testing.T) {
	t.Parallel()

	const tail = "builds:\n  - main: .\n"

	for name, body := range map[string]string{
		"a comment on the release line":        "before:\n  hooks:\n    - a\nrelease: # GitHub\n  mode: keep-existing\n",
		"a comment on the before line":         "before: # x\n  hooks:\n    - a\n",
		"a tab after before":                   "before:\t\n  hooks:\n    - a\n",
		"CRLF line endings":                    "before:\r\n  hooks:\r\n    - a\r\nrelease:\r\n  mode: keep-existing\r\n",
		"a flow-style before":                  "before: {hooks: [a, b]}\n",
		"an anchored release":                  "release: &r\n  mode: keep-existing\n",
		"an aliased release":                   "x: &m {mode: keep-existing}\nrelease: *m\n",
		"flow-style extra_files":               "release:\n  extra_files: [{glob: N}]\n",
		"no final newline":                     "release:\n  mode: keep-existing",
		"hooks followed by another child":      "before:\n  hooks:\n    - a\n  extra:\n    - b\n",
		"a duplicate key elsewhere":            "project_name: a\nproject_name: b\n",
		"CRLF with neither block":              "project_name: a\r\nbuilds:\r\n  - main: .\r\n",
		"map-shaped hooks":                     "before:\n  hooks:\n    - cmd: make\n",
		"a kept-newline scalar ending release": "release:\n  footer: |+\n    text\n\n",
		"a before with no value":               "before:\nrelease:\n  mode: keep-existing\n",
		"a quoted key":                         "\"release\":\n  mode: keep-existing\n",
		"a byte order mark":                    "\xef\xbb\xbfrelease:\n  mode: keep-existing\n",
		"a release in a later document":        "project_name: a\n---\nrelease:\n  mode: keep-existing\n",
		"a document ended by '... '":           "version: 2\nbuilds:\n  - main: .\n... \nfoo: bar\n",
		"a document opened by '---' and a tab": "version: 2\nbuilds:\n  - main: .\n---\t\nfoo: bar\n",
	} {
		_, ok := spliceRotationWiring([]byte(body))
		assert.False(t, ok, name)
	}

	_, ok := spliceRotationWiring([]byte("# yaml-language-server: x\n---\n" + tail))
	assert.True(t, ok, "a separator opening the only document is fine")

	got, ok := spliceRotationWiring([]byte(tail))
	require.True(t, ok)
	back, ok := unspliceRotationWiring(got)
	require.True(t, ok)
	assert.Equal(t, tail, string(back), "removing what the splice added whole leaves no empty before: or release:")
}

// Removing a block the splice appended whole must not take an author's lines
// after it, or join them onto the value above.
func TestUnspliceRotationWiring_KeepsLinesAddedAfterTheBlock(t *testing.T) {
	t.Parallel()

	for name, base := range map[string]string{
		"a scalar above": "project_name: mytool\n",
		"a bool above":   "release:\n  skip_tls_verify: false\n",
	} {
		spliced, ok := spliceRotationWiring([]byte(base))
		require.True(t, ok, name)

		edited := strings.Replace(string(spliced), rotationHookBlock, rotationHookBlock+"    - go mod tidy\n", 1)
		got, ok := unspliceRotationWiring([]byte(edited))
		require.True(t, ok, name)
		assert.NotContains(t, string(got), releaseRotationVerifyHook, name)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal(got, &doc), name)
		assert.Equal(t, []any{"go mod tidy"}, doc["before"].(map[string]any)["hooks"], name)
	}
}

func TestEnableSigning_HalfWiredConfigIsAdvised(t *testing.T) {
	t.Parallel()

	g, fs, buf := newUnsignedGitHubProject(t)
	half := strings.Replace(readGenerated(t, fs, "/work/.goreleaser.yaml"),
		"    - go generate ./...\n", "    - go generate ./...\n"+rotationHookBlock, 1)
	require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(half), 0o644))

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	assert.NotContains(t, readGenerated(t, fs, "/work/.goreleaser.yaml"), releaseRotationGlob)
	assert.Equal(t, releaseRotationGlob, attrValue(t, buf, "add the rotation manifest's two lines", "release.extra_files.glob"))
}

func TestAttachesRotationToRelease(t *testing.T) {
	t.Parallel()

	for provider, want := range map[string]bool{
		"github": true, "gitlab": false, "gitea": true, "codeberg": true, "bitbucket": false, "static": false, "": false,
	} {
		assert.Equal(t, want, attachesRotationToRelease(provider, releaseSigning), provider)
		assert.False(t, attachesRotationToRelease(provider, ManifestSigning{}), provider+" unsigned")
	}

	assert.True(t, usesReleaseManifestTool("static", ManifestSigning{}), "the static channel always builds its manifest")
	assert.True(t, usesReleaseManifestTool("gitea", releaseSigning), "the before hook runs it")
	assert.False(t, usesReleaseManifestTool("gitlab", releaseSigning))
}

func TestEnableSigning_IgnoredGitHubConfigIsAdvised(t *testing.T) {
	t.Parallel()

	g, fs, buf := newUnsignedGitHubProject(t)
	require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(".goreleaser.yaml\n"), 0o644))
	g.rules = nil // as a later gtb run reads it
	before := readGenerated(t, fs, "/work/.goreleaser.yaml")

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	assert.Equal(t, before, readGenerated(t, fs, "/work/.goreleaser.yaml"))
	assert.Equal(t, releaseRotationGlob, attrValue(t, buf, "add the rotation manifest's two lines", "release.extra_files.glob"))
}

// Disable removes only gtb's lines and keeps whatever the author placed beside
// them, whichever side of a blank or comment line it sits.
func TestUnspliceRotationWiring_KeepsTheAuthorsNeighbours(t *testing.T) {
	t.Parallel()

	spliced, ok := spliceRotationWiring([]byte("project_name: mytool\n"))
	require.True(t, ok)

	for name, tc := range map[string]struct {
		after, insert string
		section, list string
		want          []any
	}{
		"a hook after a blank line": {rotationHookBlock, "\n    - make docs\n", "before", "hooks", []any{"make docs"}},
		"a hook after a comment":    {rotationHookBlock, "# mine\n    - make docs\n", "before", "hooks", []any{"make docs"}},
		"another release file":      {rotationGlobBlock, "    - glob: NOTICE\n", "release", "extra_files", []any{map[string]any{"glob": "NOTICE"}}},
	} {
		edited := strings.Replace(string(spliced), tc.after, tc.after+tc.insert, 1)
		got, ok := unspliceRotationWiring([]byte(edited))
		require.True(t, ok, name)

		var doc map[string]any
		require.NoError(t, yaml.Unmarshal(got, &doc), name)
		assert.Equal(t, tc.want, doc[tc.section].(map[string]any)[tc.list], name)
		assert.NotContains(t, string(got), releaseRotationVerifyHook, name)
		assert.NotContains(t, string(got), releaseRotationGlob, name)
	}

	got, ok := unspliceRotationWiring([]byte(string(spliced) + "# trailing note\n"))
	require.True(t, ok)
	assert.Equal(t, "project_name: mytool\n# trailing note\n", string(got), "a comment after the block leaves no empty before: behind")
}

// A glob whose gtb comment the author edited is not gtb's to remove alone, so
// disable advises rather than leaving the hook gone and the glob in place.
func TestUnspliceRotationWiring_EditedGlobIsAdvised(t *testing.T) {
	t.Parallel()

	spliced, ok := spliceRotationWiring([]byte("project_name: mytool\n"))
	require.True(t, ok)

	edited := strings.Replace(string(spliced), "# Matches nothing until", "# Empty until", 1)
	_, ok = unspliceRotationWiring([]byte(edited))
	assert.False(t, ok)
}

// The equality check notices an edit that drops an author's hook or release
// file alongside gtb's own.
func TestCheckedRotationEdit_RefusesADroppedAuthorEntry(t *testing.T) {
	t.Parallel()

	original := "before:\n  hooks:\n    - make docs\n    - " + releaseRotationVerifyHook +
		"\nrelease:\n  extra_files:\n    - glob: NOTICE\n    - glob: " + releaseRotationGlob + "\n"

	for name, edit := range map[string]string{
		"the author's hook": "before:\n  hooks: []\nrelease:\n  extra_files:\n    - glob: NOTICE\n",
		"the author's file": "before:\n  hooks:\n    - make docs\nrelease:\n  extra_files: []\n",
	} {
		_, ok := checkedRotationEdit([]byte(original), edit, false)
		assert.False(t, ok, name)
	}

	_, ok := checkedRotationEdit([]byte(original), "before:\n  hooks:\n    - make docs\nrelease:\n  extra_files:\n    - glob: NOTICE\n", false)
	assert.True(t, ok, "dropping only gtb's two entries is accepted")
}

// Disable gives back the author's bytes exactly: the equality check cannot see
// a newline taken from the line before a block.
func TestUnspliceRotationWiring_KeepsTheAuthorsBytes(t *testing.T) {
	t.Parallel()

	hook := "before:\n  hooks:\n" + rotationHookBlock
	glob := "release:\n" + rotationGlobBlock

	for name, tc := range map[string]struct{ in, want string }{
		"no blank line before either block":     {"project_name: a\n# keep me\n" + hook + glob, "project_name: a\n# keep me\n"},
		"a blank line before release only":      {"project_name: a\n" + hook + "\n" + glob, "project_name: a\n"},
		"before on the first line":              {hook + "project_name: a\n" + glob, "project_name: a\n"},
		"a release key after extra_files":       {hook + glob + "  mode: keep-existing\n", "release:\n  mode: keep-existing\n"},
		"blank lines appendBlock wrote":         {"project_name: a\n\n" + hook + "\n" + glob, "project_name: a\n"},
		"an author blank before a partial span": {"before:\n  foo: 1\n\n  hooks:\n" + rotationHookBlock + glob, "before:\n  foo: 1\n\n"},
		"a comment ending in before:":           {"before:\n# hooks run before:\n  hooks:\n" + rotationHookBlock + glob, "before:\n# hooks run before:\n"},
	} {
		got, ok := unspliceRotationWiring([]byte(tc.in))
		require.True(t, ok, name)
		assert.Equal(t, tc.want, string(got), name)
	}
}

// A column-0 comment inside before.hooks does not end the block, so the hook
// goes after the author's last hook.
func TestSpliceRotationWiring_AColumnZeroCommentStaysInsideHooks(t *testing.T) {
	t.Parallel()

	got, ok := spliceRotationWiring([]byte("before:\n  hooks:\n    - a\n# note\n    - b\nrelease:\n  mode: keep-existing\n"))
	require.True(t, ok)
	assert.Contains(t, string(got), "    - b\n"+rotationHookBlock)
}

// Lines that only mention the hook and glob, in comments, are not wiring.
func TestEnableSigning_RotationTextInCommentsIsAdvised(t *testing.T) {
	t.Parallel()

	g, fs, buf := newUnsignedGitHubProject(t)
	body := readGenerated(t, fs, "/work/.goreleaser.yaml") +
		"# " + releaseRotationVerifyHook + "\n# " + releaseRotationGlob + "\n"
	require.NoError(t, afero.WriteFile(fs, "/work/.goreleaser.yaml", []byte(body), 0o644))

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	assert.False(t, rotationPlaced([]byte(readGenerated(t, fs, "/work/.goreleaser.yaml"))))
	assert.Equal(t, releaseRotationGlob, attrValue(t, buf, "add the rotation manifest's two lines", "release.extra_files.glob"))
}

// Both lines must sit where goreleaser reads them: one placed and the other
// only in a comment is not wired, so enable must not report it as done.
func TestRotationPlaced(t *testing.T) {
	t.Parallel()

	hookPlaced := "before:\n  hooks:\n    - " + releaseRotationVerifyHook + "\n"
	hookCommented := "before:\n  hooks:\n    - make\n    # - " + releaseRotationVerifyHook + "\n"
	globPlaced := "release:\n  extra_files:\n    - glob: " + releaseRotationGlob + "\n"
	globCommented := "release:\n  extra_files:\n    - glob: NOTICE\n    # - glob: " + releaseRotationGlob + "\n"

	assert.True(t, rotationPlaced([]byte(hookPlaced+globPlaced)))
	assert.False(t, rotationPlaced([]byte(hookPlaced+globCommented)), "the glob only in a comment")
	assert.False(t, rotationPlaced([]byte(hookCommented+globPlaced)), "the hook only in a comment")
	assert.False(t, rotationPlaced([]byte(hookPlaced+globPlaced+"---\nfoo: bar\n")), "a second document")
}

// A file already as the posture wants is neither written nor reported.
func TestEnableSigning_AlreadyWiredConfigIsLeftAlone(t *testing.T) {
	t.Parallel()

	g, fs, buf := newUnsignedGitHubProject(t)
	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	wired := readGenerated(t, fs, "/work/.goreleaser.yaml")
	before := len(buf.Messages())

	require.NoError(t, g.EnableSigning(context.Background(), releaseSigning))

	assert.Equal(t, wired, readGenerated(t, fs, "/work/.goreleaser.yaml"))

	for _, msg := range buf.Messages()[before:] {
		assert.NotContains(t, msg, "rotation manifest", "a wired file gets no update and no advice")
	}
}
