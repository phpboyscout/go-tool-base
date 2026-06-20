package generator

// White-box unit tests for the deterministic, I/O-free pure helpers in the
// generator package: manifest decode/encode, merge resolution, the recursive
// command-tree walkers, template-source spec parsing/inference, the overlay
// containment/suppression logic, and the remaining validate.go branches. These
// complement the generator_build integration tests (which compile the emitted
// scaffold); file-emission paths are deliberately left to those.
//
// Every test is CI-deterministic: no network, keychain, TTY, or wall-clock,
// and runs over afero.NewMemMapFs() / in-memory inputs.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator/templates"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

// newPureGenerator returns a Generator over an empty MemMapFs with the given
// config. No skeleton is scaffolded — these tests exercise pure helpers
// directly, seeding only the manifest bytes they need.
func newPureGenerator(t *testing.T, cfg *Config) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	return New(p, cfg), fs
}

// ---------------------------------------------------------------------------
// manifest.go: decode/encode/marshal helpers + MultilineString
// ---------------------------------------------------------------------------

func TestDecodeManifestFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	t.Run("missing file", func(t *testing.T) {
		t.Parallel()

		_, err := DecodeManifestFile(fs, "/nope/manifest.yaml")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to read manifest")
	})

	t.Run("malformed yaml", func(t *testing.T) {
		t.Parallel()

		path := "/bad/manifest.yaml"
		require.NoError(t, afero.WriteFile(fs, path, []byte("\tname: : :["), 0o644))

		_, err := DecodeManifestFile(fs, path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal manifest")
	})

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		path := "/ok/manifest.yaml"
		require.NoError(t, afero.WriteFile(fs, path,
			[]byte("properties:\n  name: foo\ncommands:\n  - name: a\n"), 0o644))

		m, err := DecodeManifestFile(fs, path)
		require.NoError(t, err)
		assert.Equal(t, "foo", m.Properties.Name)
		require.Len(t, m.Commands, 1)
		assert.Equal(t, "a", m.Commands[0].Name)
	})
}

func TestEncodeAndMarshalManifestFile_RoundTrip(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	m := &Manifest{
		Properties: ManifestProperties{Name: "round-trip"},
		Commands:   []ManifestCommand{{Name: "child"}},
	}

	encPath := "/enc/manifest.yaml"
	require.NoError(t, EncodeManifestFile(fs, encPath, m))

	got, err := DecodeManifestFile(fs, encPath)
	require.NoError(t, err)
	assert.Equal(t, "round-trip", got.Properties.Name)

	marPath := "/mar/manifest.yaml"
	require.NoError(t, MarshalManifestFile(fs, marPath, m, os.FileMode(DefaultFileMode)))

	got2, err := DecodeManifestFile(fs, marPath)
	require.NoError(t, err)
	require.Len(t, got2.Commands, 1)
	assert.Equal(t, "child", got2.Commands[0].Name)
}

