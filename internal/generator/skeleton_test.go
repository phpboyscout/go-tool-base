package generator

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"gitlab.com/phpboyscout/go-tool-base/pkg/logger"
	"gitlab.com/phpboyscout/go-tool-base/pkg/props"
)

func TestGenerateSkeleton(t *testing.T) {
	fs := afero.NewMemMapFs()
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
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
		require.NoError(t, err, "Error checking if %s exists", f)
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
	assert.Equal(t, "github", m.ReleaseSource.Type)
	assert.Equal(t, "github.com", m.ReleaseSource.Host)
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
	l := logger.NewNoop()
	p := &props.Props{
		FS:     fs,
		Logger: l,
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

	assert.Equal(t, "gitlab", m.ReleaseSource.Type)
	assert.Equal(t, "gitlab.com", m.ReleaseSource.Host)
	// org is everything before the last slash
	assert.Equal(t, "myorg/mygroup", m.ReleaseSource.Owner)
	// repo name is the segment after the last slash
	assert.Equal(t, "my-tool", m.ReleaseSource.Repo)
}

func TestSplitRepoPath(t *testing.T) {
	tests := []struct {
		input    string
		wantOrg  string
		wantRepo string
		wantErr  bool
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

// generateReadmeFixture scaffolds a project with the given features and
// returns the rendered README and justfile contents.
func generateReadmeFixture(t *testing.T, cfg SkeletonConfig) (readme, justfile string) {
	t.Helper()

	fs := afero.NewMemMapFs()
	p := &props.Props{FS: fs, Logger: logger.NewNoop()}

	g := New(p, &Config{})
	g.runCommand = func(_ context.Context, _, _ string, _ ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	require.NoError(t, g.GenerateSkeleton(context.Background(), cfg))

	readmeBytes, err := afero.ReadFile(fs, cfg.Path+"/README.md")
	require.NoError(t, err)

	justBytes, err := afero.ReadFile(fs, cfg.Path+"/justfile")
	require.NoError(t, err)

	return string(readmeBytes), string(justBytes)
}

// TestSkeletonReadme_Accuracy guards the richer default README against the
// accuracy constraint from the generated-README spec: no leftover template
// delimiters, the install path points at cmd/<name> (the real main package),
// per-project fields are substituted, and every `just <recipe>` it names is
// actually defined in the rendered justfile.
func TestSkeletonReadme_Accuracy(t *testing.T) {
	t.Parallel()

	readme, justfile := generateReadmeFixture(t, SkeletonConfig{
		Name:        "myapp",
		Repo:        "acme/myapp",
		Host:        "gitlab.com",
		Description: "A sample tool",
		Path:        "/work",
		EnvPrefix:   "MYAPP",
		Features: []ManifestFeature{
			{Name: "init", Enabled: true},
			{Name: "docs", Enabled: true},
		},
	})

	// No leftover template delimiters anywhere in the rendered README.
	assert.NotContains(t, readme, "{{", "README still contains unrendered template delimiters")
	assert.NotContains(t, readme, "}}", "README still contains unrendered template delimiters")

	// Install path uses the real main package location, cmd/<name>.
	assert.Contains(t, readme, "go install gitlab.com/acme/myapp/cmd/myapp@latest")
	assert.NotContains(t, readme, "go install gitlab.com/acme/myapp@latest",
		"README must not install the module root, which has no main package")

	// Per-project field substitution.
	assert.Contains(t, readme, "# myapp")
	assert.Contains(t, readme, "A sample tool")
	assert.Contains(t, readme, "MYAPP_LOG_LEVEL")
	assert.Contains(t, readme, "./bin/myapp --help")

	// Fenced code blocks are balanced.
	assert.Equal(t, 0, strings.Count(readme, "```")%2, "unbalanced ``` fences in README")

	// Every `just <recipe>` the README names must exist in the justfile.
	recipeRe := regexp.MustCompile(`just ([a-z][a-z-]*)`)
	defined := func(name string) bool {
		return regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + `:`).MatchString(justfile)
	}

	seen := map[string]bool{}
	for _, m := range recipeRe.FindAllStringSubmatch(readme, -1) {
		recipe := m[1]
		if seen[recipe] {
			continue
		}

		seen[recipe] = true

		assert.Truef(t, defined(recipe),
			"README references `just %s` but no such recipe is defined in the justfile", recipe)
	}

	require.NotEmpty(t, seen, "expected the README to reference at least one just recipe")
}

// TestSkeletonReadme_EnabledBuiltins exercises the inline feature->name map:
// with no opt-in features the README states none are enabled; with opt-in
// features enabled it lists their readable names.
func TestSkeletonReadme_EnabledBuiltins(t *testing.T) {
	t.Parallel()

	t.Run("no opt-in features", func(t *testing.T) {
		t.Parallel()

		readme, _ := generateReadmeFixture(t, SkeletonConfig{
			Name: "plain", Repo: "acme/plain", Host: "github.com",
			Description: "Plain tool", Path: "/work", EnvPrefix: "PLAIN",
			Features: []ManifestFeature{{Name: "docs", Enabled: true}},
		})

		assert.Contains(t, readme, "No opt-in built-ins")
		assert.NotContains(t, readme, "**AI chat**")
	})

	t.Run("opt-in features enabled", func(t *testing.T) {
		t.Parallel()

		readme, _ := generateReadmeFixture(t, SkeletonConfig{
			Name: "rich", Repo: "acme/rich", Host: "github.com",
			Description: "Rich tool", Path: "/work", EnvPrefix: "RICH",
			Features: []ManifestFeature{
				{Name: "ai", Enabled: true},
				{Name: "config", Enabled: true},
				{Name: "telemetry", Enabled: true},
			},
		})

		assert.Contains(t, readme, "**AI chat**")
		assert.Contains(t, readme, "**Config management**")
		assert.Contains(t, readme, "**Telemetry**")
	})
}
