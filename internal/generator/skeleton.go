package generator

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/cockroachdb/errors"
	"github.com/dave/jennifer/jen"
	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"

	"github.com/phpboyscout/gtb/internal/generator/templates"
)

//go:embed assets/skeleton/*
var skeletonAssets embed.FS

//go:embed all:assets/skeleton-github
var skeletonGitHubAssets embed.FS

//go:embed all:assets/skeleton-gitlab
var skeletonGitLabAssets embed.FS

type SkeletonConfig struct {
	Name         string
	Repo         string
	Host         string
	Description  string
	Path         string
	GoVersion    string // overrides autodetected version when set
	Features     []ManifestFeature
	Private      bool   // true if the repository requires authentication to access
	HelpType     string // "slack", "teams", or ""
	SlackChannel string
	SlackTeam    string
	TeamsChannel string
	TeamsTeam    string
}

// splitRepoPath splits a repository path on the last '/', returning the org
// (everything before) and repo name (everything after). This supports both
// simple GitHub paths ("org/repo") and deeply-nested GitLab group paths
// ("group/subgroup/repo").
func splitRepoPath(repo string) (org, repoName string, err error) {
	i := strings.LastIndex(repo, "/")
	if i <= 0 {
		return "", "", errors.Newf("invalid repository format: path must contain '/' with a non-empty org (e.g. org/repo)")
	}

	if i == len(repo)-1 {
		return "", "", errors.Newf("invalid repository format: repository name must not be empty (e.g. org/repo)")
	}

	return repo[:i], repo[i+1:], nil
}

func releaseProviderForHost(host string) string {
	if strings.Contains(host, "gitlab") {
		return "gitlab"
	}

	return "github"
}

func (g *Generator) currentVersion() string {
	if g.props.Version != nil {
		return g.props.Version.GetVersion()
	}

	return ""
}

func resolveGoVersion(configured string) string {
	if configured != "" {
		return configured
	}

	return strings.TrimPrefix(runtime.Version(), "go")
}

func (g *Generator) runSkeletonPostProcessing(ctx context.Context, path string) {
	g.props.Logger.Info("Running go mod tidy...")

	if err := g.runSkeletonCommand(ctx, path, "go", "mod", "tidy"); err != nil {
		g.props.Logger.Warn("Failed to run go mod tidy", "error", err)
	}

	g.props.Logger.Info("Running golangci-lint run --fix...")

	if err := g.runSkeletonCommand(ctx, path, "golangci-lint", "run", "--fix"); err != nil {
		g.props.Logger.Warn("Failed to run golangci-lint", "error", err)
	}
}

func (g *Generator) GenerateSkeleton(ctx context.Context, config SkeletonConfig) error {
	g.props.Logger.Infof("Generating skeleton for %s in %s...", config.Name, config.Path)

	org, repoName, err := splitRepoPath(config.Repo)
	if err != nil {
		return err
	}

	if config.Host == "" {
		config.Host = "github.com"
	}

	if config.Description == "" {
		config.Description = fmt.Sprintf("%s utility", config.Name)
	}

	data := struct {
		Name              string
		Repo              string
		Host              string
		ModulePath        string
		Description       string
		Org               string
		RepoName          string
		ReleaseProvider   string
		GoToolBaseVersion string
		GoVersion         string
		DisabledFeatures  []string
		EnabledFeatures   []string
		Private           bool
		HelpType          string
		SlackChannel      string
		SlackTeam         string
		TeamsChannel      string
		TeamsTeam         string
	}{
		Name:              config.Name,
		Repo:              config.Repo,
		Host:              config.Host,
		ModulePath:        fmt.Sprintf("%s/%s", config.Host, config.Repo),
		Description:       config.Description,
		Org:               org,
		RepoName:          repoName,
		ReleaseProvider:   releaseProviderForHost(config.Host),
		GoToolBaseVersion: g.currentVersion(),
		GoVersion:         resolveGoVersion(config.GoVersion),
		DisabledFeatures:  calculateDisabledFeatures(config.Features),
		EnabledFeatures:   calculateEnabledFeatures(config.Features),
		Private:           config.Private,
		HelpType:          config.HelpType,
		SlackChannel:      config.SlackChannel,
		SlackTeam:         config.SlackTeam,
		TeamsChannel:      config.TeamsChannel,
		TeamsTeam:         config.TeamsTeam,
	}

	if err := g.generateSkeletonGoFiles(config.Path, data); err != nil {
		return err
	}

	if err := g.generateSkeletonTemplateFiles(config.Path, data); err != nil {
		return err
	}

	if err := g.writeSkeletonManifest(config); err != nil {
		return err
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, config.Path)
	}

	g.props.Logger.Infof("Successfully generated skeleton in %s", config.Path)

	return nil
}