func TestEncodeManifestFile_CreateError(t *testing.T) {
	t.Parallel()

	// A read-only filesystem makes Create fail, exercising the error branch.
	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := EncodeManifestFile(ro, "/x/manifest.yaml", &Manifest{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open manifest for writing")
}

func TestMarshalManifestFile_WriteError(t *testing.T) {
	t.Parallel()

	ro := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := MarshalManifestFile(ro, "/x/manifest.yaml", &Manifest{}, os.FileMode(DefaultFileMode))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write manifest")
}

func TestManifestPathFor(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "proj/.gtb/manifest.yaml", ManifestPathFor("proj"))
}

func TestMultilineString_MarshalYAML(t *testing.T) {
	t.Parallel()

	t.Run("single line is plain scalar", func(t *testing.T) {
		t.Parallel()

		out, err := yaml.Marshal(map[string]MultilineString{"k": "one line"})
		require.NoError(t, err)
		assert.NotContains(t, string(out), "|")
		assert.Contains(t, string(out), "one line")
	})

	t.Run("multi line uses literal block", func(t *testing.T) {
		t.Parallel()

		out, err := yaml.Marshal(map[string]MultilineString{"k": "line1\nline2"})
		require.NoError(t, err)
		assert.Contains(t, string(out), "|")
	})
}

func TestManifestCommand_MarshalYAML_DeprecatedHashMigration(t *testing.T) {
	t.Parallel()

	cmd := ManifestCommand{Name: "c", Hash: "deadbeef", Warning: "renamed"}

	out, err := yaml.Marshal(cmd)
	require.NoError(t, err)

	s := string(out)
	// The deprecated single hash migrates into hashes[cmd.go] and clears `hash`.
	assert.Contains(t, s, "cmd.go: deadbeef")
	assert.NotRegexp(t, `(?m)^hash:`, s)
	// Warning becomes a comment.
	assert.Contains(t, s, "renamed")
}

func TestManifestFlag_MarshalYAML_Warning(t *testing.T) {
	t.Parallel()

	flag := ManifestFlag{Name: "f", Type: "string", Default: "x", Warning: "default unresolved"}

	out, err := yaml.Marshal(flag)
	require.NoError(t, err)
	assert.Contains(t, string(out), "default unresolved")
}

func TestManifest_GetReleaseSource(t *testing.T) {
	t.Parallel()

	m := &Manifest{ReleaseSource: ManifestReleaseSource{Type: "github", Owner: "o", Repo: "r"}}
	typ, owner, repo := m.GetReleaseSource()
	assert.Equal(t, "github", typ)
	assert.Equal(t, "o", owner)
	assert.Equal(t, "r", repo)
}

// ---------------------------------------------------------------------------
// hash.go: mergeHashes, calculateHash, promptOverwrite (non-interactive)
// ---------------------------------------------------------------------------

func TestMergeHashes(t *testing.T) {
	t.Parallel()

	base := map[string]string{"a": "1", "b": "2"}
	over := map[string]string{"b": "20", "c": "3"}

	got := mergeHashes(base, over)
	assert.Equal(t, map[string]string{"a": "1", "b": "20", "c": "3"}, got)

	// Inputs are not mutated.
	assert.Equal(t, "2", base["b"])
	assert.NotContains(t, base, "c")

	// Nil inputs are tolerated.
	assert.Empty(t, mergeHashes(nil, nil))
	assert.Equal(t, map[string]string{"x": "9"}, mergeHashes(nil, map[string]string{"x": "9"}))
}

func TestCalculateHash_Stable(t *testing.T) {
	t.Parallel()

	// Known SHA-256 of "abc".
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	assert.Equal(t, want, calculateHash([]byte("abc")))
	// Known SHA-256 of "xyz" — confirms a different input yields a different digest.
	const wantXYZ = "3608bca1e44ea6c4d268eb6db02260269892c0b42b86bbf1e77a6fa16c3c9282"
	assert.Equal(t, wantXYZ, calculateHash([]byte("xyz")))
}

func TestPromptOverwrite_NonInteractiveBranches(t *testing.T) {
	t.Parallel()

	t.Run("allow", func(t *testing.T) {
		t.Parallel()

		g, _ := newPureGenerator(t, &Config{Overwrite: "allow"})
		assert.True(t, g.promptOverwrite("f", nil, nil))
	})

	t.Run("deny", func(t *testing.T) {
		t.Parallel()

		g, _ := newPureGenerator(t, &Config{Overwrite: "deny"})
		assert.False(t, g.promptOverwrite("f", nil, nil))
	})
}

func TestPromptOverwrite_NonInteractiveEnv(t *testing.T) {
	// t.Setenv forbids t.Parallel; this test mutates a process-global env var.
	t.Setenv("GTB_NON_INTERACTIVE", "true")

	g, _ := newPureGenerator(t, &Config{Overwrite: "ask"})
	assert.False(t, g.promptOverwrite("f", nil, nil))
}

// ---------------------------------------------------------------------------
// features.go: pure resolution helpers
// ---------------------------------------------------------------------------

func TestFeatureDefaultEnabled(t *testing.T) {
	t.Parallel()

	on := []string{"init", "update", "mcp", "docs", "doctor", "changelog"}
	for _, name := range on {
		assert.True(t, featureDefaultEnabled(name), name)
	}

	off := []string{"ai", "config", "telemetry", "keychain", "unknown"}
	for _, name := range off {
		assert.False(t, featureDefaultEnabled(name), name)
	}
}

func TestFeatureEnabledIn(t *testing.T) {
	t.Parallel()

	feats := []ManifestFeature{
		{Name: "ai", Enabled: true},      // override on (default off)
		{Name: "update", Enabled: false}, // override off (default on)
	}

	assert.True(t, featureEnabledIn(feats, "ai"))
	assert.False(t, featureEnabledIn(feats, "update"))
	// Not overridden -> framework default.
	assert.True(t, featureEnabledIn(feats, "docs"))
	assert.False(t, featureEnabledIn(feats, "config"))
}

func TestUpsertOrClearFeature(t *testing.T) {
	t.Parallel()

	t.Run("add non-default entry", func(t *testing.T) {
		t.Parallel()

		out := upsertOrClearFeature(nil, "ai", true)
		require.Len(t, out, 1)
		assert.Equal(t, ManifestFeature{Name: "ai", Enabled: true}, out[0])
	})

	t.Run("update existing entry", func(t *testing.T) {
		t.Parallel()

		in := []ManifestFeature{{Name: "update", Enabled: false}, {Name: "ai", Enabled: true}}
		out := upsertOrClearFeature(in, "ai", false)
		// ai back to default (off) -> entry removed; update kept.
		require.Len(t, out, 1)
		assert.Equal(t, "update", out[0].Name)
	})

	t.Run("return to default removes entry", func(t *testing.T) {
		t.Parallel()

		in := []ManifestFeature{{Name: "update", Enabled: false}}
		out := upsertOrClearFeature(in, "update", true) // default for update is on
		assert.Empty(t, out)
	})
}

func TestApplyFeatureChanges(t *testing.T) {
	t.Parallel()

	t.Run("idempotent no-op", func(t *testing.T) {
		t.Parallel()

		// docs default on; requesting on changes nothing.
		feats, changed := applyFeatureChanges(nil, map[string]bool{"docs": true})
		assert.Empty(t, changed)
		assert.Empty(t, feats)
	})

	t.Run("multiple changes sorted", func(t *testing.T) {
		t.Parallel()

		_, changed := applyFeatureChanges(nil, map[string]bool{
			"ai":     true,  // off -> on : change
			"config": true,  // off -> on : change
			"docs":   false, // on -> off : change
		})
		assert.Equal(t, []string{"ai", "config", "docs"}, changed)
	})
}

func TestValidateFeatureNames(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateFeatureNames(map[string]bool{"ai": true, "docs": false}))

	err := validateFeatureNames(map[string]bool{"bogus": true})
	require.ErrorIs(t, err, ErrInvalidInput)
}

// ---------------------------------------------------------------------------
// features.go: instance helpers gated on a verified project / loaded manifest
// ---------------------------------------------------------------------------

func TestCurrentFeatures_And_FeatureEnabled(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/proj"})
	manifest := "properties:\n  name: tool\n  features:\n    - name: ai\n      enabled: true\nversion:\n  gtb: dev\n"
	require.NoError(t, afero.WriteFile(fs, ManifestPathFor("/proj"), []byte(manifest), 0o644))

	feats, err := g.CurrentFeatures()
	require.NoError(t, err)
	require.Len(t, feats, 1)
	assert.Equal(t, "ai", feats[0].Name)

	enabled, err := g.FeatureEnabled("ai")
	require.NoError(t, err)
	assert.True(t, enabled)

	// Not overridden -> default.
	docs, err := g.FeatureEnabled("docs")
	require.NoError(t, err)
	assert.True(t, docs)
}

