package generator

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/setup/forge"
)

const (
	githubReleaseWorkflow = ".github/workflows/goreleaser.yaml"
	gitlabPipeline        = ".gitlab-ci.yml"
	signerRoleVariable    = "SIGNER_ROLE_ARN"
)

var kmsSigning = ManifestSigning{
	Enabled:   true,
	KeyID:     "alias/acme-release-signing-v1",
	Backend:   "aws-kms",
	KMSRegion: "us-east-1",
	PublicKey: "internal/trustkeys/keys/signing-key-v1.asc",
}

var localSigning = ManifestSigning{
	Enabled:   true,
	KeyID:     "./release.pem",
	Backend:   "local",
	PublicKey: "internal/trustkeys/keys/signing-key-v1.asc",
}

func signingCIConfig(backend props.FeatureID, path string, signing ManifestSigning) SkeletonConfig {
	return SkeletonConfig{
		Name: "acme", Repo: "acme/acme-cli", Host: string(backend) + ".com", ForgeBackend: backend,
		Description: "signing scaffold", Path: path,
		Features:       []ManifestFeature{{Name: string(backend), Enabled: true}},
		ReleaseChannel: ReleaseChannelForge,
		Signing:        signing,
	}
}

func generateSigningCI(t *testing.T, backend props.FeatureID, signing ManifestSigning) (*Generator, afero.Fs) {
	t.Helper()

	fs := afero.NewMemMapFs()
	g := newSkeletonGeneratorForTest(t, fs)
	require.NoError(t, g.GenerateSkeleton(context.Background(), signingCIConfig(backend, "/work", signing)))

	g.config.Path = "/work"

	return g, fs
}

func decodeYAMLMap(t *testing.T, content string) map[string]any {
	t.Helper()

	var doc map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(content), &doc), "generated CI must stay valid YAML:\n%s", content)

	return doc
}

// The ticket's first failure: a pull request build has no OIDC token, so a
// snapshot must not reach the signs block (#153).
func TestGitHubWorkflow_SnapshotSkipsSigningOnlyWhenSigned(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GithubFeature, kmsSigning)
	signed := readGenerated(t, fs, "/work/"+githubReleaseWorkflow)
	decodeYAMLMap(t, signed)
	assert.Contains(t, signed, `flags=--snapshot --skip=sign`)

	_, fs = generateSigningCI(t, forge.GithubFeature, ManifestSigning{})
	unsigned := readGenerated(t, fs, "/work/"+githubReleaseWorkflow)
	assert.Contains(t, unsigned, `flags=--snapshot"`)
	assert.NotContains(t, unsigned, "--skip=sign")
	assert.NotContains(t, unsigned, "SIGNER_ROLE_ARN")
	assert.NotContains(t, unsigned, "cli/cmd/gtb")
}

// The ticket's second failure: a tag release needs the signer role and gtb, and
// a non-tag run must get neither.
func TestGitHubWorkflow_TagReleaseGetsRoleAndGtb(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GithubFeature, kmsSigning)
	wf := readGenerated(t, fs, "/work/"+githubReleaseWorkflow)

	type step struct {
		Name string         `yaml:"name"`
		If   string         `yaml:"if"`
		Uses string         `yaml:"uses"`
		Run  string         `yaml:"run"`
		With map[string]any `yaml:"with"`
	}

	var doc struct {
		Jobs map[string]struct {
			Steps []step `yaml:"steps"`
		} `yaml:"jobs"`
	}
	require.NoError(t, yaml.Unmarshal([]byte(wf), &doc))

	byName := map[string]step{}
	position := map[string]int{}

	for i, s := range doc.Jobs["goreleaser"].Steps {
		byName[s.Name] = s
		position[s.Name] = i
	}

	const tagOnly = "${{ startsWith(github.ref, 'refs/tags/v') }}"

	role := byName["Assume the signer role"]
	assert.Equal(t, tagOnly, role.If)
	assert.Contains(t, role.Uses, "aws-actions/configure-aws-credentials@")
	assert.Equal(t, "${{ vars.SIGNER_ROLE_ARN }}", role.With["role-to-assume"])
	assert.Equal(t, "us-east-1", role.With["aws-region"])
	assert.Equal(t, "sts.amazonaws.com", role.With["audience"])

	install := byName["Install gtb"]
	assert.Equal(t, tagOnly, install.If)
	assert.Contains(t, install.Run, "go install "+gtbInstallPackage+"@")

	run := "Run GoReleaser ${{ env.flags }}"
	require.Contains(t, position, run)
	assert.Less(t, position["Assume the signer role"], position[run])
	assert.Less(t, position["Install gtb"], position[run])
}

