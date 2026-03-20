package generator

import (
	"bytes"
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

	// Load existing project-level hashes so we can detect customised files.
	storedHashes := g.loadProjectFileHashes(config.Path)

	if err := g.generateSkeletonGoFiles(config.Path, data); err != nil {
		return err
	}

	writtenHashes, err := g.generateSkeletonTemplateFiles(config.Path, data, storedHashes)
	if err != nil {
		return err
	}

	// Merge: keep stored hashes for any files the user chose to skip so that
	// subsequent runs can still detect further modifications to those files.
	finalHashes := make(map[string]string, len(storedHashes)+len(writtenHashes))
	for k, v := range storedHashes {
		finalHashes[k] = v
	}
	for k, v := range writtenHashes {
		finalHashes[k] = v
	}

	if err := g.writeSkeletonManifest(config, finalHashes); err != nil {
		return err
	}

	if _, ok := g.props.FS.(*afero.OsFs); ok {
		g.runSkeletonPostProcessing(ctx, config.Path)

		// Post-processing tools (go mod tidy, golangci-lint) may have modified
		// tracked files. Refresh their hashes so the next run does not flag
		// post-processing changes as user customisations.
		if err := g.refreshProjectFileHashes(config.Path, writtenHashes); err != nil {
			g.props.Logger.Warn("Failed to refresh project file hashes after post-processing", "error", err)
		}
	}

	g.props.Logger.Infof("Successfully generated skeleton in %s", config.Path)

	return nil
}

// refreshProjectFileHashes re-reads the files in writtenKeys and updates
// their stored hashes in Manifest.Hashes to reflect any modifications made by
// post-processing tools (go mod tidy, golangci-lint, gofumpt).
//
// Only files present in writtenKeys are refreshed. Files where the user
// declined an overwrite are intentionally absent from writtenKeys and therefore
// retain their previously stored hash, ensuring the conflict is detected again
// on the next invocation.
func (g *Generator) refreshProjectFileHashes(projectPath string, writtenKeys map[string]string) error {
	if len(writtenKeys) == 0 {
		return nil
	}

	manifestPath := filepath.Join(projectPath, ".gtb", "manifest.yaml")

	raw, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return errors.Newf("failed to read manifest: %w", err)
	}

	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil {
		return errors.Newf("failed to unmarshal manifest: %w", err)
	}

	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}

	for relPath := range writtenKeys {
		content, readErr := afero.ReadFile(g.props.FS, filepath.Join(projectPath, relPath))
		if readErr != nil {
			// File removed by post-processing; drop it from tracking.
			delete(m.Hashes, relPath)

			continue
		}

		m.Hashes[relPath] = calculateHash(content)
	}

	f, err := g.props.FS.Create(manifestPath)
	if err != nil {
		return errors.Newf("failed to open manifest for writing: %w", err)
	}

	defer func() { _ = f.Close() }()

	enc := yaml.NewEncoder(f)

	const indent = 2
	enc.SetIndent(indent)

	if err := enc.Encode(m); err != nil {
		return errors.Newf("failed to write manifest: %w", err)
	}

	return nil
}