func TestCurrentFeatures_NoProject(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/missing"})
	_, err := g.CurrentFeatures()
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// signing.go: ApplySigningDefaults branches + CurrentSigning + remove helper
// ---------------------------------------------------------------------------

func TestApplySigningDefaults_Branches(t *testing.T) {
	t.Parallel()

	t.Run("no key id leaves untouched", func(t *testing.T) {
		t.Parallel()

		in := ManifestSigning{Enabled: true}
		assert.Equal(t, in, ApplySigningDefaults(in))
	})

	t.Run("kms backend defaulted", func(t *testing.T) {
		t.Parallel()

		out := ApplySigningDefaults(ManifestSigning{KeyID: "alias/foo"})
		assert.Equal(t, defaultSigningBackend, out.Backend)
		assert.Equal(t, defaultSigningKMSRegion, out.KMSRegion)
		assert.Equal(t, defaultSigningPublicKey, out.PublicKey)
	})

	t.Run("local backend skips region default", func(t *testing.T) {
		t.Parallel()

		out := ApplySigningDefaults(ManifestSigning{KeyID: "./key.pem", Backend: "local"})
		assert.Equal(t, "local", out.Backend)
		assert.Empty(t, out.KMSRegion)
		assert.Equal(t, defaultSigningPublicKey, out.PublicKey)
	})

	t.Run("explicit values preserved", func(t *testing.T) {
		t.Parallel()

		in := ManifestSigning{KeyID: "k", Backend: "aws-kms", KMSRegion: "us-east-1", PublicKey: "keys/p.asc"}
		out := ApplySigningDefaults(in)
		assert.Equal(t, "us-east-1", out.KMSRegion)
		assert.Equal(t, "keys/p.asc", out.PublicKey)
	})
}

func TestCurrentSigning(t *testing.T) {
	t.Parallel()

	g, fs := newPureGenerator(t, &Config{Path: "/proj"})
	manifest := "properties:\n  name: tool\n  signing:\n    enabled: true\n    key_id: alias/foo\nversion:\n  gtb: dev\n"
	require.NoError(t, afero.WriteFile(fs, ManifestPathFor("/proj"), []byte(manifest), 0o644))

	s, err := g.CurrentSigning()
	require.NoError(t, err)
	assert.True(t, s.Enabled)
	assert.Equal(t, "alias/foo", s.KeyID)
}

func TestCurrentSigning_NoProject(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{Path: "/missing"})
	_, err := g.CurrentSigning()
	require.Error(t, err)
}

func TestRemoveGeneratedSigningGo(t *testing.T) {
	t.Parallel()

	t.Run("absent is no-op", func(t *testing.T) {
		t.Parallel()

		g, _ := newPureGenerator(t, &Config{Path: "/proj"})
		require.NoError(t, g.removeGeneratedSigningGo())
	})

	t.Run("present is removed", func(t *testing.T) {
		t.Parallel()

		g, fs := newPureGenerator(t, &Config{Path: "/proj"})
		full := "/proj/" + signingGoPath
		require.NoError(t, fs.MkdirAll("/proj/pkg/cmd/root", 0o755))
		require.NoError(t, afero.WriteFile(fs, full, []byte("package root"), 0o644))

		require.NoError(t, g.removeGeneratedSigningGo())

		exists, _ := afero.Exists(fs, full)
		assert.False(t, exists)
	})
}

// ---------------------------------------------------------------------------
// templatesource_spec.go: ParseTemplateSpec / splitSpecRef / inferSourceType /
// defaultSourceName
// ---------------------------------------------------------------------------

func TestSplitSpecRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw, wantLoc, wantRef string
	}{
		{"org/repo", "org/repo", ""},
		{"org/repo@v1.2.0", "org/repo", "v1.2.0"},
		{"org/repo@main", "org/repo", "main"},
		{"git@github.com:org/repo.git", "git@github.com:org/repo.git", ""}, // scp form, ref has slash
		{"ssh://git@host/org/repo", "ssh://git@host/org/repo", ""},         // candidate ref contains slash
		{"@leading", "@leading", ""},                                       // at == 0
		{"trailing@", "trailing@", ""},                                     // at == len-1
	}

	for _, tc := range cases {
		loc, ref := splitSpecRef(tc.raw)
		assert.Equal(t, tc.wantLoc, loc, tc.raw)
		assert.Equal(t, tc.wantRef, ref, tc.raw)
	}
}

func TestInferSourceType(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/some/dir", 0o755))

	assert.Equal(t, TemplateSourceLocal, inferSourceType(fs, "./rel"))
	assert.Equal(t, TemplateSourceLocal, inferSourceType(fs, "../up"))
	assert.Equal(t, TemplateSourceLocal, inferSourceType(fs, "/abs"))
	assert.Equal(t, TemplateSourceLocal, inferSourceType(fs, "~/home"))
	assert.Equal(t, TemplateSourceGit, inferSourceType(fs, "https://host/o/r"))
	assert.Equal(t, TemplateSourceGit, inferSourceType(fs, "git@host:o/r"))
	// Existing directory on fs -> local.
	assert.Equal(t, TemplateSourceLocal, inferSourceType(fs, "/some/dir"))
	// Bare path that is not a dir -> git.
	assert.Equal(t, TemplateSourceGit, inferSourceType(fs, "org/repo"))
	// nil fs falls straight through to git for a bare path.
	assert.Equal(t, TemplateSourceGit, inferSourceType(nil, "org/repo"))
}

func TestDefaultSourceName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		location, want string
	}{
		{"github.com/org/My-Repo.git", "my-repo"},
		{"org/repo", "repo"},
		{"/path/to/Tmpl_Set", "tmpl_set"},
		{"https://host/o/cool.repo", "cool-repo"},
		{"/", ""},       // trims to empty
		{".git", ""},    // becomes empty after suffix trim
		{"123repo", ""}, // must start with a letter per templateNameRe
	}

	for _, tc := range cases {
		assert.Equal(t, tc.want, defaultSourceName(tc.location), tc.location)
	}
}

