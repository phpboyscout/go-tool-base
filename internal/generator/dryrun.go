package generator

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cockroachdb/errors"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/afero"
)

// DryRunResult contains the preview of planned file operations.
type DryRunResult struct {
	Created  []FilePreview `json:"created,omitempty"`
	Modified []FilePreview `json:"modified,omitempty"`
}

// FilePreview represents a single file operation in a dry run.
type FilePreview struct {
	Path    string `json:"path"`
	Content []byte `json:"content,omitempty"`
	Diff    string `json:"diff,omitempty"`
}

// Print writes a human-readable preview of the dry-run result to w.
func (r *DryRunResult) Print(w io.Writer) {
	if len(r.Created) == 0 && len(r.Modified) == 0 {
		_, _ = fmt.Fprintln(w, "No changes would be made.")

		return
	}

	if len(r.Created) > 0 {
		_, _ = fmt.Fprintf(w, "Files to create: %d file(s) to create\n", len(r.Created))

		for _, f := range r.Created {
			_, _ = fmt.Fprintf(w, "  + %s\n", f.Path)
		}
	}

	if len(r.Modified) > 0 {
		if len(r.Created) > 0 {
			_, _ = fmt.Fprintln(w)
		}

		_, _ = fmt.Fprintf(w, "Files to modify: %d file(s) to modify\n", len(r.Modified))

		for _, f := range r.Modified {
			_, _ = fmt.Fprintf(w, "  ~ %s\n", f.Path)

			if f.Diff != "" {
				_, _ = fmt.Fprintln(w, f.Diff)
			}
		}
	}
}

// createOverlayFS returns an in-memory filesystem that captures writes while
// reads fall through to the base filesystem.
func createOverlayFS(base afero.Fs) afero.Fs {
	return afero.NewCopyOnWriteFs(base, afero.NewMemMapFs())
}

// dryRunPostProcess defines post-processing commands to run on materialised
// files. Each entry is a command with arguments (e.g. []string{"go", "mod", "tidy"}).
type dryRunPostProcess struct {
	commands [][]string
}

// withDryRunOverlay swaps g.props.FS for an overlay filesystem, runs fn to
// generate files, then materialises the result to a temp directory, runs
// post-processing commands, and diffs the result against the original project.
func (g *Generator) withDryRunOverlay(ctx context.Context, projectPath string, fn func() error, pp *dryRunPostProcess) (*DryRunResult, error) {
	baseFS := g.props.FS
	overlayFS := createOverlayFS(baseFS)
	g.props.FS = overlayFS

	defer func() { g.props.FS = baseFS }()

	if err := fn(); err != nil {
		return nil, err
	}

	// If running against a real OS filesystem and post-processing is requested,
	// materialise to a temp directory for accurate diffs.
	if _, ok := baseFS.(*afero.OsFs); ok && pp != nil && len(pp.commands) > 0 {
		return g.materialiseAndDiff(ctx, overlayFS, projectPath, pp)
	}

	// For in-memory filesystems (tests), skip materialisation.
	return produceDryRunResult(baseFS, overlayFS, projectPath)
}

// materialiseAndDiff copies the overlay content to a temp directory on disk,
// runs post-processing commands, then diffs the result against the original.
func (g *Generator) materialiseAndDiff(ctx context.Context, overlayFS afero.Fs, projectPath string, pp *dryRunPostProcess) (*DryRunResult, error) {
	tmpDir, err := os.MkdirTemp("", "gtb-dry-run-*")
	if err != nil {
		return nil, errors.Wrap(err, "failed to create temp directory for dry-run")
	}

	defer func() { _ = os.RemoveAll(tmpDir) }()

	g.props.Logger.Info("Materialising dry-run output for post-processing...")

	if err := g.materialiseOverlay(overlayFS, projectPath, tmpDir); err != nil {
		return nil, err
	}

	g.runDryRunPostProcessing(ctx, tmpDir, pp)

	g.props.Logger.Debug("Comparing post-processed output against original project...")

	// Diff the post-processed temp dir against the original project path.
	osFS := afero.NewOsFs()

	result, err := produceDryRunResult(osFS, osFS, projectPath, tmpDir)
	if err != nil {
		return nil, err
	}

	g.props.Logger.Debugf("Dry run result: %d file(s) to create, %d file(s) to modify", len(result.Created), len(result.Modified))

	return result, nil
}

