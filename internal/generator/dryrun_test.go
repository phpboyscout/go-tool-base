package generator

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/phpboyscout/go-tool-base/pkg/logger"
	"github.com/phpboyscout/go-tool-base/pkg/props"
	"github.com/phpboyscout/go-tool-base/pkg/version"
)

func TestDryRunResult_Print_CreatedFiles(t *testing.T) {
	t.Parallel()

	result := &DryRunResult{
		Created: []FilePreview{
			{Path: "cmd/hello/cmd.go", Content: []byte("package hello\n")},
			{Path: "cmd/hello/main.go", Content: []byte("package hello\n\nfunc Run() {}\n")},
		},
	}

	var buf bytes.Buffer
	result.Print(&buf)
	text := buf.String()

	assert.Contains(t, text, "Files to create:")
	assert.Contains(t, text, "+ cmd/hello/cmd.go")
	assert.Contains(t, text, "+ cmd/hello/main.go")
}

func TestDryRunResult_Print_ModifiedFiles(t *testing.T) {
	t.Parallel()

	result := &DryRunResult{
		Modified: []FilePreview{
			{Path: "cmd/root/cmd.go", Diff: "--- a/cmd/root/cmd.go\n+++ b/cmd/root/cmd.go\n@@ -1,3 +1,4 @@\n package root\n+import \"hello\"\n"},
		},
	}

	var buf bytes.Buffer
	result.Print(&buf)
	text := buf.String()

	assert.Contains(t, text, "Files to modify:")
	assert.Contains(t, text, "~ cmd/root/cmd.go")
	assert.Contains(t, text, "+import")
}

func TestDryRunResult_Print_EmptyResult(t *testing.T) {
	t.Parallel()

	result := &DryRunResult{}

	var buf bytes.Buffer
	result.Print(&buf)
	text := buf.String()

	assert.Contains(t, text, "No changes")
}

func TestDryRunResult_Print_Summary(t *testing.T) {
	t.Parallel()

	result := &DryRunResult{
		Created: []FilePreview{
			{Path: "a.go"},
			{Path: "b.go"},
		},
		Modified: []FilePreview{
			{Path: "c.go"},
		},
	}

	var buf bytes.Buffer
	result.Print(&buf)
	text := buf.String()

	assert.Contains(t, text, "2 file(s) to create")
	assert.Contains(t, text, "1 file(s) to modify")
}

func TestProduceDryRunResult_NewFiles(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	overlayFS := afero.NewMemMapFs()

	// Write a file only in the overlay (new file)
	require.NoError(t, afero.WriteFile(overlayFS, "/project/cmd/hello/cmd.go", []byte("package hello\n"), DefaultFileMode))

	result, err := produceDryRunResult(baseFS, overlayFS, "/project")
	require.NoError(t, err)

	assert.Len(t, result.Created, 1)
	assert.Equal(t, "cmd/hello/cmd.go", result.Created[0].Path)
	assert.Equal(t, []byte("package hello\n"), result.Created[0].Content)
	assert.Empty(t, result.Modified)
}

func TestProduceDryRunResult_ModifiedFiles(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	overlayFS := afero.NewMemMapFs()

	// File exists on both but with different content
	require.NoError(t, afero.WriteFile(baseFS, "/project/cmd/root/cmd.go", []byte("package root\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(overlayFS, "/project/cmd/root/cmd.go", []byte("package root\n\nimport \"hello\"\n"), DefaultFileMode))

	result, err := produceDryRunResult(baseFS, overlayFS, "/project")
	require.NoError(t, err)

	assert.Empty(t, result.Created)
	assert.Len(t, result.Modified, 1)
	assert.Equal(t, "cmd/root/cmd.go", result.Modified[0].Path)
	assert.NotEmpty(t, result.Modified[0].Diff)
	assert.Contains(t, result.Modified[0].Diff, "+import")
}