func TestParseTemplateSpec_ErrorBranches(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()

	t.Run("empty rejected", func(t *testing.T) {
		t.Parallel()

		_, err := ParseTemplateSpec(fs, "  ", "")
		require.Error(t, err)
	})

	t.Run("inferred name from location", func(t *testing.T) {
		t.Parallel()

		ts, err := ParseTemplateSpec(fs, "org/repo@v1.0.0", "")
		require.NoError(t, err)
		assert.Equal(t, "repo", ts.Name)
	})

	t.Run("invalid spec rejected by validation", func(t *testing.T) {
		t.Parallel()

		// A single-segment bare git path fails validateBareGitPath.
		_, err := ParseTemplateSpec(fs, "single", "")
		require.Error(t, err)
	})
}

// ---------------------------------------------------------------------------
// templatesource_validate.go: ValidateTemplateSource + sub-validators
// ---------------------------------------------------------------------------

func TestValidateTemplateSource_AllBranches(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		ts      TemplateSource
		wantErr bool
	}{
		{"valid git org/repo", TemplateSource{Type: TemplateSourceGit, Location: "org/repo"}, false},
		{"valid git nested host path", TemplateSource{Type: TemplateSourceGit, Location: "gitlab.com/grp/sub/repo"}, false},
		{"valid git https url", TemplateSource{Type: TemplateSourceGit, Location: "https://host/o/r"}, false},
		{"valid git scp", TemplateSource{Type: TemplateSourceGit, Location: "git@host:o/r"}, false},
		{"valid local abs", TemplateSource{Type: TemplateSourceLocal, Location: "/abs/path"}, false},
		{"valid with name/ref/resolved", TemplateSource{Type: TemplateSourceGit, Location: "org/repo", Name: "abc", Ref: "feature/x", Resolved: "deadbeef"}, false},
		{"valid fingerprint", TemplateSource{Type: TemplateSourceLocal, Location: "/p", Fingerprint: strings.Repeat("a", 64)}, false},

		{"bad type", TemplateSource{Type: "svn", Location: "x"}, true},
		{"bad name", TemplateSource{Type: TemplateSourceGit, Location: "org/repo", Name: "Bad-Name"}, true},
		{"empty location", TemplateSource{Type: TemplateSourceGit, Location: ""}, true},
		{"dotdot traversal", TemplateSource{Type: TemplateSourceLocal, Location: "/a/../b"}, true},
		{"insecure http url", TemplateSource{Type: TemplateSourceGit, Location: "http://host/o/r"}, true},
		{"single-segment git path", TemplateSource{Type: TemplateSourceGit, Location: "solo"}, true},
		{"git path segment with bad char", TemplateSource{Type: TemplateSourceGit, Location: "org/re po"}, true},
		{"bad ref dotdot", TemplateSource{Type: TemplateSourceGit, Location: "org/repo", Ref: "a..b"}, true},
		{"bad resolved", TemplateSource{Type: TemplateSourceGit, Location: "org/repo", Resolved: "nothex!"}, true},
		{"bad fingerprint", TemplateSource{Type: TemplateSourceLocal, Location: "/p", Fingerprint: "short"}, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := ValidateTemplateSource(&tc.ts)
			if tc.wantErr {
				require.ErrorIs(t, err, ErrInvalidInput, "should wrap ErrInvalidInput")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestHasDotDotSegment(t *testing.T) {
	t.Parallel()

	assert.True(t, hasDotDotSegment("a/../b"))
	assert.True(t, hasDotDotSegment(`a\..\b`))
	assert.False(t, hasDotDotSegment("a/b/c"))
	assert.False(t, hasDotDotSegment("a..b")) // not a whole segment
}

func TestValidateManifestTemplates(t *testing.T) {
	t.Parallel()

	require.NoError(t, validateManifestTemplates([]TemplateSource{
		{Type: TemplateSourceGit, Location: "org/repo"},
	}))

	err := validateManifestTemplates([]TemplateSource{
		{Type: TemplateSourceGit, Location: "org/repo"},
		{Type: "svn", Location: "x"},
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// templatesource.go: overlay containment / suppression / descriptor logic
// ---------------------------------------------------------------------------

func TestIsProtectedOverlayPath(t *testing.T) {
	t.Parallel()

	protected := []string{".gtb/manifest.yaml", ".gtb", "internal/trustkeys/keys/x.asc", "go.mod", "go.sum"}
	for _, p := range protected {
		assert.True(t, isProtectedOverlayPath(p), p)
	}

	allowed := []string{"README.md", "pkg/cmd/root/cmd.go", "internal/other/x.go", "go.modx"}
	for _, p := range allowed {
		assert.False(t, isProtectedOverlayPath(p), p)
	}
}

func TestCleanRelativeOutputPath(t *testing.T) {
	t.Parallel()

	t.Run("valid", func(t *testing.T) {
		t.Parallel()

		got, err := cleanRelativeOutputPath("a/./b/c")
		require.NoError(t, err)
		assert.Equal(t, "a/b/c", got)
	})

	for _, bad := range []string{"", "/abs", "../escape", ".."} {
		bad := bad
		t.Run("reject "+bad, func(t *testing.T) {
			t.Parallel()

			_, err := cleanRelativeOutputPath(bad)
			require.Error(t, err)
		})
	}
}

func TestContainedOutputPath(t *testing.T) {
	t.Parallel()

	t.Run("contained", func(t *testing.T) {
		t.Parallel()

		got, err := containedOutputPath("/project", "pkg/cmd/foo.go")
		require.NoError(t, err)
		assert.Equal(t, "pkg/cmd/foo.go", got)
	})

	t.Run("escape rejected", func(t *testing.T) {
		t.Parallel()

		_, err := containedOutputPath("/project", "../../etc/passwd")
		require.Error(t, err)
	})
}

func TestValidateTemplateDescriptor(t *testing.T) {
	t.Parallel()

	t.Run("zero contract treated as v1", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, validateTemplateDescriptor(&TemplateDescriptor{}))
	})

	t.Run("known alias", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, validateTemplateDescriptor(&TemplateDescriptor{
			Contract: 1, Replaces: []string{"gitlab-ci", "github-ci"},
		}))
	})

	t.Run("unsupported contract", func(t *testing.T) {
		t.Parallel()

		err := validateTemplateDescriptor(&TemplateDescriptor{Contract: 99})
		require.Error(t, err)
	})

	t.Run("unknown alias", func(t *testing.T) {
		t.Parallel()

		err := validateTemplateDescriptor(&TemplateDescriptor{Replaces: []string{"bogus"}})
		require.Error(t, err)
	})
}

func TestParseTemplateDescriptor(t *testing.T) {
	t.Parallel()

	t.Run("missing is zero", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		desc, err := parseTemplateDescriptor(fs, "/src")
		require.NoError(t, err)
		assert.Equal(t, TemplateDescriptor{}, desc)
	})

	t.Run("present and valid", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src/gtb-template.yaml",
			[]byte("contract: 1\nreplaces:\n  - gitlab-ci\n"), 0o644))

		desc, err := parseTemplateDescriptor(fs, "/src")
		require.NoError(t, err)
		assert.Equal(t, []string{"gitlab-ci"}, desc.Replaces)
	})

	t.Run("malformed yaml", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src/gtb-template.yaml",
			[]byte("contract: : ["), 0o644))

		_, err := parseTemplateDescriptor(fs, "/src")
		require.Error(t, err)
	})

	t.Run("present but invalid alias", func(t *testing.T) {
		t.Parallel()

		fs := afero.NewMemMapFs()
		require.NoError(t, afero.WriteFile(fs, "/src/gtb-template.yaml",
			[]byte("replaces:\n  - bogus\n"), 0o644))

		_, err := parseTemplateDescriptor(fs, "/src")
		require.Error(t, err)
	})
}