// A local-key backend has no role to assume, but still calls gtb.
func TestGitHubWorkflow_LocalBackendInstallsGtbWithoutARole(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GithubFeature, localSigning)
	wf := readGenerated(t, fs, "/work/"+githubReleaseWorkflow)

	assert.Contains(t, wf, "--skip=sign")
	assert.Contains(t, wf, "go install "+gtbInstallPackage+"@")
	assert.NotContains(t, wf, "SIGNER_ROLE_ARN")
}

// GitLab's half of the ticket: the tag job needs the OIDC token and the role.
func TestGitLabPipeline_TagJobGetsOIDCAndGtb(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GitlabFeature, kmsSigning)
	doc := decodeYAMLMap(t, readGenerated(t, fs, "/work/"+gitlabPipeline))

	job, ok := doc["goreleaser"].(map[string]any)
	require.True(t, ok, "signing adds a goreleaser overlay")

	tokens, ok := job["id_tokens"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, map[string]any{"aud": "sts.amazonaws.com"}, tokens["AWS_WEB_IDENTITY_TOKEN"])

	vars, ok := job["variables"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "${SIGNER_ROLE_ARN}", vars["AWS_ROLE_ARN"])
	assert.Equal(t, "us-east-1", vars["AWS_REGION"])
	assert.NotEmpty(t, vars["AWS_WEB_IDENTITY_TOKEN_FILE"])

	before, ok := job["before_script"].([]any)
	require.True(t, ok)
	require.Len(t, before, 2)
	assert.Contains(t, before[0], "AWS_WEB_IDENTITY_TOKEN_FILE")
	assert.Contains(t, before[1], "go install "+gtbInstallPackage+"@")
}

func TestGitLabPipeline_UnsignedProjectHasNoOverlay(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GitlabFeature, ManifestSigning{})
	doc := decodeYAMLMap(t, readGenerated(t, fs, "/work/"+gitlabPipeline))

	assert.NotContains(t, doc, "goreleaser")
}

func TestGitLabPipeline_LocalBackendInstallsGtbWithoutOIDC(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GitlabFeature, localSigning)
	doc := decodeYAMLMap(t, readGenerated(t, fs, "/work/"+gitlabPipeline))

	job, ok := doc["goreleaser"].(map[string]any)
	require.True(t, ok)
	assert.NotContains(t, job, "id_tokens")
	assert.NotContains(t, job, "variables")

	before, ok := job["before_script"].([]any)
	require.True(t, ok)
	require.Len(t, before, 1)
	assert.Contains(t, before[0], "go install "+gtbInstallPackage+"@")
}

// The GitLab goreleaser component hands the job the release store's keys, and
// static keys outrank the web-identity token in the AWS SDK's chain. On GitHub
// the role step supplies exactly those variables, so blanking them there would
// remove the only credentials the signer has.
func TestGoreleaserSigns_BlanksStoreKeysOnlyOnGitLabKMS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend props.FeatureID
		signing ManifestSigning
		blanks  bool
	}{
		{"gitlab kms", forge.GitlabFeature, kmsSigning, true},
		{"gitlab local", forge.GitlabFeature, localSigning, false},
		{"github kms", forge.GithubFeature, kmsSigning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, fs := generateSigningCI(t, tt.backend, tt.signing)
			gr := readGenerated(t, fs, "/work/.goreleaser.yaml")
			decodeYAMLMap(t, gr)

			if tt.blanks {
				assert.Contains(t, gr, `- "AWS_ACCESS_KEY_ID="`)
				assert.Contains(t, gr, `- "AWS_SECRET_ACCESS_KEY="`)

				return
			}

			assert.NotContains(t, gr, "AWS_ACCESS_KEY_ID")
		})
	}
}

// The manifest records "0.50.3"; go install needs the tag, "v0.50.3".
func TestGTBInstallPackage_UsesTheReleaseTag(t *testing.T) {
	t.Parallel()

	const want = "gitlab.com/phpboyscout/go-tool-base/cli/cmd/gtb@v0.50.3"

	assert.Equal(t, want, skeletonTemplateData{GoToolBaseVersion: "0.50.3"}.GTBInstallPackage())
	assert.Equal(t, want, skeletonTemplateData{GoToolBaseVersion: "v0.50.3"}.GTBInstallPackage())
}

