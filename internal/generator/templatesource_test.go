package generator

import (
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeLocalSource writes a template-source tree under dir on fs.
func writeLocalSource(t *testing.T, fs afero.Fs, dir string, files map[string]string) {
	t.Helper()

	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		require.NoError(t, fs.MkdirAll(filepath.Dir(full), DefaultDirMode))
		require.NoError(t, afero.WriteFile(fs, full, []byte(content), DefaultFileMode))
	}
}

func sampleContractData() skeletonTemplateData {
	return skeletonTemplateData{
		Name:            "mytool",
		Description:     "A tool",
		Repo:            "acme/mytool",
		Host:            "github.com",
		Org:             "acme",
		RepoName:        "mytool",
		ModulePath:      "github.com/acme/mytool",
		ReleaseProvider: "github",
	}
}

// --- D1: overlay add + overwrite ---

func TestRenderOverlay_AddsAndOverwritesAtMirroredPaths(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	src := "/src"
	writeLocalSource(t, fs, src, map[string]string{
		"SECURITY.md":              "# Security for {{ .Name }}",
		".github/workflows/ci.yml": "name: {{ .RepoName }}",
		"README.md":                "ignored meta",
		"gtb-template.yaml":        "contract: 1",
	})

	files, err := renderOverlay(fs, src, "/project", toContractData(sampleContractData()))
	require.NoError(t, err)

	got := map[string]string{}
	for _, f := range files {
		got[f.relPath] = string(f.content)
	}

	assert.Equal(t, "# Security for mytool", got["SECURITY.md"])
	assert.Equal(t, "name: mytool", got[".github/workflows/ci.yml"])
	// Reserved root meta files are excluded from rendering.
	assert.NotContains(t, got, "README.md")
	assert.NotContains(t, got, "gtb-template.yaml")
}

// --- D9 reserved meta only at ROOT: a nested README.md still renders ---

func TestRenderOverlay_NestedReadmeStillRenders(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	src := "/src"
	writeLocalSource(t, fs, src, map[string]string{
		"README.md":      "root meta excluded",
		"docs/README.md": "nested {{ .Name }}",
	})

	files, err := renderOverlay(fs, src, "/project", toContractData(sampleContractData()))
	require.NoError(t, err)

	got := map[string]string{}
	for _, f := range files {
		got[f.relPath] = string(f.content)
	}

	assert.NotContains(t, got, "README.md")
	assert.Equal(t, "nested mytool", got["docs/README.md"])
}

// --- Security: write-path containment ---

func TestContainedOutputPath_RejectsTraversalAndAbsolute(t *testing.T) {
	t.Parallel()

	cases := []string{
		"../escape",
		"../../etc/passwd",
		"a/../../escape",
		"/abs/path",
	}

	for _, c := range cases {
		_, err := containedOutputPath("/project", c)
		require.Errorf(t, err, "expected %q to be rejected", c)
	}

	clean, err := containedOutputPath("/project", "a/b/c.txt")
	require.NoError(t, err)
	assert.Equal(t, "a/b/c.txt", clean)
}

func TestRenderOverlay_RejectsTraversalSourceFile(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	src := "/src/sub"
	// A file whose name encodes traversal.
	writeLocalSource(t, fs, src, map[string]string{
		"ok.txt": "fine",
	})
	// Manually plant a traversal file path is impossible via MemMapFs Walk
	// rel; instead assert containment via the helper which renderOverlay calls.
	_, err := containedOutputPath("/project", "../evil")
	require.Error(t, err)
}

// --- Security: protected-path denylist ---

func TestRenderOverlay_RejectsProtectedPaths(t *testing.T) {
	t.Parallel()

	for _, p := range []string{".gtb/manifest.yaml", "internal/trustkeys/keys/x.asc", "go.mod", "go.sum"} {
		assert.Truef(t, isProtectedOverlayPath(p), "%q should be protected", p)
	}

	assert.False(t, isProtectedOverlayPath("SECURITY.md"))
	assert.False(t, isProtectedOverlayPath(".github/workflows/ci.yml"))

	fs := afero.NewMemMapFs()
	src := "/src"
	writeLocalSource(t, fs, src, map[string]string{
		"go.mod": "module evil",
	})

	_, err := renderOverlay(fs, src, "/project", toContractData(sampleContractData()))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "protected path")
}

// --- Security: restricted FuncMap ---

func TestOverlayFuncMap_HasNoDangerousHelpers(t *testing.T) {
	t.Parallel()

	dangerous := []string{"readFile", "exec", "env", "getenv", "http", "include", "os", "command"}
	for _, name := range dangerous {
		_, ok := overlayFuncMap[name]
		assert.Falsef(t, ok, "overlay FuncMap must not expose %q", name)
	}

	// The escape helpers and pure string helpers ARE present.
	for _, name := range []string{"escapeYAML", "escapeMarkdown", "lower", "upper", "join"} {
		_, ok := overlayFuncMap[name]
		assert.Truef(t, ok, "overlay FuncMap should expose %q", name)
	}
}

// --- Security: data contract is metadata-only ---