// materialiseOverlay copies all files from the overlay filesystem under
// projectPath into the target directory on the real OS filesystem.
func (g *Generator) materialiseOverlay(overlay afero.Fs, projectPath, targetDir string) error {
	return afero.Walk(overlay, projectPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		relPath, err := filepath.Rel(projectPath, path)
		if err != nil {
			return errors.Wrap(err, "failed to compute relative path")
		}

		destPath := filepath.Join(targetDir, relPath)

		if info.IsDir() {
			return os.MkdirAll(destPath, DefaultDirMode)
		}

		content, err := afero.ReadFile(overlay, path)
		if err != nil {
			return errors.Wrapf(err, "failed to read overlay file %s", path)
		}

		if err := os.MkdirAll(filepath.Dir(destPath), DefaultDirMode); err != nil {
			return errors.Wrapf(err, "failed to create directory for %s", destPath)
		}

		g.props.Logger.Debugf("Materialising %s", relPath)

		return os.WriteFile(destPath, content, DefaultFileMode)
	})
}

// runDryRunPostProcessing runs the configured post-processing commands in the
// materialised temp directory. Failures are logged as warnings (non-fatal).
func (g *Generator) runDryRunPostProcessing(ctx context.Context, dir string, pp *dryRunPostProcess) {
	for _, cmdArgs := range pp.commands {
		if len(cmdArgs) == 0 {
			continue
		}

		g.props.Logger.Infof("Dry run post-processing: %s", strings.Join(cmdArgs, " "))

		cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...) //nolint:gosec // commands are hardcoded by callers, not user input
		cmd.Dir = dir

		if err := cmd.Run(); err != nil {
			g.props.Logger.Warnf("Dry run post-processing failed (%s): %v", cmdArgs[0], err)
		}
	}
}

// produceDryRunResult compares files between two filesystem locations and
// produces a DryRunResult describing what would change. When basePath and
// overlayPath differ (materialised mode), files are read from separate
// directory trees on the same filesystem.
func produceDryRunResult(base, overlay afero.Fs, paths ...string) (*DryRunResult, error) {
	var basePath, overlayPath string

	switch len(paths) {
	case 1:
		// Same path on both filesystems (overlay mode).
		basePath = paths[0]
		overlayPath = paths[0]
	case 2: //nolint:mnd // basePath and overlayPath
		// Separate paths (materialised mode): base is the original project,
		// overlay is the temp directory.
		basePath = paths[0]
		overlayPath = paths[1]
	default:
		return nil, errors.New("produceDryRunResult requires 1 or 2 path arguments")
	}

	result := &DryRunResult{}

	err := afero.Walk(overlay, overlayPath, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if info.IsDir() {
			return nil
		}

		return classifyFile(base, overlay, basePath, overlayPath, path, result)
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to walk overlay filesystem")
	}

	return result, nil
}

// classifyFile compares a single file between base and overlay, appending to
// the result as created or modified.
func classifyFile(base, overlay afero.Fs, basePath, overlayPath, path string, result *DryRunResult) error {
	relPath, err := filepath.Rel(overlayPath, path)
	if err != nil {
		return errors.Wrap(err, "failed to compute relative path")
	}

	overlayContent, err := afero.ReadFile(overlay, path)
	if err != nil {
		return errors.Wrap(err, "failed to read overlay file")
	}

	baseFilePath := filepath.Join(basePath, relPath)

	existsOnBase, err := afero.Exists(base, baseFilePath)
	if err != nil {
		return errors.Wrap(err, "failed to check base file existence")
	}

	if !existsOnBase {
		result.Created = append(result.Created, FilePreview{
			Path:    relPath,
			Content: overlayContent,
		})

		return nil
	}

	return classifyModifiedFile(base, relPath, baseFilePath, overlayContent, result)
}

// classifyModifiedFile checks whether an existing file has changed and, if so,
// generates a unified diff and appends it to the result.
func classifyModifiedFile(base afero.Fs, relPath, absPath string, overlayContent []byte, result *DryRunResult) error {
	baseContent, err := afero.ReadFile(base, absPath)
	if err != nil {
		return errors.Wrap(err, "failed to read base file")
	}

	if string(baseContent) == string(overlayContent) {
		return nil
	}

	diff, err := generateUnifiedDiff(relPath, baseContent, overlayContent)
	if err != nil {
		return errors.Wrap(err, "failed to generate diff")
	}

	result.Modified = append(result.Modified, FilePreview{
		Path: relPath,
		Diff: diff,
	})

	return nil
}

// generateUnifiedDiff produces a unified diff string between two byte slices.
func generateUnifiedDiff(path string, original, modified []byte) (string, error) {
	diff, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(string(original)),
		B:        difflib.SplitLines(string(modified)),
		FromFile: path + " (current)",
		ToFile:   path + " (incoming)",
		Context:  diffContextLines,
	})
	if err != nil {
		return "", errors.Wrap(err, "difflib failed")
	}

	return strings.TrimRight(diff, "\n"), nil
}