// loadProjectFileHashes reads the existing manifest at the given path and
// returns the stored project-level file hashes, or an empty map if no
// manifest exists yet.
func (g *Generator) loadProjectFileHashes(projectPath string) map[string]string {
	manifestPath := filepath.Join(projectPath, ".gtb", "manifest.yaml")

	raw, err := afero.ReadFile(g.props.FS, manifestPath)
	if err != nil {
		return make(map[string]string)
	}

	var m Manifest
	if err := yaml.Unmarshal(raw, &m); err != nil || m.Hashes == nil {
		return make(map[string]string)
	}

	return m.Hashes
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

func (g *Generator) generateSkeletonTemplateFiles(destPath string, data any, storedHashes map[string]string) (map[string]string, error) {
	collectedHashes := make(map[string]string)

	tmplFiles := map[string]string{
		"pkg/cmd/root/assets/init/config.yaml": templates.SkeletonConfig,
		"go.mod":                               templates.SkeletonGoMod,
	}

	for relPath, tmplStr := range tmplFiles {
		fullPath := filepath.Join(destPath, relPath)

		hash, err := g.renderAndHashSkeletonTemplate(fullPath, relPath, tmplStr, data, storedHashes)
		if err != nil {
			g.props.Logger.Warnf("Skipped %s: %v", relPath, err)
			continue
		}

		collectedHashes[relPath] = hash
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
		if err != nil || d.IsDir() {
			return err
		}

		relPath, err := filepath.Rel("assets/skeleton", path)
		if err != nil {
			return errors.Newf("failed to get relative path: %w", err)
		}

		content, err := skeletonAssets.ReadFile(path)
		if err != nil {
			return errors.Newf("failed to read embedded file %s: %w", path, err)
		}

		hash, err := g.renderAndHashSkeletonTemplate(filepath.Join(destPath, relPath), relPath, string(content), data, storedHashes)
		if err != nil {
			g.props.Logger.Warnf("Skipped %s: %v", relPath, err)
			return nil // non-fatal, continue walk
		}

		collectedHashes[relPath] = hash

		return nil
	}); err != nil {
		return nil, err
	}

	// Walk the provider-specific CI assets.
	providerFS, providerRoot := skeletonGitHubAssets, "assets/skeleton-github"
	if releaseProvider == "gitlab" {
		providerFS, providerRoot = skeletonGitLabAssets, "assets/skeleton-gitlab"
	}

	if err := fs.WalkDir(providerFS, providerRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		relPath, err := filepath.Rel(providerRoot, path)
		if err != nil {
			return errors.Newf("failed to get relative path: %w", err)
		}

		content, err := providerFS.ReadFile(path)
		if err != nil {
			return errors.Newf("failed to read embedded provider file %s: %w", path, err)
		}

		hash, err := g.renderAndHashSkeletonTemplate(filepath.Join(destPath, relPath), relPath, string(content), data, storedHashes)
		if err != nil {
			g.props.Logger.Warnf("Skipped %s: %v", relPath, err)
			return nil // non-fatal, continue walk
		}

		collectedHashes[relPath] = hash

		return nil
	}); err != nil {
		return nil, err
	}

	return collectedHashes, nil
}

// renderAndHashSkeletonTemplate renders a template to disk, checking the
// stored hash first so customised files are not silently overwritten.
// It returns the SHA256 hash of the content that was written.
func (g *Generator) renderAndHashSkeletonTemplate(fullPath, relPath, tmplStr string, data any, storedHashes map[string]string) (string, error) {
	if err := g.props.FS.MkdirAll(filepath.Dir(fullPath), os.ModePerm); err != nil {
		return "", errors.Newf("failed to create directory %s: %w", filepath.Dir(fullPath), err)
	}

	tmpl, err := template.New(fullPath).Parse(tmplStr)
	if err != nil {
		return "", errors.Newf("failed to parse template %s: %w", fullPath, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.Newf("failed to execute template %s: %w", fullPath, err)
	}

	newContent := buf.Bytes()

	// If the file already exists, verify the user has not customised it.
	if exists, _ := afero.Exists(g.props.FS, fullPath); exists {
		existingContent, err := afero.ReadFile(g.props.FS, fullPath)
		if err != nil {
			return "", errors.Newf("failed to read existing file %s: %w", fullPath, err)
		}

		currentHash := calculateHash(existingContent)
		storedHash := storedHashes[relPath]

		if storedHash != "" && storedHash != currentHash && !g.config.Force {
			g.props.Logger.Warnf("Conflict detected for %s: File has been manually modified.", fullPath)

			if !g.promptOverwrite(fullPath, existingContent, newContent) {
				g.props.Logger.Warnf("Skipping overwrite of %s", fullPath)

				return "", errors.Newf("overwrite skipped by user")
			}

			g.props.Logger.Warnf("Overwriting modified file %s", fullPath)
		}
	}

	if err := afero.WriteFile(g.props.FS, fullPath, newContent, os.ModePerm); err != nil {
		return "", errors.Newf("failed to write file %s: %w", fullPath, err)
	}

	return calculateHash(newContent), nil
}

func (g *Generator) writeSkeletonManifest(config SkeletonConfig, fileHashes map[string]string) error {
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
		Hashes: fileHashes,
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