func TestIsReleasedVersion(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]bool{
		"0.50.3":                                true,
		"v0.50.3":                               true,
		"v0.50.4-0.20261106181352-7f881953587b": false,
		"v0.50.3+dirty":                         false,
		"dev":                                   false,
		"":                                      false,
	} {
		assert.Equal(t, want, isReleasedVersion(version), version)
	}
}

// `gtb enable signing` is how the block appears on an existing project, so the
// CI half has to arrive with it rather than waiting for a full regenerate.
func TestEnableSigning_RendersTheCIFile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		backend props.FeatureID
		relPath string
	}{
		{"github", forge.GithubFeature, githubReleaseWorkflow},
		{"gitlab", forge.GitlabFeature, gitlabPipeline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			g, fs := generateSigningCI(t, tt.backend, ManifestSigning{})
			require.NotContains(t, readGenerated(t, fs, "/work/"+tt.relPath), signerRoleVariable)

			require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

			rendered := readGenerated(t, fs, "/work/"+tt.relPath)
			assert.Contains(t, rendered, signerRoleVariable)

			m, err := g.loadManifest()
			require.NoError(t, err)
			assert.Equal(t, calculateHash([]byte(rendered)), m.Hashes[tt.relPath],
				"the new render is gtb's baseline, so a later regenerate sees no edit")

			require.NoError(t, g.DisableSigning(context.Background()))
			assert.NotContains(t, readGenerated(t, fs, "/work/"+tt.relPath), signerRoleVariable)
		})
	}
}

// Enabling signing with no key id wires verification only, so CI stays as it is.
func TestEnableSigning_WithoutKeyIDLeavesCIAlone(t *testing.T) {
	t.Parallel()

	g, fs := generateSigningCI(t, forge.GithubFeature, ManifestSigning{})
	before := readGenerated(t, fs, "/work/"+githubReleaseWorkflow)

	require.NoError(t, g.EnableSigning(context.Background(), ManifestSigning{ExternalKeyEmail: "release@acme.dev"}))

	assert.Equal(t, before, readGenerated(t, fs, "/work/"+githubReleaseWorkflow))
}

type capturedLogs interface {
	Contains(substr string) bool
	Entries() []logger.Entry
}

func loggedReason(logs capturedLogs, reason string) bool {
	return slices.ContainsFunc(logs.Entries(), func(entry logger.Entry) bool {
		return slices.Contains(entry.Keyvals, any(reason))
	})
}

// A generator with its own logger and its own ignore rules, because the one that
// scaffolded the project has already cached a rule set with no ignore file in it.
func signingCIGenerator(t *testing.T, fs afero.Fs, overwrite OverwriteMode) (*Generator, capturedLogs) {
	t.Helper()

	buf := logger.NewBuffer()
	g := New(&props.Props{FS: fs, Logger: buf, Config: emptyTestStore(t)}, &Config{Path: "/work", Overwrite: overwrite})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return nil, nil
	}

	return g, buf
}

func setTemplateSource(t *testing.T, g *Generator, suppliedPath string) {
	t.Helper()

	m, err := g.loadManifest()
	require.NoError(t, err)

	m.Properties.Templates = []TemplateSource{{
		Name: "acme", Type: "git", Location: "acme/templates", Ref: "v1",
		Hashes: map[string]string{suppliedPath: "hash"},
	}}
	require.NoError(t, g.writeManifest(m))
}

func TestEnableSigning_ProtectsACIFileGtbDoesNotOwn(t *testing.T) {
	t.Parallel()

	const edited = "# my own pipeline\n"

	tests := []struct {
		name   string
		setup  func(t *testing.T, fs afero.Fs, g *Generator)
		reason string
	}{
		{"listed in .gtb/ignore", func(t *testing.T, fs afero.Fs, _ *Generator) {
			t.Helper()
			require.NoError(t, afero.WriteFile(fs, "/work/.gtb/ignore", []byte(githubReleaseWorkflow+"\n"), 0o644))
		}, "it is listed in .gtb/ignore"},
		{"edited, overwrite refused", func(*testing.T, afero.Fs, *Generator) {}, "it has edits gtb won't overwrite"},
		{"supplied by a template source", func(t *testing.T, _ afero.Fs, g *Generator) {
			t.Helper()
			setTemplateSource(t, g, githubReleaseWorkflow)
		}, "a template source supplies it"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, fs := generateSigningCI(t, forge.GithubFeature, ManifestSigning{})
			require.NoError(t, afero.WriteFile(fs, "/work/"+githubReleaseWorkflow, []byte(edited), 0o644))

			g, buf := signingCIGenerator(t, fs, "deny")
			tt.setup(t, fs, g)

			require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

			assert.Equal(t, edited, readGenerated(t, fs, "/work/"+githubReleaseWorkflow))
			assert.True(t, buf.Contains("CI file not modified"), "the author is told what to bring across")
			assert.True(t, loggedReason(buf, tt.reason), "the advisory says why")
		})
	}
}