func (g *Generator) generateSkeletonGoFiles(destPath string, data struct {
	Name              string
	Repo              string
	Host              string
	ModulePath        string
	Description       string
	Org               string
	RepoName          string
	ReleaseProvider   string
	GoToolBaseVersion string
	GoVersion         string
	DisabledFeatures  []string
	EnabledFeatures   []string
	Private           bool
	HelpType          string
	SlackChannel      string
	SlackTeam         string
	TeamsChannel      string
	TeamsTeam         string
}) error {
	goFiles := map[string]*jen.File{
		filepath.Join("cmd", data.Name, "main.go"): templates.SkeletonMain(data.ModulePath),
		"internal/version/version.go":              templates.SkeletonInternalVersion(data.ModulePath),
		"pkg/cmd/root/cmd.go": templates.SkeletonRoot(templates.SkeletonRootData{
			Name:             data.Name,
			Description:      data.Description,
			ReleaseProvider:  data.ReleaseProvider,
			Host:             data.Host,
			Org:              data.Org,
			RepoName:         data.RepoName,
			Private:          data.Private,
			DisabledFeatures: data.DisabledFeatures,
			EnabledFeatures:  data.EnabledFeatures,
			HelpType:         data.HelpType,
			SlackChannel:     data.SlackChannel,
			SlackTeam:        data.SlackTeam,
			TeamsChannel:     data.TeamsChannel,
			TeamsTeam:        data.TeamsTeam,
		}),
	}

	for path, f := range goFiles {
		fullPath := filepath.Join(destPath, path)
		if err := g.props.FS.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
			return errors.Newf("failed to create directory %s: %w", filepath.Dir(fullPath), err)
		}

		out, err := g.props.FS.Create(fullPath)
		if err != nil {
			return errors.Newf("failed to create file %s: %w", fullPath, err)
		}

		if err := f.Render(out); err != nil {
			_ = out.Close()

			return errors.Newf("failed to render jennifer file %s: %w", path, err)
		}

		if err := out.Close(); err != nil {
			return errors.Newf("failed to close file %s: %w", fullPath, err)
		}
	}

	return nil
}

func (g *Generator) walkCommonSkeletonAsset(destPath string, data any, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if d.IsDir() {
		return nil
	}

	relPath, relErr := filepath.Rel("assets/skeleton", path)
	if relErr != nil {
		return errors.Newf("failed to get relative path: %w", relErr)
	}

	content, readErr := skeletonAssets.ReadFile(path)
	if readErr != nil {
		return errors.Newf("failed to read embedded file %s: %w", path, readErr)
	}

	return g.renderSkeletonTemplate(filepath.Join(destPath, relPath), string(content), data)
}

func (g *Generator) walkProviderSkeletonAsset(destPath, providerRoot string, providerFS embed.FS, data any, path string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}

	if d.IsDir() {
		return nil
	}

	relPath, relErr := filepath.Rel(providerRoot, path)
	if relErr != nil {
		return errors.Newf("failed to get relative path: %w", relErr)
	}

	content, readErr := providerFS.ReadFile(path)
	if readErr != nil {
		return errors.Newf("failed to read embedded provider file %s: %w", path, readErr)
	}

	return g.renderSkeletonTemplate(filepath.Join(destPath, relPath), string(content), data)
}

func (g *Generator) generateSkeletonTemplateFiles(destPath string, data any) error {
	tmplFiles := map[string]string{
		"pkg/cmd/root/assets/init/config.yaml": templates.SkeletonConfig,
		"go.mod":                               templates.SkeletonGoMod,
	}

	for path, tmplStr := range tmplFiles {
		fullPath := filepath.Join(destPath, path)
		if err := g.renderSkeletonTemplate(fullPath, tmplStr, data); err != nil {
			return err
		}
	}

	// Extract the provider so we can filter CI files appropriately.
	releaseProvider := ""
	if m, ok := data.(struct {
		Name              string
		Repo              string
		Host              string
		ModulePath        string
		Description       string
		Org               string
		RepoName          string
		ReleaseProvider   string
		GoToolBaseVersion string
		GoVersion         string
		DisabledFeatures  []string
		EnabledFeatures   []string
		Private           bool
		HelpType          string
		SlackChannel      string
		SlackTeam         string
		TeamsChannel      string
		TeamsTeam         string
	}); ok {
		releaseProvider = m.ReleaseProvider
	}

	// Walk the common skeleton assets.
	if err := fs.WalkDir(skeletonAssets, "assets/skeleton", func(path string, d fs.DirEntry, err error) error {
		return g.walkCommonSkeletonAsset(destPath, data, path, d, err)
	}); err != nil {
		return err
	}

	// Walk the provider-specific CI assets.
	providerFS, providerRoot := skeletonGitHubAssets, "assets/skeleton-github"
	if releaseProvider == "gitlab" {
		providerFS, providerRoot = skeletonGitLabAssets, "assets/skeleton-gitlab"
	}

	return fs.WalkDir(providerFS, providerRoot, func(path string, d fs.DirEntry, err error) error {
		return g.walkProviderSkeletonAsset(destPath, providerRoot, providerFS, data, path, d, err)
	})
}