func TestKnownAliases(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{"github-ci", "gitlab-ci"}, knownAliases())
}

func TestSuppressedPrefixesAndIsSuppressed(t *testing.T) {
	t.Parallel()

	prefixes := suppressedPrefixes(TemplateDescriptor{Replaces: []string{"gitlab-ci"}})
	assert.Contains(t, prefixes, ".gitlab-ci.yml")
	assert.Contains(t, prefixes, ".gitlab/")

	assert.True(t, isSuppressed(".gitlab-ci.yml", prefixes))   // file-exact
	assert.True(t, isSuppressed(".gitlab/ci/x.yml", prefixes)) // dir-prefix
	assert.False(t, isSuppressed("pkg/cmd/root.go", prefixes)) // unrelated
	assert.False(t, isSuppressed("renovate.json", prefixes))   // not the .json5 entry
}

func TestToOverlayContractData(t *testing.T) {
	t.Parallel()

	// Non-skeleton data yields a zero contract (no secret leakage).
	assert.Equal(t, TemplateContractData{}, toOverlayContractData("not skeleton"))
}

func TestTemplateContractData_GetReleaseProvider(t *testing.T) {
	t.Parallel()

	d := TemplateContractData{ReleaseProvider: "gitlab"}
	assert.Equal(t, "gitlab", d.GetReleaseProvider())
}

// ---------------------------------------------------------------------------
// manifest_query.go: pure recursive command tree walkers
// ---------------------------------------------------------------------------

func sampleTree() []ManifestCommand {
	return []ManifestCommand{
		{Name: "alpha", Commands: []ManifestCommand{
			{Name: "beta", Commands: []ManifestCommand{
				{Name: "gamma"},
			}},
		}},
		{Name: "delta"},
	}
}

func TestFindCommandPathRecursive(t *testing.T) {
	t.Parallel()

	path, found := findCommandPathRecursive(sampleTree(), "gamma")
	assert.True(t, found)
	assert.Equal(t, []string{"alpha", "beta", "gamma"}, path)

	path, found = findCommandPathRecursive(sampleTree(), "delta")
	assert.True(t, found)
	assert.Equal(t, []string{"delta"}, path)

	_, found = findCommandPathRecursive(sampleTree(), "nope")
	assert.False(t, found)
}

func TestFindCommandAt(t *testing.T) {
	t.Parallel()

	cmds := sampleTree()

	// Root-level.
	assert.NotNil(t, findCommandAt(cmds, nil, "delta"))
	assert.Nil(t, findCommandAt(cmds, nil, "beta"))

	// Nested under a parent path.
	got := findCommandAt(cmds, []string{"alpha", "beta"}, "gamma")
	require.NotNil(t, got)
	assert.Equal(t, "gamma", got.Name)

	// Parent path that does not exist.
	assert.Nil(t, findCommandAt(cmds, []string{"nope"}, "gamma"))
	// Name not present under existing parent.
	assert.Nil(t, findCommandAt(cmds, []string{"alpha"}, "missing"))
}

func TestFindCommandRecursive(t *testing.T) {
	t.Parallel()

	cmds := sampleTree()

	// Empty parent path -> nil (root handled elsewhere).
	assert.Nil(t, findCommandRecursive(cmds, nil, "alpha"))

	got := findCommandRecursive(cmds, []string{"alpha"}, "beta")
	require.NotNil(t, got)
	assert.Equal(t, "beta", got.Name)

	got = findCommandRecursive(cmds, []string{"alpha", "beta"}, "gamma")
	require.NotNil(t, got)
	assert.Equal(t, "gamma", got.Name)

	// Wrong parent first segment.
	assert.Nil(t, findCommandRecursive(cmds, []string{"zzz"}, "x"))
	// Right parent, missing child.
	assert.Nil(t, findCommandRecursive(cmds, []string{"alpha"}, "missing"))
}