func TestProduceDryRunResult_UnchangedFiles(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	overlayFS := afero.NewMemMapFs()

	content := []byte("package root\n")
	require.NoError(t, afero.WriteFile(baseFS, "/project/cmd/root/cmd.go", content, DefaultFileMode))
	require.NoError(t, afero.WriteFile(overlayFS, "/project/cmd/root/cmd.go", content, DefaultFileMode))

	result, err := produceDryRunResult(baseFS, overlayFS, "/project")
	require.NoError(t, err)

	assert.Empty(t, result.Created)
	assert.Empty(t, result.Modified)
}

func TestProduceDryRunResult_MixedOperations(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	overlayFS := afero.NewMemMapFs()

	// Existing file modified
	require.NoError(t, afero.WriteFile(baseFS, "/project/existing.go", []byte("old"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(overlayFS, "/project/existing.go", []byte("new"), DefaultFileMode))

	// New file
	require.NoError(t, afero.WriteFile(overlayFS, "/project/new.go", []byte("brand new"), DefaultFileMode))

	// Unchanged file
	require.NoError(t, afero.WriteFile(baseFS, "/project/same.go", []byte("same"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(overlayFS, "/project/same.go", []byte("same"), DefaultFileMode))

	result, err := produceDryRunResult(baseFS, overlayFS, "/project")
	require.NoError(t, err)

	assert.Len(t, result.Created, 1)
	assert.Len(t, result.Modified, 1)
	assert.Equal(t, "new.go", result.Created[0].Path)
	assert.Equal(t, "existing.go", result.Modified[0].Path)
}

func TestCreateOverlayFS(t *testing.T) {
	t.Parallel()

	baseFS := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(baseFS, "/project/existing.go", []byte("original"), DefaultFileMode))

	overlay := createOverlayFS(baseFS)

	// Should be able to read through to base
	content, err := afero.ReadFile(overlay, "/project/existing.go")
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))

	// Write to overlay should not affect base
	require.NoError(t, afero.WriteFile(overlay, "/project/new.go", []byte("new content"), DefaultFileMode))

	// Base should be unchanged
	exists, err := afero.Exists(baseFS, "/project/new.go")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMaterialiseOverlay(t *testing.T) {
	t.Parallel()

	overlay := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(overlay, "/project/cmd/hello/cmd.go", []byte("package hello\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(overlay, "/project/go.mod", []byte("module example.com/test\n"), DefaultFileMode))

	tmpDir := t.TempDir()

	gen := New(&props.Props{Logger: logger.NewNoop()}, &Config{})
	err := gen.materialiseOverlay(overlay, "/project", tmpDir)
	require.NoError(t, err)

	// Verify files were written to the temp directory
	content, err := os.ReadFile(filepath.Join(tmpDir, "cmd/hello/cmd.go"))
	require.NoError(t, err)
	assert.Equal(t, "package hello\n", string(content))

	content, err = os.ReadFile(filepath.Join(tmpDir, "go.mod"))
	require.NoError(t, err)
	assert.Equal(t, "module example.com/test\n", string(content))
}

func TestProduceDryRunResult_TwoPathMode(t *testing.T) {
	t.Parallel()

	// Simulate materialised mode: base and overlay on same FS but different dirs
	fs := afero.NewMemMapFs()

	// Base project (original)
	require.NoError(t, afero.WriteFile(fs, "/base/existing.go", []byte("old content"), DefaultFileMode))

	// Temp dir (post-processed result) — has modified file + new file
	require.NoError(t, afero.WriteFile(fs, "/temp/existing.go", []byte("new content"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, "/temp/new.go", []byte("brand new"), DefaultFileMode))

	result, err := produceDryRunResult(fs, fs, "/base", "/temp")
	require.NoError(t, err)

	assert.Len(t, result.Created, 1)
	assert.Equal(t, "new.go", result.Created[0].Path)

	assert.Len(t, result.Modified, 1)
	assert.Equal(t, "existing.go", result.Modified[0].Path)
	assert.Contains(t, result.Modified[0].Diff, "-old content")
	assert.Contains(t, result.Modified[0].Diff, "+new content")
}

// setupDryRunProject creates a minimal project structure on the given FS
// with a manifest and go.mod so that GenerateDryRun can run.
func setupDryRunProject(t *testing.T, fs afero.Fs, root string) {
	t.Helper()

	require.NoError(t, fs.MkdirAll(root+"/.gtb", DefaultDirMode))
	require.NoError(t, afero.WriteFile(fs, root+"/.gtb/manifest.yaml", []byte("properties:\n  name: mytool\nversion:\n  gtb: v1.0.0\ncommands:\n  - name: root\n"), DefaultFileMode))
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/testproject\n\ngo 1.22\n"), DefaultFileMode))
}

func TestGenerateDryRun_NoWriteToBase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	setupDryRunProject(t, fs, root)

	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	cfg := &Config{
		Name:   "hello",
		Short:  "Say hello",
		Path:   root,
		Parent: "root",
		DryRun: true,
	}

	gen := New(p, cfg)
	result, err := gen.GenerateDryRun(context.Background())
	require.NoError(t, err)

	// Should have created files in the preview
	assert.NotEmpty(t, result.Created, "dry run should report files to create")

	// Verify base filesystem is unchanged — no new files written
	cmdDir := root + "/pkg/cmd/hello"
	exists, err := afero.DirExists(fs, cmdDir)
	require.NoError(t, err)
	assert.False(t, exists, "dry run must not create directories on the base filesystem")
}

func TestGenerateDryRun_ReportsNewFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	setupDryRunProject(t, fs, root)

	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	cfg := &Config{
		Name:   "greet",
		Short:  "Greet someone",
		Path:   root,
		Parent: "root",
		DryRun: true,
	}

	gen := New(p, cfg)
	result, err := gen.GenerateDryRun(context.Background())
	require.NoError(t, err)

	// Should report cmd.go and main.go as created
	var paths []string
	for _, f := range result.Created {
		paths = append(paths, f.Path)
	}

	assert.Contains(t, paths, "pkg/cmd/greet/cmd.go")
	assert.Contains(t, paths, "pkg/cmd/greet/main.go")
}

func TestGenerateSkeletonDryRun_NoWriteToBase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
	}

	gen := New(p, &Config{Path: root})
	gen.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	config := SkeletonConfig{
		Name:        "test-project",
		Repo:        "phpboyscout/test-project",
		Host:        "github.com",
		Description: "A test project",
		Path:        root,
		Features: []ManifestFeature{
			{Name: "init", Enabled: true},
			{Name: "docs", Enabled: true},
		},
	}

	result, err := gen.GenerateSkeletonDryRun(context.Background(), config)
	require.NoError(t, err)

	// Should report files to create
	assert.NotEmpty(t, result.Created, "dry run should report files to create")

	// Verify base filesystem is unchanged
	exists, err := afero.Exists(fs, root+"/cmd/test-project/main.go")
	require.NoError(t, err)
	assert.False(t, exists, "dry run must not create files on the base filesystem")

	exists, err = afero.Exists(fs, root+"/.gtb/manifest.yaml")
	require.NoError(t, err)
	assert.False(t, exists, "dry run must not create manifest on the base filesystem")
}

func TestGenerateSkeletonDryRun_ReportsExpectedFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
	}

	gen := New(p, &Config{Path: root})
	gen.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	config := SkeletonConfig{
		Name:        "myapp",
		Repo:        "org/myapp",
		Host:        "github.com",
		Description: "My app",
		Path:        root,
		Features: []ManifestFeature{
			{Name: "init", Enabled: true},
		},
	}

	result, err := gen.GenerateSkeletonDryRun(context.Background(), config)
	require.NoError(t, err)

	var paths []string
	for _, f := range result.Created {
		paths = append(paths, f.Path)
	}

	assert.Contains(t, paths, "cmd/myapp/main.go")
	assert.Contains(t, paths, "pkg/cmd/root/cmd.go")
	assert.Contains(t, paths, ".gtb/manifest.yaml")
}