// A template source that doesn't supply the CI file has no claim on it.
func TestEnableSigning_RendersCIBesideAnUnrelatedTemplateSource(t *testing.T) {
	t.Parallel()

	_, fs := generateSigningCI(t, forge.GithubFeature, ManifestSigning{})
	g, _ := signingCIGenerator(t, fs, "deny")
	setTemplateSource(t, g, "docs/index.md")

	require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

	assert.Contains(t, readGenerated(t, fs, "/work/"+githubReleaseWorkflow), signerRoleVariable)
}

// A GitLab project that enabled signing before the store keys were blanked has
// gtb's block without them. Re-running enable has to bring it up to date.
func TestEnableSigning_RefreshesGtbsOwnSignsBlock(t *testing.T) {
	t.Parallel()

	const path = "/work/.goreleaser.yaml"

	withoutBlanks := func(t *testing.T, fs afero.Fs) string {
		t.Helper()

		var kept []string

		for line := range strings.Lines(readGenerated(t, fs, path)) {
			if !strings.Contains(line, "env:") && !strings.Contains(line, "AWS_") {
				kept = append(kept, line)
			}
		}

		return strings.Join(kept, "")
	}

	t.Run("unedited file is updated", func(t *testing.T) {
		t.Parallel()

		_, fs := generateSigningCI(t, forge.GitlabFeature, kmsSigning)
		g, _ := signingCIGenerator(t, fs, "deny")

		old := withoutBlanks(t, fs)
		require.NoError(t, afero.WriteFile(fs, path, []byte(old), 0o644))

		m, err := g.loadManifest()
		require.NoError(t, err)
		m.Hashes[goreleaserAssetRelPath] = calculateHash([]byte(old))
		require.NoError(t, g.writeManifest(m))

		require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

		updated := readGenerated(t, fs, path)
		decodeYAMLMap(t, updated)
		assert.Contains(t, updated, `- "AWS_ACCESS_KEY_ID="`)
		assert.Equal(t, 1, strings.Count(updated, "\nsigns:\n"))

		m, err = g.loadManifest()
		require.NoError(t, err)
		assert.Equal(t, calculateHash([]byte(updated)), m.Hashes[goreleaserAssetRelPath])
	})

	t.Run("edited file is left with an advisory", func(t *testing.T) {
		t.Parallel()

		_, fs := generateSigningCI(t, forge.GitlabFeature, kmsSigning)
		g, buf := signingCIGenerator(t, fs, "deny")

		old := withoutBlanks(t, fs)
		require.NoError(t, afero.WriteFile(fs, path, []byte(old), 0o644))

		require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

		assert.Equal(t, old, readGenerated(t, fs, path))
		assert.True(t, buf.Contains("Release config not modified"))
	})

	t.Run("a current block is left alone, without an advisory", func(t *testing.T) {
		t.Parallel()

		_, fs := generateSigningCI(t, forge.GitlabFeature, kmsSigning)
		g, buf := signingCIGenerator(t, fs, "deny")
		before := readGenerated(t, fs, path)

		require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

		assert.Equal(t, before, readGenerated(t, fs, path))
		assert.False(t, buf.Contains("Release config not modified"))
	})
}

func TestEnableSigning_LeavesADeletedCIFileDeleted(t *testing.T) {
	t.Parallel()

	g, fs := generateSigningCI(t, forge.GithubFeature, ManifestSigning{})
	require.NoError(t, fs.Remove("/work/"+githubReleaseWorkflow))

	require.NoError(t, g.EnableSigning(context.Background(), kmsSigning))

	exists, err := afero.Exists(fs, "/work/"+githubReleaseWorkflow)
	require.NoError(t, err)
	assert.False(t, exists)
}