func TestRemoveCommand(t *testing.T) {
	t.Parallel()

	t.Run("root level", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.True(t, removeCommand(&cmds, nil, "delta"))
		_, found := findCommandPathRecursive(cmds, "delta")
		assert.False(t, found)
	})

	t.Run("root level missing", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.False(t, removeCommand(&cmds, nil, "nope"))
	})

	t.Run("nested", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.True(t, removeCommand(&cmds, []string{"alpha", "beta"}, "gamma"))
		_, found := findCommandPathRecursive(cmds, "gamma")
		assert.False(t, found)
	})
}

func TestRemoveFromCommandRecursive_Misses(t *testing.T) {
	t.Parallel()

	cmds := sampleTree()
	assert.False(t, removeFromCommandRecursive(&cmds, nil, "x"))
	assert.False(t, removeFromCommandRecursive(&cmds, []string{"zzz"}, "x"))
	assert.False(t, removeFromCommandRecursive(&cmds, []string{"alpha"}, "missing"))
}

func TestUpdateProtectionRecursive(t *testing.T) {
	t.Parallel()

	t.Run("nested target", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.True(t, updateProtectionRecursive(&cmds, []string{"alpha", "beta", "gamma"}, true))
		got := findCommandAt(cmds, []string{"alpha", "beta"}, "gamma")
		require.NotNil(t, got)
		require.NotNil(t, got.Protected)
		assert.True(t, *got.Protected)
	})

	t.Run("misses", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.False(t, updateProtectionRecursive(&cmds, nil, true))
		assert.False(t, updateProtectionRecursive(&cmds, []string{"zzz"}, true))
	})
}

// ---------------------------------------------------------------------------
// manifest_update.go: pure recursive update walkers
// ---------------------------------------------------------------------------

func TestUpdateCommandRecursive_AppendAndUpdate(t *testing.T) {
	t.Parallel()

	cmds := sampleTree()

	// Append a new child under alpha/beta.
	added := updateCommandRecursive(&cmds, []string{"alpha", "beta"}, ManifestCommandUpdate{
		Name:        "newchild",
		Description: "desc",
	})
	assert.True(t, added)
	got := findCommandAt(cmds, []string{"alpha", "beta"}, "newchild")
	require.NotNil(t, got)
	assert.Equal(t, MultilineString("desc"), got.Description)

	// Update the same child (found path).
	updated := updateCommandRecursive(&cmds, []string{"alpha", "beta"}, ManifestCommandUpdate{
		Name:        "newchild",
		Description: "changed",
		Protected:   boolPtr(true),
		MCPEnabled:  boolPtr(false),
	})
	assert.True(t, updated)
	got = findCommandAt(cmds, []string{"alpha", "beta"}, "newchild")
	require.NotNil(t, got)
	assert.Equal(t, MultilineString("changed"), got.Description)
	require.NotNil(t, got.Protected)
	assert.True(t, *got.Protected)
}

func TestUpdateCommandRecursive_Misses(t *testing.T) {
	t.Parallel()

	cmds := sampleTree()
	assert.False(t, updateCommandRecursive(&cmds, nil, ManifestCommandUpdate{Name: "x"}))
	assert.False(t, updateCommandRecursive(&cmds, []string{"zzz"}, ManifestCommandUpdate{Name: "x"}))
}

func TestCreateNewManifestCommand(t *testing.T) {
	t.Parallel()

	u := ManifestCommandUpdate{
		Name:            "c",
		Description:     "d",
		LongDescription: "ld",
		Aliases:         []string{"x"},
		Args:            "args",
		Hashes:          map[string]string{"cmd.go": "h"},
		WithAssets:      true,
		Protected:       boolPtr(true),
		MCPEnabled:      boolPtr(true),
		Hidden:          true,
	}

	cmd := createNewManifestCommand(u)
	assert.Equal(t, "c", cmd.Name)
	assert.Equal(t, MultilineString("d"), cmd.Description)
	assert.True(t, cmd.WithAssets)
	assert.True(t, cmd.Hidden)
	require.NotNil(t, cmd.Protected)
	assert.True(t, *cmd.Protected)
}

func TestUpdateCommandHashRecursive(t *testing.T) {
	t.Parallel()

	t.Run("nested sets hash", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.True(t, updateCommandHashRecursive(&cmds, []string{"alpha", "beta"}, "abc123"))
		got := findCommandAt(cmds, []string{"alpha"}, "beta")
		require.NotNil(t, got)
		assert.Equal(t, "abc123", got.Hashes["cmd.go"])
	})

	t.Run("misses", func(t *testing.T) {
		t.Parallel()

		cmds := sampleTree()
		assert.False(t, updateCommandHashRecursive(&cmds, nil, "h"))
		assert.False(t, updateCommandHashRecursive(&cmds, []string{"zzz"}, "h"))
	})
}

// ---------------------------------------------------------------------------
// context.go: CommandContext construction / conversion
// ---------------------------------------------------------------------------

func TestBuildCommandContext_And_ToConfig(t *testing.T) {
	t.Parallel()

	cmd := ManifestCommand{
		Name:            "child",
		Description:     "short",
		LongDescription: "long",
		Aliases:         []string{"c"},
		Args:            "cobra.NoArgs",
		WithAssets:      true,
		Hidden:          true,
		Protected:       boolPtr(true),
	}

	ctx := buildCommandContext("/proj", true, false, true, cmd, []string{"parent"})
	assert.Equal(t, "child", ctx.Name)
	assert.Equal(t, []string{"parent"}, ctx.ParentPath)
	assert.Equal(t, "short", ctx.Short)
	assert.True(t, ctx.DryRun)
	assert.True(t, ctx.UpdateDocs)

	cfg := ctx.ToConfig()
	assert.Equal(t, "child", cfg.Name)
	assert.Equal(t, "parent", cfg.Parent)
	assert.Equal(t, "/proj", cfg.Path)
	assert.True(t, cfg.WithAssets)

	// Empty parent path -> "root".
	rootCtx := buildCommandContext("/proj", false, false, false, cmd, nil)
	assert.Equal(t, "root", rootCtx.ToConfig().Parent)
}

