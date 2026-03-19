package generator

import (
	"context"
	"io"
	"testing"

	"github.com/charmbracelet/log"
	"github.com/phpboyscout/gtb/pkg/props"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestGenerateSkeleton(t *testing.T) {
	fs := afero.NewMemMapFs()
	logger := log.New(io.Discard)
	p := &props.Props{
		FS:     fs,
		Logger: logger,
	}

	g := New(p, &Config{})
	g.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	ctx := context.Background()
	config := SkeletonConfig{
		Name:        "test-project",
		Repo:        "phpboyscout/test-project",
		Host:        "github.com",
		Description: "A test project",
		Path:        "/work",
		Features: []ManifestFeature{
			{Name: "init", Enabled: true},
			{Name: "docs", Enabled: true},
		},
	}

	err := g.GenerateSkeleton(ctx, config)
	require.NoError(t, err)

	expectedFiles := []string{
		"/work/cmd/test-project/main.go",
		"/work/pkg/cmd/root/cmd.go",
		"/work/pkg/cmd/root/assets/init/config.yaml",
		"/work/go.mod",
		"/work/.gtb/manifest.yaml",
	}

	for _, f := range expectedFiles {
		exists, err := afero.Exists(fs, f)
		assert.NoError(t, err, "Error checking if %s exists", f)
		assert.True(t, exists, "File %s should exist", f)
	}

	// Verify manifest content
	manifestPath := "/work/.gtb/manifest.yaml"
	data, err := afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	err = yaml.Unmarshal(data, &m)
	require.NoError(t, err)

	assert.Equal(t, "test-project", m.Properties.Name)
	assert.Equal(t, "phpboyscout/test-project", m.Properties.Repo)
	assert.Equal(t, "github", m.ReleaseSource.Type)
	assert.Equal(t, "phpboyscout", m.ReleaseSource.Owner)
	assert.Equal(t, "test-project", m.ReleaseSource.Repo)

	featureNames := []string{}
	for _, f := range m.Properties.Features {
		if f.Enabled {
			featureNames = append(featureNames, f.Name)
		}
	}
	assert.Contains(t, featureNames, "init")
	assert.Contains(t, featureNames, "docs")

	// Verify generated root/cmd.go
	rootCmdPath := "/work/pkg/cmd/root/cmd.go"
	rootCmdContent, err := afero.ReadFile(fs, rootCmdPath)
	require.NoError(t, err)
	content := string(rootCmdContent)
	assert.Contains(t, content, "ReleaseSource: props.ReleaseSource{")
	assert.Contains(t, content, "Type:  \"github\"")
	assert.Contains(t, content, "Owner: \"phpboyscout\"")
	assert.Contains(t, content, "Repo:  \"test-project\"")
}

func TestGenerateSkeletonGitLabNestedPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	logger := log.New(io.Discard)
	p := &props.Props{
		FS:     fs,
		Logger: logger,
	}

	g := New(p, &Config{})
	g.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	config := SkeletonConfig{
		Name:        "my-tool",
		Repo:        "myorg/mygroup/my-tool",
		Host:        "gitlab.com",
		Description: "A tool in a nested GitLab group",
		Path:        "/work",
	}

	err := g.GenerateSkeleton(context.Background(), config)
	require.NoError(t, err)

	manifestPath := "/work/.gtb/manifest.yaml"
	data, err := afero.ReadFile(fs, manifestPath)
	require.NoError(t, err)

	var m Manifest
	require.NoError(t, yaml.Unmarshal(data, &m))

	assert.Equal(t, "myorg/mygroup/my-tool", m.Properties.Repo)
	assert.Equal(t, "gitlab", m.ReleaseSource.Type)
	// org is everything before the last slash
	assert.Equal(t, "myorg/mygroup", m.ReleaseSource.Owner)
	// repo name is the segment after the last slash
	assert.Equal(t, "my-tool", m.ReleaseSource.Repo)
}

func TestSplitRepoPath(t *testing.T) {
	tests := []struct {
		input       string
		wantOrg     string
		wantRepo    string
		wantErr     bool
	}{
		{"org/repo", "org", "repo", false},
		{"group/subgroup/repo", "group/subgroup", "repo", false},
		{"a/b/c/d", "a/b/c", "d", false},
		{"noslash", "", "", true},
		{"/noleadingorg", "", "", true},
		{"notrailingrepo/", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			org, repo, err := splitRepoPath(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantOrg, org)
				assert.Equal(t, tt.wantRepo, repo)
			}
		})
	}
}

func TestCalculateDisabledFeatures(t *testing.T) {
	tests := []struct {
		name     string
		features []ManifestFeature
		want     []string
	}{
		{
			name: "All enabled",
			features: []ManifestFeature{
				{Name: "init", Enabled: true},
				{Name: "update", Enabled: true},
				{Name: "mcp", Enabled: true},
				{Name: "docs", Enabled: true},
			},
			want: []string{},
		},
		{
			name: "Some disabled",
			features: []ManifestFeature{
				{Name: "init", Enabled: true},
				{Name: "update", Enabled: false},
				{Name: "mcp", Enabled: true},
				{Name: "docs", Enabled: false},
			},
			want: []string{"update", "docs"},
		},
		{
			name:     "None enabled",
			features: []ManifestFeature{},
			want:     []string{}, // Note: calculateDisabledFeatures now only returns what is EXPLICITLY disabled in the slice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateDisabledFeatures(tt.features)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}

func TestRunSkeletonCommand(t *testing.T) {
	g := &Generator{
		runCommand: func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
			assert.Equal(t, ".", dir)
			assert.Equal(t, "echo", name)
			return []byte("hello"), nil
		},
	}

	ctx := context.Background()
	err := g.runSkeletonCommand(ctx, ".", "echo", "hello")
	assert.NoError(t, err)
}
