package generate

import (
	"context"
	"strings"
	"testing"

	"github.com/cockroachdb/errors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"gitlab.com/phpboyscout/go-tool-base/internal/generator"
	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
	"gitlab.com/phpboyscout/go-tool-base/pkg/version"
)

func TestSkeletonRun_GitFlagsConflict(t *testing.T) {
	t.Parallel()

	p := &props.Props{FS: afero.NewMemMapFs(), Logger: logger.NewNoop()}

	opts := SkeletonOptions{
		Name:  "test-tool",
		Repo:  "phpboyscout/test-tool",
		Path:  "test-project",
		NoGit: true,
		Push:  true,
	}

	err := opts.Run(context.Background(), p)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrGitFlagsConflict))
}

func TestSkeletonRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{
				Type:  "github",
				Owner: "phpboyscout",
				Repo:  "gtb",
			},
		},
		Version: version.NewInfo("1.2.3", "", ""),
	}

	opts := SkeletonOptions{
		Name:        "test-tool",
		Repo:        "phpboyscout/test-tool",
		Description: "A description of the test tool",
		Path:        "test-project",
	}

	err := opts.Run(context.Background(), p)
	if err != nil {
		t.Logf("Run failed: %v", err)
	}
	require.NoError(t, err)

	expectedFiles := []string{
		"test-project/cmd/test-tool/main.go",
		"test-project/pkg/cmd/root/cmd.go",
		"test-project/pkg/cmd/root/assets/init/config.yaml",
		"test-project/go.mod",
		"test-project/README.md",
		"test-project/.gitignore",
		"test-project/.golangci.yaml",
		"test-project/zensical.toml",
		"test-project/justfile",
		"test-project/docs/index.md",
		"test-project/.github/CODEOWNERS",
		"test-project/.github/renovate.json5",
		"test-project/.github/workflows/docs.yaml",
		"test-project/.github/workflows/releaser-pleaser.yaml",
		"test-project/.github/workflows/test.yaml",
		"test-project/.github/workflows/goreleaser.yaml",
		"test-project/.goreleaser.yaml",
		"test-project/.gtb/manifest.yaml",
	}

	for _, f := range expectedFiles {
		exists, err := afero.Exists(fs, f)
		require.NoError(t, err)
		assert.True(t, exists, "file %s should exist", f)
	}

	// Verify go.mod content
	content, err := afero.ReadFile(fs, "test-project/go.mod")
	require.NoError(t, err)
	assert.Contains(t, string(content), "module github.com/phpboyscout/test-tool")

	// Verify .golangci.yaml content
	content, err = afero.ReadFile(fs, "test-project/.golangci.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "local-prefixes")
	assert.Contains(t, string(content), "github.com/phpboyscout/test-tool")

	// Verify config.yaml content
	content, err = afero.ReadFile(fs, "test-project/pkg/cmd/root/assets/init/config.yaml")
	require.NoError(t, err)
	assert.NotContains(t, string(content), "splunk")

	// Verify manifest content
	content, err = afero.ReadFile(fs, "test-project/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "name: test-tool")
	assert.Contains(t, string(content), "host: github.com")
	assert.Contains(t, string(content), "owner: phpboyscout")
	assert.Contains(t, string(content), "repo: test-tool")
	assert.Contains(t, string(content), "gtb: v1.2.3")

	// Verify .goreleaser.yaml uses github provider
	content, err = afero.ReadFile(fs, "test-project/.goreleaser.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "force_token: github")
	assert.Contains(t, string(content), "github_urls:")
	assert.NotContains(t, string(content), "gitlab_urls:")
}

func TestSkeletonRunGitLab(t *testing.T) {
	memFs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     memFs,
		Logger: logger.NewNoop(),
		Tool: props.Tool{
			ReleaseSource: props.ReleaseSource{
				Type:  "gitlab",
				Owner: "mygroup",
				Repo:  "my-tool",
			},
		},
		Version: version.NewInfo("1.2.3", "", ""),
	}

	opts := SkeletonOptions{
		Name:        "my-tool",
		Repo:        "mygroup/my-tool",
		Host:        "gitlab.com",
		Description: "A GitLab-hosted tool",
		Path:        "gitlab-project",
	}

	err := opts.Run(context.Background(), p)
	require.NoError(t, err)

	// GitLab CI files should be present
	gitlabFiles := []string{
		"gitlab-project/.gitlab-ci.yml",
		"gitlab-project/.gitlab/CODEOWNERS",
		"gitlab-project/renovate.json5",
	}

	for _, f := range gitlabFiles {
		exists, err := afero.Exists(memFs, f)
		require.NoError(t, err)
		assert.True(t, exists, "file %s should exist", f)
	}

	// The local-job-file CI model was replaced by the phpboyscout/cicd
	// component model (spec 2026-06-15-generator-gitlab-ci-refresh); the four
	// .gitlab/ci/*.yml job files must no longer be emitted.
	removedCIFiles := []string{
		"gitlab-project/.gitlab/ci/test.yml",
		"gitlab-project/.gitlab/ci/lint.yml",
		"gitlab-project/.gitlab/ci/release.yml",
		"gitlab-project/.gitlab/ci/pages.yml",
	}

	for _, f := range removedCIFiles {
		exists, err := afero.Exists(memFs, f)
		require.NoError(t, err)
		assert.False(t, exists, "local CI job file %s should no longer be scaffolded", f)
	}

	// GitHub CI files should NOT be present
	githubFiles := []string{
		"gitlab-project/.github/CODEOWNERS",
		"gitlab-project/.github/renovate.json5",
		"gitlab-project/.github/workflows/goreleaser.yaml",
	}

	for _, f := range githubFiles {
		exists, err := afero.Exists(memFs, f)
		require.NoError(t, err)
		assert.False(t, exists, "file %s should not exist for gitlab provider", f)
	}

	// .gitlab-ci.yml must mirror the component model: absolute cicd component
	// paths pinned to the lockstep version, the $CI_SERVER_FQDN-relative
	// releaser-pleaser component, and the templated repositories input. No
	// leftover local-job includes.
	ciContent, err := afero.ReadFile(memFs, "gitlab-project/.gitlab-ci.yml")
	require.NoError(t, err)
	ci := string(ciContent)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/go-lint@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/go-test@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/go-security@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/goreleaser@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/zensical-pages@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: gitlab.com/phpboyscout/cicd/renovate-self@"+generator.CICDComponentVersion)
	assert.Contains(t, ci, "component: $CI_SERVER_FQDN/apricote/releaser-pleaser/run@"+generator.ReleaserPleaserComponentVersion)
	assert.Contains(t, ci, `repositories: '["mygroup/my-tool"]'`)
	assert.Contains(t, ci, "enable_e2e: false")
	assert.NotContains(t, ci, "local: .gitlab/ci/")

	// renovate.json5 must extend the public cicd preset so the component-pin
	// auto-bump manager is active downstream.
	renovateContent, err := afero.ReadFile(memFs, "gitlab-project/renovate.json5")
	require.NoError(t, err)
	assert.Contains(t, string(renovateContent), "gitlab>phpboyscout/cicd")

	// .goreleaser.yaml should use gitlab provider
	content, err := afero.ReadFile(memFs, "gitlab-project/.goreleaser.yaml")
	require.NoError(t, err)
	assert.Contains(t, string(content), "force_token: gitlab")
	assert.Contains(t, string(content), "gitlab_urls:")
	assert.NotContains(t, string(content), "github_urls:")

	// CODEOWNERS should have the correct org
	content, err = afero.ReadFile(memFs, "gitlab-project/.gitlab/CODEOWNERS")
	require.NoError(t, err)
	assert.Contains(t, string(content), "@mygroup")
}

// TestSplitRepoOrgForValidate covers both repo shapes, including the
// two-segment (host-less) case that previously errored and silently
// skipped org validation (audit: org-validation-silently-skipped-two-segment-repo).
func TestSplitRepoOrgForValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repo    string
		wantOrg string
		wantErr bool
	}{
		{"github.com/myorg/mytool", "myorg", false},
		{"gitlab.com/group/sub/mytool", "group/sub", false},
		{"myorg/mytool", "myorg", false}, // two-segment, no host
		// Host-less GitLab nested group: the first segment is a group, not a
		// host (no dot), so the whole namespace is the org — bug #195.
		{"group/sub/mytool", "group/sub", false},
		{"single", "", true},
	}

	for _, tc := range cases {
		org, err := splitRepoOrgForValidate(tc.repo)
		if tc.wantErr {
			require.Errorf(t, err, "repo %q", tc.repo)

			continue
		}

		require.NoErrorf(t, err, "repo %q", tc.repo)
		assert.Equalf(t, tc.wantOrg, org, "repo %q", tc.repo)
	}
}

func TestNormalizeRepoHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		repo     string
		host     string
		wantRepo string
		wantHost string
	}{
		{"host-qualified github adopts host", "github.com/acme/tool", "", "acme/tool", "github.com"},
		{"host-qualified gitlab nested adopts host", "gitlab.com/group/sub/tool", "", "group/sub/tool", "gitlab.com"},
		{"self-hosted host stripped", "git.acme.example/mirror/tool", "", "mirror/tool", "git.acme.example"},
		{"bare org/repo unchanged", "acme/tool", "", "acme/tool", ""},
		{"host-less nested group preserved", "group/sub/tool", "", "group/sub/tool", ""},
		{"explicit host kept, repo host stripped", "github.com/acme/tool", "github.com", "acme/tool", "github.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			gotRepo, gotHost := normalizeRepoHost(tc.repo, tc.host)
			assert.Equal(t, tc.wantRepo, gotRepo, "repo")
			assert.Equal(t, tc.wantHost, gotHost, "host")
		})
	}
}

// TestValidateCoreFields_TwoSegmentRepoOrgValidated proves a malformed
// org in a two-segment repo is now rejected. "my_org" passes the repo
// segment rule (which permits `_`) but violates the GitHub org rule, so
// before the fix it slipped through unvalidated.
func TestValidateCoreFields_TwoSegmentRepoOrgValidated(t *testing.T) {
	t.Parallel()

	o := &SkeletonOptions{
		Name:        "mytool",
		Description: "a tool",
		Repo:        "my_org/mytool",
		GitBackend:  "github",
	}

	err := o.validateCoreFields()
	require.Error(t, err)
	require.ErrorIs(t, err, generator.ErrInvalidInput)
	assert.Contains(t, strings.Join(errors.GetAllHints(err), " "), "Org",
		"the two-segment repo's org must now reach ValidateOrg")

	// A well-formed two-segment org passes.
	o.Repo = "myorg/mytool"
	require.NoError(t, o.validateCoreFields())
}