func TestGenerateSkeletonDryRun_DispatchedFromConfig(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	p := &props.Props{
		FS:     fs,
		Logger: logger.NewNoop(),
	}

	gen := New(p, &Config{Path: root, DryRun: true})
	gen.runCommand = func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		return []byte("done"), nil
	}

	config := SkeletonConfig{
		Name:        "testcli",
		Repo:        "org/testcli",
		Host:        "github.com",
		Description: "A test CLI",
		Path:        root,
		Features: []ManifestFeature{
			{Name: "init", Enabled: true},
		},
	}

	// GenerateSkeleton with DryRun=true should behave like dry-run
	err := gen.GenerateSkeleton(context.Background(), config)
	require.NoError(t, err)

	// Base filesystem should be unchanged
	exists, err := afero.Exists(fs, root+"/cmd/testcli/main.go")
	require.NoError(t, err)
	assert.False(t, exists, "GenerateSkeleton with DryRun=true must not write files")
}

func TestRegenerateProjectDryRun_NoWriteToBase(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	// Set up a minimal existing project with manifest
	setupDryRunProject(t, fs, root)

	// Add go.mod with module path
	require.NoError(t, afero.WriteFile(fs, root+"/go.mod", []byte("module example.com/testproject\n\ngo 1.22\n"), DefaultFileMode))

	// Create root cmd.go so regeneration has something to work with
	require.NoError(t, fs.MkdirAll(root+"/pkg/cmd/root", DefaultDirMode))
	require.NoError(t, afero.WriteFile(fs, root+"/pkg/cmd/root/cmd.go", []byte("package root\n// existing\n"), DefaultFileMode))

	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	gen := New(p, &Config{Path: root, DryRun: true})

	result, err := gen.RegenerateProjectDryRun(context.Background())
	require.NoError(t, err)

	// Should report modifications
	assert.NotNil(t, result)

	// Verify base filesystem content is unchanged
	content, err := afero.ReadFile(fs, root+"/pkg/cmd/root/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, "package root\n// existing\n", string(content))
}

func TestGenerateDryRun_ReportsModifiedFiles(t *testing.T) {
	t.Parallel()

	fs := afero.NewMemMapFs()
	root := "/project"

	setupDryRunProject(t, fs, root)

	// Pre-create a cmd.go with different content
	cmdDir := root + "/pkg/cmd/existing"
	require.NoError(t, fs.MkdirAll(cmdDir, DefaultDirMode))
	require.NoError(t, afero.WriteFile(fs, cmdDir+"/cmd.go", []byte("package existing\n// old content\n"), DefaultFileMode))

	p := &props.Props{
		FS:      fs,
		Logger:  logger.NewNoop(),
		Version: version.NewInfo("v1.0.0", "", ""),
	}

	cfg := &Config{
		Name:   "existing",
		Short:  "An existing command",
		Path:   root,
		Parent: "root",
		DryRun: true,
		Force:  true, // Force so it overwrites
	}

	gen := New(p, cfg)
	result, err := gen.GenerateDryRun(context.Background())
	require.NoError(t, err)

	// cmd.go should appear as modified (different content)
	var modifiedPaths []string
	for _, f := range result.Modified {
		modifiedPaths = append(modifiedPaths, f.Path)
	}

	assert.Contains(t, modifiedPaths, "pkg/cmd/existing/cmd.go")

	// Verify the original file is unchanged on the base FS
	content, err := afero.ReadFile(fs, cmdDir+"/cmd.go")
	require.NoError(t, err)
	assert.Equal(t, "package existing\n// old content\n", string(content))
}