// ---------------------------------------------------------------------------
// ast.go: getParentPathParts
// ---------------------------------------------------------------------------

func TestGetParentPathParts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		parent string
		want   []string
	}{
		{"", []string{}},
		{"root", []string{}},
		{"/kube/ctx/", []string{"kube", "ctx"}},
		{"alpha", []string{"alpha"}},
	}

	for _, tc := range cases {
		g, _ := newPureGenerator(t, &Config{Parent: tc.parent})
		assert.Equal(t, tc.want, g.getParentPathParts(), tc.parent)
	}
}

// ---------------------------------------------------------------------------
// validate.go: remaining branches not covered by validate_test.go
// ---------------------------------------------------------------------------

func TestValidateSigningPublicKey_Branches(t *testing.T) {
	t.Parallel()

	valid := []string{"", "internal/trustkeys/keys/signing-key-v1.asc", "./key.asc", "keys/p.asc"}
	for _, v := range valid {
		require.NoError(t, ValidateSigningPublicKey(v), v)
	}

	invalid := []string{
		"/abs/key.asc",   // absolute
		`keys\key.asc`,   // backslash
		"../escape.asc",  // traversal
		"keys//double",   // doubled slash (unclean)
		"keys/trailing/", // trailing slash (unclean)
		"./../up.asc",    // beyond single ./
		"keys/.asc",      // segment fails char class
	}
	for _, v := range invalid {
		require.Error(t, ValidateSigningPublicKey(v), v)
	}
}

func TestValidateSigningKeyID_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSigningKeyID(""))
	require.NoError(t, ValidateSigningKeyID("arn:aws:kms:eu-west-2:123:key/abc"))
	require.NoError(t, ValidateSigningKeyID("alias/my-key"))
	require.NoError(t, ValidateSigningKeyID("./release.pem"))

	require.Error(t, ValidateSigningKeyID("has..dotdot"))
	require.Error(t, ValidateSigningKeyID("has space"))
	require.Error(t, ValidateSigningKeyID("has\"quote"))
}

func TestValidateOrg_GitLabBranches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateOrg("group/sub/repo-ns", "gitlab"))
	require.Error(t, ValidateOrg("", "gitlab"))
	require.Error(t, ValidateOrg("a/b/c/d/e", "gitlab"))              // depth > 4
	require.Error(t, ValidateOrg("bad seg/x", "gitlab"))              // bad char class
	require.Error(t, ValidateOrg("org", "bitbucket"))                 // unknown provider
	require.Error(t, ValidateOrg(strings.Repeat("a", 256), "gitlab")) // too long
}

func TestValidateHost_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateHost("github.com"))
	require.NoError(t, ValidateHost("git.example.com:8443"))
	require.NoError(t, ValidateHost("xn--bcher-kva.example")) // punycode

	require.Error(t, ValidateHost(""))
	require.Error(t, ValidateHost("host:"))           // empty port
	require.Error(t, ValidateHost("host:abc"))        // non-numeric port
	require.Error(t, ValidateHost("münchen.example")) // raw unicode
}

func TestValidateTeamsValue_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateTeamsChannel(""))
	require.NoError(t, ValidateTeamsChannel("General Channel"))

	require.Error(t, ValidateTeamsChannel(strings.Repeat("a", 101))) // too long
	require.Error(t, ValidateTeamsChannel("has {{ template"))        // brace
	require.Error(t, ValidateTeamsTeam("has\x00null"))               // control char
}

func TestValidateUpdateCheckInterval_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateUpdateCheckInterval(""))
	require.NoError(t, ValidateUpdateCheckInterval("24h"))

	require.Error(t, ValidateUpdateCheckInterval(strings.Repeat("9", 40)+"h")) // too long
	require.Error(t, ValidateUpdateCheckInterval("notaduration"))
	require.Error(t, ValidateUpdateCheckInterval("-5m")) // negative
}

func TestValidateManifest_NilAndValid(t *testing.T) {
	t.Parallel()

	require.Error(t, ValidateManifest(nil))

	m := &Manifest{
		Properties: ManifestProperties{
			Name:        "tool",
			Description: "a desc",
			EnvPrefix:   "TOOL",
			Help:        ManifestHelp{SlackChannel: "general"},
			Templates:   []TemplateSource{{Type: TemplateSourceGit, Location: "org/repo"}},
		},
		ReleaseSource: ManifestReleaseSource{Type: "github", Host: "github.com", Owner: "acme"},
		Commands:      []ManifestCommand{{Name: "sub"}},
	}
	require.NoError(t, ValidateManifest(m))
}

func TestValidateManifest_ReleaseSourceRejected(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Properties:    ManifestProperties{Name: "tool"},
		ReleaseSource: ManifestReleaseSource{Type: "github", Owner: "bad org!"},
	}
	require.Error(t, ValidateManifest(m))
}

// ---------------------------------------------------------------------------
// signing.go: EnableSigning / DisableSigning orchestration over MemMapFs.
//
// These assert the deterministic orchestration *decision* (manifest signing
// block + which signing-owned files are emitted/removed) over an in-memory FS.
// The OsFs-only post-processing (`go mod tidy`, gofmt) branch is skipped on
// MemMapFs, so no `go` toolchain, network, or wall-clock is touched — the
// emitted-content assertions stay in the gated generator_build integration
// tests. The spec (OQ3) permits using afero to verify orchestration decisions.
// ---------------------------------------------------------------------------

func newSigningProject(t *testing.T) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	scaffolder := New(p, &Config{})
	scaffolder.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	require.NoError(t, scaffolder.GenerateSkeleton(context.Background(), SkeletonConfig{
		Name:        "sign-tool",
		Repo:        "acme/sign-tool",
		Host:        "github.com",
		Description: "signing fixture",
		Path:        "/work",
	}))

	g := New(p, &Config{Path: "/work", Overwrite: "allow"})

	return g, fs
}