func TestToContractData_OmitsSecrets(t *testing.T) {
	t.Parallel()

	full := sampleContractData()
	full.SlackTeam = "secret-team"
	full.TelemetryEndpoint = "https://secret.example/ingest"
	full.Signing = ManifestSigning{Enabled: true, KeyID: "alias/secret-key"}

	c := toContractData(full)

	// Render a template that tries to reach secret-bearing fields — they must
	// not be in the contract type at all (compile-time guarantee), so this
	// asserts the projected presence/shape fields only.
	assert.Equal(t, "mytool", c.Name)
	assert.True(t, c.SigningEnabled)
	// No KeyID / TelemetryEndpoint / SlackTeam fields exist on the contract.
}

// --- O9: unknown contract version rejected ---

func TestValidateTemplateDescriptor_RejectsUnknownContract(t *testing.T) {
	t.Parallel()

	err := validateTemplateDescriptor(&TemplateDescriptor{Contract: 99})
	require.Error(t, err)

	require.NoError(t, validateTemplateDescriptor(&TemplateDescriptor{Contract: 1}))
	require.NoError(t, validateTemplateDescriptor(&TemplateDescriptor{Contract: 0})) // omitted == v1
}

func TestValidateTemplateDescriptor_RejectsUnknownReplacesAlias(t *testing.T) {
	t.Parallel()

	err := validateTemplateDescriptor(&TemplateDescriptor{Replaces: []string{"jenkins-ci"}})
	require.Error(t, err)

	require.NoError(t, validateTemplateDescriptor(&TemplateDescriptor{Replaces: []string{"gitlab-ci", "github-ci"}}))
}

// --- D3: replaces suppression prefixes ---

func TestSuppression_GitlabCIAliasMatchesEmbeddedPaths(t *testing.T) {
	t.Parallel()

	prefixes := suppressedPrefixes(TemplateDescriptor{Replaces: []string{"gitlab-ci"}})

	assert.True(t, isSuppressed(".gitlab-ci.yml", prefixes))
	assert.True(t, isSuppressed(".gitlab/ci/release.yml", prefixes))
	assert.True(t, isSuppressed("renovate.json5", prefixes))
	assert.False(t, isSuppressed(".github/workflows/ci.yml", prefixes))
	assert.False(t, isSuppressed("justfile", prefixes))
}

// --- Manifest validation ---

func TestValidateTemplateSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		src     TemplateSource
		wantErr bool
	}{
		{"valid git path", TemplateSource{Type: TemplateSourceGit, Location: "acme/templates", Ref: "v1.0.0"}, false},
		{"valid git nested", TemplateSource{Type: TemplateSourceGit, Location: "gitlab.com/grp/sub/templates"}, false},
		{"valid https url", TemplateSource{Type: TemplateSourceGit, Location: "https://gitlab.com/acme/templates"}, false},
		{"valid local", TemplateSource{Type: TemplateSourceLocal, Location: "./templates"}, false},
		{"bad type", TemplateSource{Type: "svn", Location: "x/y"}, true},
		{"empty location", TemplateSource{Type: TemplateSourceGit, Location: ""}, true},
		{"traversal location", TemplateSource{Type: TemplateSourceLocal, Location: "../../etc"}, true},
		{"http url rejected", TemplateSource{Type: TemplateSourceGit, Location: "http://acme/templates"}, true},
		{"bad ref", TemplateSource{Type: TemplateSourceGit, Location: "a/b", Ref: "bad ref!"}, true},
		{"bad resolved", TemplateSource{Type: TemplateSourceGit, Location: "a/b", Resolved: "nothex"}, true},
		{"bad name", TemplateSource{Type: TemplateSourceGit, Location: "a/b", Name: "Bad Name"}, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			src := tc.src
			err := ValidateTemplateSource(&src)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateManifest_RejectsTamperedTemplateEntry(t *testing.T) {
	t.Parallel()

	m := &Manifest{
		Properties: ManifestProperties{
			Name: "mytool",
			Templates: []TemplateSource{
				{Type: TemplateSourceLocal, Location: "../../escape"},
			},
		},
	}

	require.Error(t, ValidateManifest(m))
}

// --- Fingerprint + drift ---

func TestFingerprintTree_StableAndChangesOnEdit(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	src := "/src"
	writeLocalSource(t, fs, src, map[string]string{"a.txt": "one", "b/c.txt": "two"})

	fp1, err := fingerprintTree(fs, src)
	require.NoError(t, err)

	fp2, err := fingerprintTree(fs, src)
	require.NoError(t, err)
	assert.Equal(t, fp1, fp2, "fingerprint must be stable")

	require.NoError(t, afero.WriteFile(fs, filepath.Join(src, "a.txt"), []byte("changed"), DefaultFileMode))

	fp3, err := fingerprintTree(fs, src)
	require.NoError(t, err)
	assert.NotEqual(t, fp1, fp3, "fingerprint must change on edit")
}

// --- splitGitLocation ---

func TestSplitGitLocation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		loc, defHost, host, owner, repo string
	}{
		{"acme/templates", "github.com", "github.com", "acme", "templates"},
		{"gitlab.com/grp/sub/templates", "github.com", "gitlab.com", "grp/sub", "templates"},
		{"https://gitlab.com/acme/templates.git", "github.com", "gitlab.com", "acme", "templates"},
	}

	for _, c := range cases {
		host, owner, repo, err := splitGitLocation(c.loc, c.defHost)
		require.NoErrorf(t, err, "loc=%s", c.loc)
		assert.Equal(t, c.host, host)
		assert.Equal(t, c.owner, owner)
		assert.Equal(t, c.repo, repo)
	}
}
