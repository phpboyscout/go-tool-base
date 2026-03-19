package generate

import (
	"context"
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/phpboyscout/gtb/pkg/version"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSkeletonRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     fs,
		Logger: log.New(io.Discard),
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
		"test-project/.github/workflows/semantic-release.yaml",
		"test-project/.github/workflows/test.yaml",
		"test-project/.github/workflows/goreleaser.yaml",
		"test-project/.goreleaser.yaml",
		"test-project/.gtb/manifest.yaml",
	}

	for _, f := range expectedFiles {
		exists, err := afero.Exists(fs, f)
		assert.NoError(t, err)
		assert.True(t, exists, "file %s should exist", f)
	}

	// Verify go.mod content
	content, err := afero.ReadFile(fs, "test-project/go.mod")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "module github.com/phpboyscout/test-tool")

	// Verify .golangci.yaml content
	content, err = afero.ReadFile(fs, "test-project/.golangci.yaml")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "local-prefixes")
	assert.Contains(t, string(content), "github.com/phpboyscout/test-tool")

	// Verify config.yaml content
	content, err = afero.ReadFile(fs, "test-project/pkg/cmd/root/assets/init/config.yaml")
	assert.NoError(t, err)
	assert.NotContains(t, string(content), "splunk")

	// Verify manifest content
	content, err = afero.ReadFile(fs, "test-project/.gtb/manifest.yaml")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "name: test-tool")
	assert.Contains(t, string(content), "host: github.com")
	assert.Contains(t, string(content), "owner: phpboyscout")
	assert.Contains(t, string(content), "repo: test-tool")
	assert.Contains(t, string(content), "gtb: v1.2.3")

	// Verify .goreleaser.yaml uses github provider
	content, err = afero.ReadFile(fs, "test-project/.goreleaser.yaml")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "force_token: github")
	assert.Contains(t, string(content), "github_urls:")
	assert.NotContains(t, string(content), "gitlab_urls:")
}

func TestSkeletonRunGitLab(t *testing.T) {
	memFs := afero.NewMemMapFs()
	p := &props.Props{
		FS:     memFs,
		Logger: log.New(io.Discard),
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
		"gitlab-project/.gitlab/ci/test.yml",
		"gitlab-project/.gitlab/ci/lint.yml",
		"gitlab-project/.gitlab/ci/release.yml",
		"gitlab-project/.gitlab/ci/pages.yml",
		"gitlab-project/.gitlab/CODEOWNERS",
		"gitlab-project/renovate.json5",
	}

	for _, f := range gitlabFiles {
		exists, err := afero.Exists(memFs, f)
		assert.NoError(t, err)
		assert.True(t, exists, "file %s should exist", f)
	}

	// GitHub CI files should NOT be present
	githubFiles := []string{
		"gitlab-project/.github/CODEOWNERS",
		"gitlab-project/.github/renovate.json5",
		"gitlab-project/.github/workflows/goreleaser.yaml",
	}

	for _, f := range githubFiles {
		exists, err := afero.Exists(memFs, f)
		assert.NoError(t, err)
		assert.False(t, exists, "file %s should not exist for gitlab provider", f)
	}

	// .goreleaser.yaml should use gitlab provider
	content, err := afero.ReadFile(memFs, "gitlab-project/.goreleaser.yaml")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "force_token: gitlab")
	assert.Contains(t, string(content), "gitlab_urls:")
	assert.NotContains(t, string(content), "github_urls:")

	// CODEOWNERS should have the correct org
	content, err = afero.ReadFile(memFs, "gitlab-project/.gitlab/CODEOWNERS")
	assert.NoError(t, err)
	assert.Contains(t, string(content), "@mygroup/ace")
}