func (g *Generator) renderSkeletonTemplate(fullPath, tmplStr string, data any) error {
	if err := g.props.FS.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return errors.Newf("failed to create directory %s: %w", filepath.Dir(fullPath), err)
	}

	tmpl, err := template.New(fullPath).Parse(tmplStr)
	if err != nil {
		return errors.Newf("failed to parse template %s: %w", fullPath, err)
	}

	f, err := g.props.FS.Create(fullPath)
	if err != nil {
		return errors.Newf("failed to create file %s: %w", fullPath, err)
	}

	defer func() {
		_ = f.Close()
	}()

	if err := tmpl.Execute(f, data); err != nil {
		return errors.Newf("failed to execute template %s: %w", fullPath, err)
	}

	return nil
}

func (g *Generator) writeSkeletonManifest(config SkeletonConfig) error {
	org, repoName, err := splitRepoPath(config.Repo)
	if err != nil {
		return err
	}

	manifest := Manifest{
		Properties: ManifestProperties{
			Name:        config.Name,
			Description: MultilineString(config.Description),
			Features:    config.Features,
			Help: ManifestHelp{
				Type:         config.HelpType,
				SlackChannel: config.SlackChannel,
				SlackTeam:    config.SlackTeam,
				TeamsChannel: config.TeamsChannel,
				TeamsTeam:    config.TeamsTeam,
			},
		},
		ReleaseSource: ManifestReleaseSource{
			Type:    releaseProviderForHost(config.Host),
			Host:    config.Host,
			Owner:   org,
			Repo:    repoName,
			Private: config.Private,
		},
		Version: ManifestVersion{
			GoToolBase: func() string {
				if g.props.Version != nil {
					return g.props.Version.GetVersion()
				}

				return ""
			}(),
		},
	}

	manifestDir := filepath.Join(config.Path, ".gtb")
	if err := g.props.FS.MkdirAll(manifestDir, os.ModePerm); err != nil {
		return errors.Newf("failed to create manifest directory: %w", err)
	}

	manifestPath := filepath.Join(manifestDir, "manifest.yaml")

	f, err := g.props.FS.Create(manifestPath)
	if err != nil {
		return errors.Newf("failed to create manifest file: %w", err)
	}

	defer func() {
		_ = f.Close()
	}()

	enc := yaml.NewEncoder(f)

	const indent = 2
	enc.SetIndent(indent)

	if err := enc.Encode(manifest); err != nil {
		return errors.Newf("failed to encode manifest: %w", err)
	}

	return nil
}

func (g *Generator) runSkeletonCommand(ctx context.Context, dir, name string, args ...string) error {
	if g.runCommand != nil {
		_, err := g.runCommand(ctx, dir, name, args...)

		return err
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	return cmd.Run()
}

func calculateDisabledFeatures(features []ManifestFeature) []string {
	allFeatures := []string{"init", "update", "mcp", "docs"}
	disabled := []string{}

	featureMap := make(map[string]bool)

	for _, f := range features {
		if !f.Enabled {
			featureMap[f.Name] = true
		}
	}

	for _, f := range allFeatures {
		if featureMap[f] {
			disabled = append(disabled, f)
		}
	}

	return disabled
}

// calculateEnabledFeatures extracts opt-in features from the feature list.
// These features are off by default and must be explicitly enabled.
func calculateEnabledFeatures(features []ManifestFeature) []string {
	optInFeatures := []string{"ai"}
	enabled := []string{}

	featureMap := make(map[string]bool)

	for _, f := range features {
		if f.Enabled {
			featureMap[f.Name] = true
		}
	}

	for _, f := range optInFeatures {
		if featureMap[f] {
			enabled = append(enabled, f)
		}
	}

	return enabled
}