func TestEnableThenDisableSigning_MemFS(t *testing.T) {
	// Not parallel: GenerateSkeleton + signing orchestration are heavier and
	// share no globals, but kept serial for clarity over a scaffolded tree.
	g, fs := newSigningProject(t)

	signingGoFull := "/work/" + signingGoPath
	trustkeysFull := "/work/" + signingTrustKeysPath

	// Plain project: nothing signing-related yet.
	exists, _ := afero.Exists(fs, signingGoFull)
	require.False(t, exists)

	// Enable.
	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{
		ExternalKeyEmail: "release@example.test",
		KeySource:        "both",
	}))

	for _, p := range []string{signingGoFull, trustkeysFull, "/work/internal/trustkeys/keys/.gitkeep"} {
		ex, _ := afero.Exists(fs, p)
		assert.True(t, ex, "expected %s after enable", p)
	}

	m, err := DecodeManifestFile(fs, ManifestPathFor("/work"))
	require.NoError(t, err)
	assert.True(t, m.Properties.Signing.Enabled)
	assert.Equal(t, "release@example.test", m.Properties.Signing.ExternalKeyEmail)

	// Disable: signing.go removed; trustkeys + keys preserved (never deleted).
	require.NoError(t, g.DisableSigning(context.Background()))

	ex, _ := afero.Exists(fs, signingGoFull)
	assert.False(t, ex, "signing.go should be removed after disable")
	ex, _ = afero.Exists(fs, trustkeysFull)
	assert.True(t, ex, "trustkeys.go must be preserved after disable")

	m, err = DecodeManifestFile(fs, ManifestPathFor("/work"))
	require.NoError(t, err)
	assert.False(t, m.Properties.Signing.Enabled)
}

func TestEnableSigning_InvalidKeyIDRejected(t *testing.T) {
	g, _ := newSigningProject(t)

	// An invalid key id fails validateManifestSigning before any write.
	err := g.EnableSigning(context.Background(), ManifestSigning{KeyID: "bad id with spaces"})
	require.ErrorIs(t, err, ErrInvalidInput)
}

// ---------------------------------------------------------------------------
// ignore.go: middle `/**/ ` glob (matchSimpleGlob) + rule parsing edges
// ---------------------------------------------------------------------------

func TestIsIgnored_MiddleDoubleStarGlob(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/proj/.gtb", 0o755))
	// `docs/**/*.md` exercises the strings.Cut(pattern, "/**/") branch which
	// routes through matchSimpleGlob on the basename.
	require.NoError(t, afero.WriteFile(fs, "/proj/.gtb/ignore",
		[]byte("docs/**/*.md\n"), 0o644))

	r := LoadIgnoreRules(fs, "/proj")
	assert.True(t, r.IsIgnored("docs/sub/page.md"))
	assert.False(t, r.IsIgnored("docs/sub/page.txt"))
	assert.False(t, r.IsIgnored("other/page.md"))
}

func TestParseIgnoreRule_NegateAndDir(t *testing.T) {
	t.Parallel()

	neg := parseIgnoreRule("!keep.txt")
	assert.True(t, neg.negate)
	assert.Equal(t, "keep.txt", neg.pattern)

	dir := parseIgnoreRule("build/")
	assert.True(t, dir.dirOnly)
	assert.Equal(t, "build", dir.pattern)

	anchored := parseIgnoreRule(".github/workflows/ci.yml")
	assert.True(t, anchored.hasSlash)
}

// ---------------------------------------------------------------------------
// manifest.go: convertFlagsToManifest projection
// ---------------------------------------------------------------------------

func TestConvertFlagsToManifest(t *testing.T) {
	t.Parallel()

	g, _ := newPureGenerator(t, &Config{})
	out := g.convertFlagsToManifest([]templates.CommandFlag{
		{
			Name:        "verbose",
			Type:        "bool",
			Description: "enable verbose output",
			Shorthand:   "v",
			Persistent:  true,
		},
	})
	require.Len(t, out, 1)
	assert.Equal(t, "verbose", out[0].Name)
	assert.Equal(t, "bool", out[0].Type)
	assert.Equal(t, MultilineString("enable verbose output"), out[0].Description)
	assert.Equal(t, "v", out[0].Shorthand)
	assert.True(t, out[0].Persistent)
}

// ---------------------------------------------------------------------------
// validate.go: a few remaining optional-field branches
// ---------------------------------------------------------------------------

func TestValidateSlackTeam_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateSlackTeam(""))
	require.NoError(t, ValidateSlackTeam("Acme-Corp"))
	require.Error(t, ValidateSlackTeam("bad team name with spaces"))
}

func TestValidateTelemetryEndpoint_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateTelemetryEndpoint(""))
	require.NoError(t, ValidateTelemetryEndpoint("https://otel.example.com/v1/traces"))

	require.Error(t, ValidateTelemetryEndpoint(strings.Repeat("a", 2049))) // too long
	require.Error(t, ValidateTelemetryEndpoint("ftp://host/x"))            // bad scheme
	require.Error(t, ValidateTelemetryEndpoint("https://"))                // no host
	require.Error(t, ValidateTelemetryEndpoint("https://host/\x00"))       // control char
	require.Error(t, ValidateTelemetryEndpoint("http://%zz"))              // unparseable
}

func TestValidateCIComponentSource_Branches(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateCIComponentSource(""))
	require.NoError(t, ValidateCIComponentSource("gitlab.com/phpboyscout/cicd"))

	require.Error(t, ValidateCIComponentSource(strings.Repeat("a", 256))) // too long
	require.Error(t, ValidateCIComponentSource("has\x01control"))         // control char
	require.Error(t, ValidateCIComponentSource("has {{ brace"))           // template brace
	require.Error(t, ValidateCIComponentSource("https://scheme/x"))       // disallowed rune `:`
}
